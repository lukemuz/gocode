package agent

import (
	"encoding/json"
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
