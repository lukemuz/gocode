package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
)

// ToolFunc is the signature every tool implementation must satisfy.
// input is the raw JSON the model produced as arguments for this call.
// Return the result as a plain string, or an error if the tool failed.
// Errors are fed back to the model as is_error=true results so it can
// recover; they do not abort the loop.
type ToolFunc func(ctx context.Context, input json.RawMessage) (string, error)

// TypedToolFunc returns a ToolFunc that automatically unmarshals the raw
// JSON input into the generic Input type before calling the provided
// handler. This removes the repetitive json.Unmarshal + error handling
// boilerplate while producing an ordinary ToolFunc that works with any
// dispatch map and preserves all existing error/inspectability behavior.
//
// For struct Input types, empty input (nil, zero-length, or JSON "null")
// is treated as `{}` to avoid unmarshal errors on tools with no params.
// Other types (string, int, etc.) unmarshal strictly.
//
// See NewTypedTool(...) for a one-line way to obtain both a Tool and
// the corresponding ToolFunc. Example (from the roadmap):
//
//	type CalculatorInput struct {
//		Operation string  `json:"operation"`
//		A         float64 `json:"a"`
//		B         float64 `json:"b"`
//	}
//
//	fn := TypedToolFunc(func(ctx context.Context, in CalculatorInput) (string, error) {
//		switch in.Operation {
//		case "add":
//			return fmt.Sprintf("%f", in.A+in.B), nil
//		case "subtract":
//			return fmt.Sprintf("%f", in.A-in.B), nil
//		case "multiply":
//			return fmt.Sprintf("%f", in.A*in.B), nil
//		default:
//			return "", fmt.Errorf("unknown operation: %s", in.Operation)
//		}
//	})
//
// The resulting fn can be used directly in a dispatch map[string]ToolFunc.
func TypedToolFunc[Input any](f func(context.Context, Input) (string, error)) ToolFunc {
	typ := reflect.TypeOf((*Input)(nil)).Elem()
	isStruct := typ.Kind() == reflect.Struct

	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if isStruct && (len(input) == 0 || string(input) == "null") {
			input = json.RawMessage(`{}`)
		}
		var in Input
		if err := json.Unmarshal(input, &in); err != nil {
			return "", fmt.Errorf("agent: unmarshal tool input: %w", err)
		}
		return f(ctx, in)
	}
}

// NewTypedTool combines NewTool + TypedToolFunc into a single call that
// returns both the Tool (for your tools []Tool slice) and the wrapped
// ToolFunc (for your dispatch map). This is the most ergonomic path shown
// in the roadmap while still letting you inspect everything.
//
// Panics if schema cannot be marshalled to JSON; in practice InputSchema
// always marshals successfully, so this is a programmer-error indicator.
func NewTypedTool[Input any](
	name, description string,
	schema InputSchema,
	f func(context.Context, Input) (string, error),
) (Tool, ToolFunc) {
	return NewTool(name, description, schema), TypedToolFunc(f)
}

// JSONResult marshals v to a JSON string. It is the recommended helper
// for typed tool handlers that want to return structured data the model
// can reliably parse. Marshal errors are wrapped with context.
func JSONResult(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("agent: marshal tool result: %w", err)
	}
	return string(data), nil
}

// SchemaProperty describes one parameter within an InputSchema.
// The schema builder helpers (below) populate these fields; Enum supports
// constrained values; Items, Properties, and Required let arrays and nested
// objects round-trip without dropping to hand-written JSON.
//
// For schemas richer than these fields express (oneOf, $ref, patternProperties,
// etc.) construct a Tool directly with a json.RawMessage InputSchema — see the
// "Hand-rolled schemas" section in RECIPES.md.
type SchemaProperty struct {
	Type        string                    `json:"type"`
	Description string                    `json:"description,omitempty"`
	Enum        []any                     `json:"enum,omitempty"`
	Items       *SchemaProperty           `json:"items,omitempty"`      // for type:"array"
	Properties  map[string]SchemaProperty `json:"properties,omitempty"` // for type:"object"
	Required    []string                  `json:"required,omitempty"`   // for type:"object"
}

