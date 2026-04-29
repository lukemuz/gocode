package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/lukemuz/gocode"
	"github.com/lukemuz/gocode/providers/openai"
)

const (
	openRouterDefaultBaseURL = "https://openrouter.ai"
	defaultHTTPTimeout       = 60 * time.Second
)

// Config holds configuration for the OpenRouter provider.
type Config struct {
	APIKey     string       // required
	BaseURL    string       // defaults to https://openrouter.ai
	HTTPClient *http.Client // defaults to a 60-second timeout client
}

// Provider implements Provider for the OpenRouter API (OpenAI-compatible).
type Provider struct {
	cfg Config
}

// NewProvider creates an Provider, filling in defaults.
func NewProvider(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("gocode: Config.APIKey is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = openRouterDefaultBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Provider{cfg: cfg}, nil
}

// NewProviderFromEnv creates an Provider using the
// OPENROUTER_API_KEY environment variable. Returns an error if the variable
// is unset or empty.
func NewProviderFromEnv() (*Provider, error) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("gocode: OPENROUTER_API_KEY environment variable is not set")
	}
	return NewProvider(Config{APIKey: key})
}

// NewClientFromEnv creates a Client backed by the OpenRouter provider,
// reading the API key from OPENROUTER_API_KEY. model is any model string
// supported by OpenRouter (e.g. "anthropic/claude-sonnet-4-6").
func NewClientFromEnv(model string) (*gocode.Client, error) {
	provider, err := NewProviderFromEnv()
	if err != nil {
		return nil, err
	}
	return gocode.New(gocode.Config{Provider: provider, Model: model})
}

// Call implements Provider for OpenRouter.
// It uses the OpenAI-compatible chat completions endpoint exposed by OpenRouter.
func (p *Provider) Call(ctx context.Context, req gocode.ProviderRequest) (gocode.ProviderResponse, error) {
	return openai.CompatibleCall(
		ctx,
		p.cfg.HTTPClient,
		p.cfg.APIKey,
		p.cfg.BaseURL+"/api/v1/chat/completions",
		req,
	)
}

// Stream implements Provider.Stream for OpenRouter by delegating to the shared
// streaming helper (mirrors the Call pattern).
func (p *Provider) Stream(ctx context.Context, req gocode.ProviderRequest, onDelta func(gocode.ContentBlock)) (gocode.ProviderResponse, error) {
	return openai.CompatibleStream(
		ctx,
		p.cfg.HTTPClient,
		p.cfg.APIKey,
		p.cfg.BaseURL+"/api/v1/chat/completions",
		req,
		onDelta,
	)
}
