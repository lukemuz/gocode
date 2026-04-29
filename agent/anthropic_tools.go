package agent

import (
	"encoding/json"
	"fmt"
)

// This file holds the user-facing constructors for Anthropic-specific
// provider tools. Two flavours:
//
//   - Category 1 (server-executed): the Anthropic API runs the tool and
//     returns the result inline. No Go ToolFunc is involved. Constructors
//     return a ProviderTool, which is attached via Toolset.WithProviderTools.
//     Examples: AnthropicWebSearch, AnthropicCodeExecution.
//
//   - Category 2 (provider-defined schema, client-executed): the model has
//     been post-trained on the tool's name and schema, but you run it.
//     Constructors take a ToolFunc and return a ToolBinding that drops into
//     a normal Toolset. The wire declaration form is {"type": "...", "name":
//     "..."} rather than the standard {name, description, input_schema}.
//     Examples: AnthropicBashTool, AnthropicTextEditorTool, AnthropicComputerTool.
//
// All constructors target one provider; passing the result to a different
// provider fails at request build time with a clear error.

// ---------------------------------------------------------------------------
// Category 1: server-executed tools
// ---------------------------------------------------------------------------

// WebSearchOpts configures the Anthropic web_search server tool.
// All fields are optional. See Anthropic's docs for current defaults and
// caps; this struct is a thin pass-through and adds no policy of its own.
type WebSearchOpts struct {
	// MaxUses caps the number of search invocations per turn. 0 omits the
	// field and lets the API apply its default.
	MaxUses int

	// AllowedDomains restricts results to these domains (mutually exclusive
	// with BlockedDomains in the API).
	AllowedDomains []string

	// BlockedDomains excludes these domains from results.
	BlockedDomains []string

	// UserLocation, if set, biases results to the given location. Pass a
	// pre-built map matching the Anthropic schema (type/city/region/country/
	// timezone) so callers aren't tied to a struct shape that may evolve.
	UserLocation map[string]any
}

// AnthropicWebSearch returns a ProviderTool that advertises Anthropic's
// hosted web_search tool to the model. The Anthropic API performs the search
// and inlines the results as web_search_tool_result content blocks; the
// agent loop transparently round-trips them via ContentBlock.Raw.
func AnthropicWebSearch(opts WebSearchOpts) ProviderTool {
	body := map[string]any{
		"type": "web_search_20250305",
		"name": "web_search",
	}
	if opts.MaxUses > 0 {
		body["max_uses"] = opts.MaxUses
	}
	if len(opts.AllowedDomains) > 0 {
		body["allowed_domains"] = opts.AllowedDomains
	}
	if len(opts.BlockedDomains) > 0 {
		body["blocked_domains"] = opts.BlockedDomains
	}
	if opts.UserLocation != nil {
		body["user_location"] = opts.UserLocation
	}
	return ProviderTool{Provider: providerTagAnthropic, Raw: mustMarshal(body)}
}

// AnthropicCodeExecution returns a ProviderTool that advertises Anthropic's
// hosted code_execution tool. The API runs Python in a sandbox and returns
// code_execution_tool_result blocks inline.
func AnthropicCodeExecution() ProviderTool {
	body := map[string]any{
		"type": "code_execution_20250522",
		"name": "code_execution",
	}
	return ProviderTool{Provider: providerTagAnthropic, Raw: mustMarshal(body)}
}

// ---------------------------------------------------------------------------
// Category 2: provider-defined schema, client-executed tools
// ---------------------------------------------------------------------------

// AnthropicBashTool wraps a user-supplied handler as Anthropic's bash tool.
// The wire declaration is {"type": "bash_20250124", "name": "bash"}; the
// model's input shape is fixed by training (a "command" string and optional
// "restart" flag) — the handler is responsible for actually executing it.
//
// Pair with agent.WithConfirmation or sandboxing middleware when running
// untrusted output.
func AnthropicBashTool(fn ToolFunc) ToolBinding {
	body := map[string]any{
		"type": "bash_20250124",
		"name": "bash",
	}
	tool := Tool{
		Name:     "bash",
		Provider: providerTagAnthropic,
		Raw:      mustMarshal(body),
	}
	return ToolBinding{
		Tool: tool,
		Func: fn,
		Meta: ToolMetadata{
			Source:               "anthropic.bash_20250124",
			Shell:                true,
			RequiresConfirmation: true,
			Destructive:          true,
			Network:              true,
			Filesystem:           true,
		},
	}
}

// AnthropicTextEditorTool wraps a user-supplied handler as Anthropic's
// text_editor tool. The wire declaration is {"type": "text_editor_20250124",
// "name": "str_replace_editor"}; the model emits "command" actions (view,
// create, str_replace, insert, undo_edit) the handler must implement.
func AnthropicTextEditorTool(fn ToolFunc) ToolBinding {
	body := map[string]any{
		"type": "text_editor_20250124",
		"name": "str_replace_editor",
	}
	tool := Tool{
		Name:     "str_replace_editor",
		Provider: providerTagAnthropic,
		Raw:      mustMarshal(body),
	}
	return ToolBinding{
		Tool: tool,
		Func: fn,
		Meta: ToolMetadata{
			Source:     "anthropic.text_editor_20250124",
			Filesystem: true,
		},
	}
}

// ComputerOpts configures the Anthropic computer-use tool. DisplayWidthPx
// and DisplayHeightPx are required by the API. DisplayNumber is optional
// (useful on multi-display X servers).
type ComputerOpts struct {
	DisplayWidthPx  int
	DisplayHeightPx int
	DisplayNumber   int
}

// AnthropicComputerTool wraps a user-supplied handler as Anthropic's
// computer-use tool. The model emits "action" verbs (mouse_move, left_click,
// type, key, screenshot, ...) plus coordinates; the handler is responsible
// for driving the actual display, keyboard, and mouse.
//
// The handler runs with high privilege (full keyboard/mouse access on the
// host display); consider WithConfirmation or running in a contained VM.
func AnthropicComputerTool(opts ComputerOpts, fn ToolFunc) ToolBinding {
	body := map[string]any{
		"type":              "computer_20250124",
		"name":              "computer",
		"display_width_px":  opts.DisplayWidthPx,
		"display_height_px": opts.DisplayHeightPx,
	}
	if opts.DisplayNumber > 0 {
		body["display_number"] = opts.DisplayNumber
	}
	tool := Tool{
		Name:     "computer",
		Provider: providerTagAnthropic,
		Raw:      mustMarshal(body),
	}
	return ToolBinding{
		Tool: tool,
		Func: fn,
		Meta: ToolMetadata{
			Source:               "anthropic.computer_20250124",
			RequiresConfirmation: true,
			Destructive:          true,
		},
	}
}

// mustMarshal panics on json.Marshal error. The inputs above are plain maps
// of strings, ints, and slices, so marshal cannot fail in practice; a panic
// here would indicate a programmer error (corrupt memory, etc.).
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("agent: marshal anthropic provider tool: %w", err))
	}
	return b
}