// InputSchema is the JSON Schema object describing a tool's input parameters.
// The Anthropic API requires Type to be "object". Use the schema builder
// helpers below for ergonomic construction; the output is identical to
// a hand-written literal.
type InputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]SchemaProperty `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// Schema builder helpers follow the design principles (explicit core,
// simple assembly, progressive disclosure, Lego-like tools, agent-legible
// by design). They are pure convenience: no reflection, no hidden state,
// fully inspectable output (Property and InputSchema are plain structs).
// Users can always fall back to writing InputSchema by hand. Matches the
// ROADMAP.md examples and compiles down to the primitives.
//
// Example:
//
//	schema := Object(
//		String("path", "Path to read", Required()),
//		Number("limit", "Max items to return", Enum(10, 25, 50)),
//	)
type Option func(*Property)

// Property represents one field for Object(). All fields are exported so
// the constructed schema remains fully transparent and inspectable.
type Property struct {
	Name string
	SchemaProperty
	Required bool
}

// Required returns an Option that marks this property as required in
// the generated InputSchema.
func Required() Option {
	return func(p *Property) { p.Required = true }
}

// Enum returns an Option that adds an enum constraint to the property.
// The values appear directly in the JSON schema.
func Enum(values ...any) Option {
	return func(p *Property) { p.Enum = values }
}

// String returns a Property with type "string".
func String(name, description string, opts ...Option) Property {
	p := Property{
		Name:           name,
		SchemaProperty: SchemaProperty{Type: "string", Description: description},
	}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

// Number returns a Property with type "number".
func Number(name, description string, opts ...Option) Property {
	p := Property{
		Name:           name,
		SchemaProperty: SchemaProperty{Type: "number", Description: description},
	}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

// Integer returns a Property with type "integer".
func Integer(name, description string, opts ...Option) Property {
	p := Property{
		Name:           name,
		SchemaProperty: SchemaProperty{Type: "integer", Description: description},
	}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

// Boolean returns a Property with type "boolean".
func Boolean(name, description string, opts ...Option) Property {
	p := Property{
		Name:           name,
		SchemaProperty: SchemaProperty{Type: "boolean", Description: description},
	}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

// Object builds an InputSchema from Properties. This is the main entry
// point for ergonomic tool schemas. The result is identical to a manual
// InputSchema literal and works with NewTool/NewTypedTool.
func Object(fields ...Property) InputSchema {
	props, required := buildFields(fields)
	s := InputSchema{
		Type:       "object",
		Properties: props,
	}
	if len(required) > 0 {
		s.Required = required
	}
	return s
}

// ObjectOf builds a nested-object SchemaProperty, suitable for passing as the
// item type to Array or as a child of a parent ObjectOf. Use Object at the
// top level (it returns InputSchema, the type tools require); use ObjectOf
// anywhere a SchemaProperty is expected.
//
// Example:
//
//	Array("subtasks", "List of sub-questions",
//	    ObjectOf(
//	        String("question", "Sub-question to research", Required()),
//	        String("rationale", "Why this matters"),
//	    ),
//	    Required())
func ObjectOf(fields ...Property) SchemaProperty {
	props, required := buildFields(fields)
	return SchemaProperty{
		Type:       "object",
		Properties: props,
		Required:   required,
	}
}

// Array returns a Property with type "array". items describes the element
// schema; use a primitive SchemaProperty (e.g. {Type: "string"}) for arrays
// of scalars or ObjectOf(...) for arrays of objects.
//
// Example:
//
//	Array("tags", "Free-form tags", SchemaProperty{Type: "string"})
//	Array("steps", "Pipeline steps", ObjectOf(
//	    String("name", "Step name", Required()),
//	    Integer("retries", "Retry count"),
//	), Required())
func Array(name, description string, items SchemaProperty, opts ...Option) Property {
	itemsCopy := items
	p := Property{
		Name: name,
		SchemaProperty: SchemaProperty{
			Type:        "array",
			Description: description,
			Items:       &itemsCopy,
		},
	}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

// buildFields turns a slice of Property values into the (properties, required)
// pair shared by Object and ObjectOf. Required is nil-safe: an empty slice is
// returned, which marshals away under the omitempty tag.
func buildFields(fields []Property) (map[string]SchemaProperty, []string) {
	props := make(map[string]SchemaProperty, len(fields))
	var required []string
	for _, f := range fields {
		props[f.Name] = f.SchemaProperty
		if f.Required {
			required = append(required, f.Name)
		}
	}
	return props, required
}

// Tool defines a capability available to the agent during a Loop.
// InputSchema is stored as pre-serialized JSON so callers can supply schemas
// richer than InputSchema expresses (nested objects, arrays, $defs) without
// the library needing to understand them.
//
// For provider-defined client-executed tools (category 2 — e.g. Anthropic's
// bash_20250124, text_editor_20250124, computer_20250124) the wire shape is
// {"type": "...", "name": "..."} rather than {name, description, input_schema}.
// Such tools set Provider and Raw: Provider tags the binding to one provider,
// and Raw is the verbatim JSON declaration spliced into the wire tools array.
// Name remains populated so the agent loop can dispatch tool_use blocks
// returned by the model.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`

	// Provider, if set, restricts this tool to a specific provider tag
	// (e.g. "anthropic"). Providers reject mismatched tools at request
	// build time so misuse fails loudly rather than silently dropping the
	// declaration.
	Provider string `json:"-"`

	// Raw, if set, replaces the wire serialization of this tool. Used by
	// provider-defined tools whose declaration form is not the standard
	// {name, description, input_schema}. The agent loop never inspects Raw.
	Raw json.RawMessage `json:"-"`
}

// NewTool constructs a Tool from a typed InputSchema.
//
// Panics if schema cannot be marshalled to JSON. InputSchema is a plain Go
// struct of strings, maps, and slices — marshal cannot fail in practice, so
// the panic is a programmer-error indicator (corrupt unsafe.Pointer, etc.)
// rather than a runtime condition callers should handle.
func NewTool(name, description string, schema InputSchema) Tool {
	raw, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Errorf("agent: marshal tool schema for %q: %w", name, err))
	}
	return Tool{Name: name, Description: description, InputSchema: raw}
}

// MarshalJSON emits Raw verbatim when set (category-2 provider-defined tools
// have a non-standard wire form). Otherwise it emits the standard
// {name, description, input_schema} object.
func (t Tool) MarshalJSON() ([]byte, error) {
	if len(t.Raw) > 0 {
		return t.Raw, nil
	}
	type alias Tool
	return json.Marshal(alias(t))
}

// ProviderTool is a server-executed (category-1) tool advertised to the model.
// The provider runs it; no Go ToolFunc is needed. The agent loop never
// inspects ProviderTools — they pass through ProviderRequest to the provider,
// which splices Raw verbatim into its native tools array. Provider tags the
// entry to a specific provider so misuse fails at request-build time.
//
// Use the typed constructors (AnthropicWebSearch, AnthropicCodeExecution, ...)
// to build these rather than instantiating the struct directly.
type ProviderTool struct {
	Provider string          // e.g. "anthropic"
	Raw      json.RawMessage // verbatim JSON spliced into the provider's tools array
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
