package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolFunc is the signature every tool implementation must satisfy.
// input is the raw JSON the model produced as arguments for this call.
// Return the result as a plain string, or an error if the tool failed.
// Errors are fed back to the model as is_error=true results so it can
// recover; they do not abort the loop.
type ToolFunc func(ctx context.Context, input json.RawMessage) (string, error)

// SchemaProperty describes one parameter within an InputSchema.
type SchemaProperty struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// InputSchema is the JSON Schema object describing a tool's input parameters.
// The Anthropic API requires Type to be "object".
type InputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]SchemaProperty `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// Tool defines a capability available to the agent during a Loop.
// InputSchema is stored as pre-serialized JSON so callers can supply schemas
// richer than InputSchema expresses (nested objects, arrays, $defs) without
// the library needing to understand them.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// NewTool constructs a Tool from a typed InputSchema.
func NewTool(name, description string, schema InputSchema) (Tool, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return Tool{}, fmt.Errorf("agent: marshal tool schema for %q: %w", name, err)
	}
	return Tool{Name: name, Description: description, InputSchema: raw}, nil
}

// ToolResult is the output of one ToolFunc execution.
type ToolResult struct {
	ToolUseID string // matches ContentBlock.ID from the corresponding tool_use block
	Content   string // the string returned by ToolFunc, or the error message
	IsError   bool
}

// ToolUse is extracted from an assistant ContentBlock and passed to dispatch.
type ToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func extractToolUses(blocks []ContentBlock) []ToolUse {
	var uses []ToolUse
	for _, b := range blocks {
		if b.Type == TypeToolUse {
			uses = append(uses, ToolUse{ID: b.ID, Name: b.Name, Input: b.Input})
		}
	}
	return uses
}
