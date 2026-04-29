package agent

import (
	"context"
	"fmt"
	"net/http"
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

// Call implements Provider for OpenRouter.
// It uses the OpenAI-compatible chat completions endpoint exposed by OpenRouter.
func (p *OpenRouterProvider) Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	return doOpenAICompatibleCall(
		ctx,
		p.cfg.HTTPClient,
		p.cfg.APIKey,
		p.cfg.BaseURL+"/api/v1/chat/completions",
		req,
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
	)
}
