package agent

// CacheControl marks a content block, tool definition, or system prompt as
// a cache breakpoint. The semantics are Anthropic's: caching is cumulative —
// a marker on, say, the last tool definition caches the system prompt and
// every preceding tool. Up to 4 markers per request.
//
// Providers translate this marker as appropriate:
//
//   - AnthropicProvider emits native cache_control blocks.
//   - OpenRouterProvider serializes content as a typed-parts array with
//     cache_control fields; works for Anthropic-backed routes.
//   - OpenAIProvider and OpenAIResponsesProvider ignore the marker —
//     OpenAI caches automatically for prefixes ≥1024 tokens.
//
// Set TTL to "1h" for the extended (more expensive write, longer-lived)
// tier; leave empty for the default 5-minute window.
type CacheControl struct {
	Type string `json:"type"`          // always "ephemeral"
	TTL  string `json:"ttl,omitempty"` // "" (default 5m) or "1h"
}

// Ephemeral returns the standard 5-minute cache marker. It is a thin
// constructor that documents intent at call sites.
func Ephemeral() *CacheControl {
	return &CacheControl{Type: "ephemeral"}
}

// EphemeralExtended returns a 1-hour cache marker. Cache writes cost more
// at this tier but reads remain cheap, so it pays off for prompts reused
// across long sessions or many users.
func EphemeralExtended() *CacheControl {
	return &CacheControl{Type: "ephemeral", TTL: "1h"}
}
