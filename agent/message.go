package agent

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
)

// ContentBlock is one element in a message's content array.
// Each block has a Type that determines which other fields are populated:
//   - "text":        Text is the response string
//   - "tool_use":    ID, Name, and Input carry the model's tool call
//   - "tool_result": ToolUseID and Content carry the tool's return value
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
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

// NewToolResultMessage builds the user-role turn that returns tool outputs
// to the model after a tool_use response.
func NewToolResultMessage(results []ToolResult) Message {
	blocks := make([]ContentBlock, len(results))
	for i, r := range results {
		blocks[i] = ContentBlock{
			Type:      TypeToolResult,
			ToolUseID: r.ToolUseID,
			Content:   r.Content,
			IsError:   r.IsError,
		}
	}
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
