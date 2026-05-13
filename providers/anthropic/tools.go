package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/lukemuz/luft"
)

// User-facing constructors for Anthropic-specific provider tools. Two flavours:
//
//   - Category 1 (server-executed): the Anthropic API runs the tool and
//     returns the result inline. No Go ToolFunc is involved. Constructors
//     return a luft.ProviderTool, attached via Toolset.WithProviderTools.
//     Examples: WebSearch, CodeExecution.
//
//   - Category 2 (provider-defined schema, client-executed): the model has
//     been post-trained on the tool's name and schema, but you run it.
//     Constructors take a luft.ToolFunc and return a luft.ToolBinding
//     that drops into a normal Toolset. The wire declaration form is
//     {"type": "...", "name": "..."} rather than the standard
//     {name, description, input_schema}. Examples: BashTool, TextEditorTool,
//     ComputerTool.
//
// All constructors stamp ProviderTag on the returned values; passing them
// to a non-Anthropic provider fails at request build time with a clear error.

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

// WebSearch returns a luft.ProviderTool that advertises Anthropic's hosted
// web_search tool to the model. The Anthropic API performs the search and
// inlines the results as web_search_tool_result content blocks; the agent
// loop transparently round-trips them via luft.ContentBlock.Raw.
//
// Uses "web_search_20260209" — the dynamic-filtering generation. The prior
// "web_search_20250305" remains accepted by the API for callers that want
// the static-results behaviour. Bump this string when Anthropic ships a
// newer dated identifier.
func WebSearch(opts WebSearchOpts) luft.ProviderTool {
	body := map[string]any{
		"type": "web_search_20260209",
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
	return luft.ProviderTool{Provider: ProviderTag, Raw: mustMarshal(body)}
}

// WebFetchOpts configures the Anthropic web_fetch server tool.
type WebFetchOpts struct {
	// MaxUses caps the number of fetch invocations per turn. 0 omits.
	MaxUses int

	// AllowedDomains restricts fetches to these domains.
	AllowedDomains []string

	// BlockedDomains excludes these domains.
	BlockedDomains []string

	// MaxContentTokens caps how much fetched content the model receives
	// per fetch. 0 lets the API apply its default.
	MaxContentTokens int

	// Citations toggles inline citations on fetched content.
	Citations bool
}

// WebFetch returns a luft.ProviderTool that advertises Anthropic's hosted
// web_fetch tool to the model. The Anthropic API performs the fetch and
// inlines the result.
//
// Uses "web_fetch_20260209" — the dynamic-filtering generation, supported
// on Claude Opus 4.6+ and Sonnet 4.6+. Dynamic filtering only activates
// when the code_execution tool is also enabled; without it, behaviour
// matches the prior "web_fetch_20250910" version. Bump this string when
// Anthropic ships a newer dated identifier.
func WebFetch(opts WebFetchOpts) luft.ProviderTool {
	body := map[string]any{
		"type": "web_fetch_20260209",
		"name": "web_fetch",
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
	if opts.MaxContentTokens > 0 {
		body["max_content_tokens"] = opts.MaxContentTokens
	}
	if opts.Citations {
		body["citations"] = map[string]any{"enabled": true}
	}
	return luft.ProviderTool{Provider: ProviderTag, Raw: mustMarshal(body)}
}

// CodeExecution returns a luft.ProviderTool that advertises Anthropic's
// hosted code_execution tool. The API runs Python in a sandbox and returns
// code_execution_tool_result blocks inline.
func CodeExecution() luft.ProviderTool {
	body := map[string]any{
		"type": "code_execution_20250522",
		"name": "code_execution",
	}
	return luft.ProviderTool{Provider: ProviderTag, Raw: mustMarshal(body)}
}

// ---------------------------------------------------------------------------
// Category 2: provider-defined schema, client-executed tools
// ---------------------------------------------------------------------------

// BashTool wraps a user-supplied handler as Anthropic's bash tool.
// The wire declaration is {"type": "bash_20250124", "name": "bash"}; the
// model's input shape is fixed by training (a "command" string and optional
// "restart" flag) — the handler is responsible for actually executing it.
//
// Pair with luft.WithConfirmation or sandboxing middleware when running
// untrusted output.
func BashTool(fn luft.ToolFunc) luft.ToolBinding {
	body := map[string]any{
		"type": "bash_20250124",
		"name": "bash",
	}
	tool := luft.Tool{
		Name:     "bash",
		Provider: ProviderTag,
		Raw:      mustMarshal(body),
	}
	return luft.ToolBinding{
		Tool: tool,
		Func: fn,
		Meta: luft.ToolMetadata{
			Source:               "anthropic.bash_20250124",
			Shell:                true,
			RequiresConfirmation: true,
			Destructive:          true,
			Network:              true,
			Filesystem:           true,
		},
	}
}

// TextEditorTool wraps a user-supplied handler as Anthropic's text_editor
// tool. The wire declaration is {"type": "text_editor_20250124", "name":
// "str_replace_editor"}; the model emits "command" actions (view, create,
// str_replace, insert, undo_edit) the handler must implement.
//
// This is the legacy variant. For Claude 4.x prefer TextEditor20250728,
// which uses the newer name "str_replace_based_edit_tool", drops the
// undo_edit command, and adds the max_characters parameter on view.
func TextEditorTool(fn luft.ToolFunc) luft.ToolBinding {
	body := map[string]any{
		"type": "text_editor_20250124",
		"name": "str_replace_editor",
	}
	tool := luft.Tool{
		Name:     "str_replace_editor",
		Provider: ProviderTag,
		Raw:      mustMarshal(body),
	}
	return luft.ToolBinding{
		Tool: tool,
		Func: fn,
		Meta: luft.ToolMetadata{
			Source:     "anthropic.text_editor_20250124",
			Filesystem: true,
		},
	}
}

// TextEditor20250728 wraps a handler as Anthropic's latest text editor
// tool. The wire declaration is {"type": "text_editor_20250728", "name":
// "str_replace_based_edit_tool"}, supported on Claude 4.x. The model
// emits four commands the handler must implement:
//
//   - view:        {path, view_range?: [start, end], max_characters?: int}
//   - str_replace: {path, old_str, new_str}
//   - create:      {path, file_text}
//   - insert:      {path, insert_line, new_str}
//
// undo_edit was removed in this version. max_characters (added 2025-07-28)
// caps how much of a viewed file is returned.
func TextEditor20250728(fn luft.ToolFunc) luft.ToolBinding {
	body := map[string]any{
		"type": "text_editor_20250728",
		"name": "str_replace_based_edit_tool",
	}
	tool := luft.Tool{
		Name:     "str_replace_based_edit_tool",
		Provider: ProviderTag,
		Raw:      mustMarshal(body),
	}
	return luft.ToolBinding{
		Tool: tool,
		Func: fn,
		Meta: luft.ToolMetadata{
			Source:               "anthropic.text_editor_20250728",
			Filesystem:           true,
			RequiresConfirmation: true,
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

// ComputerTool wraps a user-supplied handler as Anthropic's computer-use
// tool. The model emits "action" verbs (mouse_move, left_click, type, key,
// screenshot, ...) plus coordinates; the handler is responsible for driving
// the actual display, keyboard, and mouse.
//
// The handler runs with high privilege (full keyboard/mouse access on the
// host display); consider luft.WithConfirmation or running in a contained VM.
func ComputerTool(opts ComputerOpts, fn luft.ToolFunc) luft.ToolBinding {
	body := map[string]any{
		"type":              "computer_20250124",
		"name":              "computer",
		"display_width_px":  opts.DisplayWidthPx,
		"display_height_px": opts.DisplayHeightPx,
	}
	if opts.DisplayNumber > 0 {
		body["display_number"] = opts.DisplayNumber
	}
	tool := luft.Tool{
		Name:     "computer",
		Provider: ProviderTag,
		Raw:      mustMarshal(body),
	}
	return luft.ToolBinding{
		Tool: tool,
		Func: fn,
		Meta: luft.ToolMetadata{
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
		panic(fmt.Errorf("luft: anthropic: marshal provider tool: %w", err))
	}
	return b
}
