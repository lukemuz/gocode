package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
)

const openRouterDefaultBaseURL = "https://openrouter.ai"

// OpenRouterConfig holds configuration for the OpenRouter provider.
type OpenRouterConfig struct {
	APIKey     string       // required
	BaseURL    string       // defaults to https://openrouter.ai
	HTTPClient *http.Client // defaults to a 60-second timeout client
}

// OpenRouterProvider implements Provider for the OpenRouter API (OpenAI-compatible).
type OpenRouterProvider struct {
	cfg OpenRouterConfig
}

// NewOpenRouterProvider creates an OpenRouterProvider, filling in defaults.
func NewOpenRouterProvider(cfg OpenRouterConfig) (*OpenRouterProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("agent: OpenRouterConfig.APIKey is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = openRouterDefaultBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &OpenRouterProvider{cfg: cfg}, nil
}

// NewOpenRouterProviderFromEnv creates an OpenRouterProvider using the
// OPENROUTER_API_KEY environment variable. Returns an error if the variable
// is unset or empty.
func NewOpenRouterProviderFromEnv() (*OpenRouterProvider, error) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("agent: OPENROUTER_API_KEY environment variable is not set")
	}
	return NewOpenRouterProvider(OpenRouterConfig{APIKey: key})
}

// NewOpenRouterClientFromEnv creates a Client backed by the OpenRouter provider,
// reading the API key from OPENROUTER_API_KEY. model is any model string
// supported by OpenRouter (e.g. "anthropic/claude-sonnet-4-6").
func NewOpenRouterClientFromEnv(model string) (*Client, error) {
	provider, err := NewOpenRouterProviderFromEnv()
	if err != nil {
		return nil, err
	}
	return New(Config{Provider: provider, Model: model})
}

// Call implements Provider for OpenRouter.
// It uses the OpenAI-compatible chat completions endpoint exposed by OpenRouter.
//
// cacheCompatible=true: OpenRouter accepts cache_control markers (passed
// through to Anthropic backends, ignored by OpenAI backends), so we emit
// the typed-parts content shape and tool cache_control field whenever the
// canonical types carry them.
func (p *OpenRouterProvider) Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	return doOpenAICompatibleCall(
		ctx,
		p.cfg.HTTPClient,
		p.cfg.APIKey,
		p.cfg.BaseURL+"/api/v1/chat/completions",
		req,
		true,
	)
}

// Stream implements Provider.Stream for OpenRouter by delegating to the shared
// streaming helper (mirrors the Call pattern).
func (p *OpenRouterProvider) Stream(ctx context.Context, req ProviderRequest, onDelta func(ContentBlock)) (ProviderResponse, error) {
	return doOpenAICompatibleStream(
		ctx,
		p.cfg.HTTPClient,
		p.cfg.APIKey,
		p.cfg.BaseURL+"/api/v1/chat/completions",
		req,
		onDelta,
		true,
	)
}
