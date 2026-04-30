package gocode

// Usage records token consumption for one API call.
//
// CacheCreationTokens and CacheReadTokens carry prompt-cache statistics
// from providers that report them (Anthropic and OpenRouter today).
// CacheCreationTokens are billed input tokens that wrote a fresh cache
// entry; CacheReadTokens are input tokens served from cache at a discount.
// Providers that don't surface cache info leave both fields zero.
type Usage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadTokens     int `json:"cache_read_input_tokens,omitempty"`
}

const defaultMaxTokens = 1024
