package gocode

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Role constants for Message.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Type constants for ContentBlock.
const (
	TypeText       = "text"
	TypeToolUse    = "tool_use"
	TypeToolResult = "tool_result"
	TypeImage      = "image"
)

// ContentBlock is one element in a message's content array.
// Each block has a Type that determines which other fields are populated:
//   - "text":        Text is the response string
//   - "tool_use":    ID, Name, and Input carry the model's tool call
//   - "tool_result": ToolUseID and Content carry the tool's return value
//
// Provider-specific block types (e.g. Anthropic's "server_tool_use" or
// "web_search_tool_result", emitted when category-1 provider tools run) are
// preserved opaquely in Raw and round-trip verbatim. The agent loop ignores
// them — only Type=="tool_use" is dispatched locally.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`

	// Image fields. Populated when Type=="image". Source is either a
	// base64 data URI ("data:image/png;base64,...") or an http(s) URL;
	// MediaType is the IANA media type ("image/png", "image/jpeg", ...).
	// Caller is responsible for pre-encoding bytes; gocode does not
	// downsample or re-encode.
	Source    string `json:"source,omitempty"`
	MediaType string `json:"media_type,omitempty"`

	// CacheControl, if set, marks this block as a cache breakpoint. Caching
	// is cumulative — see the CacheControl docs. Currently honored by
	// AnthropicProvider and OpenRouterProvider; ignored by other providers.
	CacheControl *CacheControl `json:"cache_control,omitempty"`

	// Raw, if non-empty, is the verbatim JSON for this block. Set by
	// UnmarshalJSON for unknown (provider-specific) types so they can be
	// resent on the next request without loss. When set, MarshalJSON emits
	// Raw and ignores all other fields.
	Raw json.RawMessage `json:"-"`
}

// known content-block types are decoded into typed fields. Anything else
// is captured opaquely into Raw and round-tripped verbatim.
func isKnownBlockType(t string) bool {
	switch t {
	case TypeText, TypeToolUse, TypeToolResult, TypeImage:
		return true
	}
	return false
}

// MarshalJSON emits Raw verbatim if set; otherwise emits the standard fields.
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	if len(b.Raw) > 0 {
		return b.Raw, nil
	}
	type alias ContentBlock
	return json.Marshal(alias(b))
}

// UnmarshalJSON decodes known block types into typed fields. For unknown
// types it captures the entire JSON object into Raw so the block can be
// re-sent verbatim on subsequent requests (Anthropic requires server-tool
// result blocks to round-trip exactly).
func (b *ContentBlock) UnmarshalJSON(data []byte) error {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	if !isKnownBlockType(head.Type) {
		*b = ContentBlock{Type: head.Type, Raw: append(json.RawMessage(nil), data...)}
		return nil
	}
	type alias ContentBlock
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*b = ContentBlock(a)
	return nil
}

// Message is one turn in a conversation.
type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// NewUserMessage creates a plain-text user turn.
func NewUserMessage(text string) Message {
	return Message{
		Role:    RoleUser,
		Content: []ContentBlock{{Type: TypeText, Text: text}},
	}
}

// ImageBlock is the caller-facing representation of an image attachment.
// Source is either a base64 data URI ("data:image/png;base64,...") or an
// http(s) URL; MediaType is the IANA media type ("image/png", ...).
type ImageBlock struct {
	Source    string
	MediaType string
}

// NewUserMessageWithImages builds a user-role turn carrying a text block
// followed by one image content block per image. Pass an empty text to
// emit an image-only turn (the leading text block is skipped).
func NewUserMessageWithImages(text string, images []ImageBlock) Message {
	content := make([]ContentBlock, 0, len(images)+1)
	if text != "" {
		content = append(content, ContentBlock{Type: TypeText, Text: text})
	}
	for _, img := range images {
		content = append(content, ContentBlock{
			Type:      TypeImage,
			Source:    img.Source,
			MediaType: img.MediaType,
		})
	}
	return Message{Role: RoleUser, Content: content}
}

// NewToolResultMessage builds the user-role turn that returns tool outputs
// to the model after a tool_use response. Image attachments collected on
// any ToolResult are flattened onto the same canonical message after the
// tool_result blocks; the wire serializer splits them into a sibling
// role="user" message when sending.
func NewToolResultMessage(results []ToolResult) Message {
	blocks := make([]ContentBlock, 0, len(results))
	var images []ContentBlock
	for _, r := range results {
		blocks = append(blocks, ContentBlock{
			Type:      TypeToolResult,
			ToolUseID: r.ToolUseID,
			Content:   r.Content,
			IsError:   r.IsError,
		})
		for _, img := range r.Images {
			images = append(images, ContentBlock{
				Type:      TypeImage,
				Source:    img.Source,
				MediaType: img.MediaType,
			})
		}
	}
	blocks = append(blocks, images...)
	return Message{Role: RoleUser, Content: blocks}
}

// TextContent extracts and concatenates all text blocks from a message.
func TextContent(msg Message) string {
	var b strings.Builder
	for _, block := range msg.Content {
		if block.Type == TypeText {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

// RenderForSummary flattens a slice of messages into a plain-text transcript
// suitable for passing to a summarizer model. Each message is rendered as
// labeled lines (USER, ASSISTANT, ASSISTANT_TOOL_USE, TOOL_RESULT) that
// preserve the structure without paying the full token cost of large tool
// outputs: tool_use inputs and tool_result contents are abbreviated to
// maxToolBytes characters with a "...[truncated]" marker. Pass 0 for
// maxToolBytes to use the default of 400.
//
// This is the rendering most ContextManager.Summarizer implementations want.
// If you need different formatting (different abbreviation thresholds, JSON
// output, redaction), write your own — the message structures are public.
func RenderForSummary(msgs []Message, maxToolBytes int) string {
	if maxToolBytes <= 0 {
		maxToolBytes = 400
	}
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case RoleUser:
			text := TextContent(m)
			if text != "" {
				fmt.Fprintf(&b, "USER: %s\n", text)
				continue
			}
			for _, c := range m.Content {
				if c.Type == TypeToolResult {
					tag := "TOOL_RESULT"
					if c.IsError {
						tag = "TOOL_ERROR"
					}
					fmt.Fprintf(&b, "%s (%s): %s\n", tag, c.ToolUseID, abbreviate(c.Content, maxToolBytes))
				}
			}
		case RoleAssistant:
			for _, c := range m.Content {
				switch c.Type {
				case TypeText:
					fmt.Fprintf(&b, "ASSISTANT: %s\n", c.Text)
				case TypeToolUse:
					fmt.Fprintf(&b, "ASSISTANT_TOOL_USE: %s(%s)\n", c.Name, abbreviate(string(c.Input), maxToolBytes))
				}
			}
		}
	}
	return b.String()
}

func abbreviate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}
