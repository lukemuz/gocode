package openrouter

import (
	"encoding/json"
	"fmt"

	"github.com/lukemuz/luft"
)

// ProviderTag identifies tools targeted at the OpenRouter provider.
const ProviderTag = "openrouter"

// WebSearchOpts configures OpenRouter's hosted web_search server tool.
// All fields are optional; OpenRouter applies sensible defaults.
//
// Docs: https://openrouter.ai/docs/guides/features/server-tools/web-search
type WebSearchOpts struct {
	// Engine selects the underlying search backend. Empty leaves OpenRouter's
	// default ("auto", which uses native provider search where available
	// and falls back to Exa). Other values: "exa", "native", "firecrawl",
	// "parallel".
	Engine string

	// MaxResults caps the number of search results returned per invocation.
	// 0 leaves the OpenRouter default.
	MaxResults int

	// SearchPrompt overrides the system text injected to instruct the model
	// on when to search. Empty leaves the default.
	SearchPrompt string
}

// WebSearch returns a luft.ProviderTool that advertises OpenRouter's
// hosted web_search tool to the model. The model decides when to invoke;
// OpenRouter executes the search server-side and returns results to the
// model with citations.
//
// Citations surface in the ProviderResponse.Content as opaque
// luft.ContentBlock entries with Type "url_citation" (the JSON shape
// OpenRouter returns under each annotation). Inspect ContentBlock.Raw to
// render them; the agent loop ignores them just like any other unknown
// block type. Streaming citations are not yet surfaced; use Call (not
// Stream) when citation extraction matters.
//
// Pricing flows through the user's existing OpenRouter credits — no
// separate API key is needed beyond the OpenRouter one.
func WebSearch(opts WebSearchOpts) luft.ProviderTool {
	body := map[string]any{
		"type": "openrouter:web_search",
	}
	params := map[string]any{}
	if opts.Engine != "" {
		params["engine"] = opts.Engine
	}
	if opts.MaxResults > 0 {
		params["max_results"] = opts.MaxResults
	}
	if opts.SearchPrompt != "" {
		params["search_prompt"] = opts.SearchPrompt
	}
	if len(params) > 0 {
		body["parameters"] = params
	}
	raw, err := json.Marshal(body)
	if err != nil {
		panic(fmt.Errorf("luft: openrouter: marshal web_search tool: %w", err))
	}
	return luft.ProviderTool{Provider: ProviderTag, Raw: raw}
}
