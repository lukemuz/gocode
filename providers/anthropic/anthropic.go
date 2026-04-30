package anthropic

import (
	"github.com/lukemuz/gocode"

	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	anthropicDefaultBaseURL = "https://api.anthropic.com"
	anthropicVersion        = "2023-06-01"
	defaultHTTPTimeout      = 60 * time.Second

	// oauthBetaHeader is the Anthropic-side opt-in required when
	// authenticating with a Claude subscription OAuth token instead of an
	// API key. Sent as the value of the "anthropic-beta" header.
	oauthBetaHeader = "oauth-2025-04-20"

	// claudeCodeIdentityPrompt is the leading system block the API
	// requires for OAuth-authenticated requests. The Anthropic OAuth
	// scopes only authorize traffic that identifies as Claude Code, so
	// every request prepends this block when OAuthToken is set.
	claudeCodeIdentityPrompt = "You are Claude Code, Anthropic's official CLI for Claude."
)

// Config holds configuration for the Anthropic provider. Exactly one of
// APIKey or OAuthToken must be set: APIKey routes traffic through the
// pay-as-you-go Anthropic API, OAuthToken authenticates against a
// Claude Pro/Max subscription using the same OAuth flow Claude Code uses.
type Config struct {
	APIKey     string       // pay-as-you-go API key (sk-ant-api...)
	OAuthToken string       // Claude subscription OAuth access token (sk-ant-oat...)
	BaseURL    string       // defaults to https://api.anthropic.com
	HTTPClient *http.Client // defaults to a 60-second timeout client
}

// Provider implements Provider for the Anthropic Messages API.
type Provider struct {
	cfg Config
}

// NewProvider creates a Provider, filling in defaults. Exactly one of
// Config.APIKey or Config.OAuthToken must be set.
func NewProvider(cfg Config) (*Provider, error) {
	switch {
	case cfg.APIKey != "" && cfg.OAuthToken != "":
		return nil, fmt.Errorf("gocode: anthropic Config: set APIKey or OAuthToken, not both")
	case cfg.APIKey == "" && cfg.OAuthToken == "":
		return nil, fmt.Errorf("gocode: anthropic Config: APIKey or OAuthToken is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = anthropicDefaultBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Provider{cfg: cfg}, nil
}

// NewProviderFromEnv creates a Provider using the ANTHROPIC_API_KEY
// environment variable. Returns an error if the variable is unset or empty.
// Use this when you need to supply a custom Config to gocode.New; otherwise
// prefer NewClientFromEnv. For Claude subscription auth, see
// LoadClaudeCredentials and NewProvider with Config.OAuthToken.
func NewProviderFromEnv() (*Provider, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("gocode: ANTHROPIC_API_KEY environment variable is not set")
	}
	return NewProvider(Config{APIKey: key})
}

// useOAuth reports whether this provider authenticates via a Claude
// subscription OAuth token rather than an API key.
func (p *Provider) useOAuth() bool { return p.cfg.OAuthToken != "" }

// applyAuthHeaders writes the auth-related headers appropriate to whichever
// credential mode this provider was constructed with.
func (p *Provider) applyAuthHeaders(h http.Header) {
	if p.useOAuth() {
		h.Set("authorization", "Bearer "+p.cfg.OAuthToken)
		h.Set("anthropic-beta", oauthBetaHeader)
		return
	}
	h.Set("x-api-key", p.cfg.APIKey)
}

// NewClientFromEnv creates a Client backed by the Anthropic provider,
// reading the API key from ANTHROPIC_API_KEY. model is the model identifier;
// pass gocode.ModelOpus, gocode.ModelSonnet, gocode.ModelHaiku, or any Anthropic model string.
// For custom retry, MaxTokens, or HTTP client settings, use
// NewProviderFromEnv + gocode.New instead.
func NewClientFromEnv(model string) (*gocode.Client, error) {
	provider, err := NewProviderFromEnv()
	if err != nil {
		return nil, err
	}
	return gocode.New(gocode.Config{Provider: provider, Model: model})
}

// anthropicRequest is the JSON body sent to POST /v1/messages.
// Tools is a pre-serialized slice so we can mix standard tool declarations
// (Tool.MarshalJSON output) with the typed declaration form used by category-1
// server tools (web_search, code_execution) and category-2 client-executed
// tools (bash, text_editor, computer) — both encoded as opaque JSON that the
// Anthropic API accepts directly.
//
// System is `any` so it can be emitted either as the simple string form or,
// when a cache breakpoint is set, as an array of typed text blocks. With
// json:"omitempty" the nil interface is dropped from the request entirely.
type anthropicRequest struct {
	Model     string            `json:"model"`
	MaxTokens int               `json:"max_tokens"`
	System    any               `json:"system,omitempty"`
	Messages  []gocode.Message  `json:"messages"`
	Tools     []json.RawMessage `json:"tools,omitempty"`
	Stream    bool              `json:"stream,omitempty"`
}

// anthropicSystemBlock is the array-form payload used when the system
// prompt carries a cache breakpoint. Anthropic accepts either a plain
// string or a [{type, text, cache_control}] array — we only need the
// text variant.
type anthropicSystemBlock struct {
	Type         string               `json:"type"`
	Text         string               `json:"text"`
	CacheControl *gocode.CacheControl `json:"cache_control,omitempty"`
}

// anthropicSystem returns the value to assign to anthropicRequest.System.
// When SystemCache is set and the prompt is non-empty, emit the array form
// so cache_control rides along; otherwise emit a plain string (or nil for
// an empty prompt, which omitempty drops).
//
// When oauth is true, the array form is always used and the Claude Code
// identity block is prepended — Anthropic's OAuth scope only authorizes
// requests that identify as Claude Code, so omitting the prefix yields
// auth errors. Any cache breakpoint is attached to the caller's prompt
// block (kept stable per session) rather than the identity block.
func anthropicSystem(text string, cache *gocode.CacheControl, oauth bool) any {
	if !oauth {
		if text == "" {
			return nil
		}
		if cache != nil {
			return []anthropicSystemBlock{{Type: "text", Text: text, CacheControl: cache}}
		}
		return text
	}
	blocks := []anthropicSystemBlock{{Type: "text", Text: claudeCodeIdentityPrompt}}
	if text != "" {
		b := anthropicSystemBlock{Type: "text", Text: text}
		if cache != nil {
			b.CacheControl = cache
		}
		blocks = append(blocks, b)
	} else if cache != nil {
		blocks[0].CacheControl = cache
	}
	return blocks
}

// ProviderTag is the value gocode.Tool.Provider and gocode.ProviderTool.Provider
// must carry for an entry to be accepted by this provider. Exported so the
// constructors in tools.go (and any third-party constructors) can stamp it.
const ProviderTag = "anthropic"

// buildTools merges local gocode.Tool declarations and provider-side
// (category-1) gocode.ProviderTool entries into the wire []json.RawMessage
// that becomes the Anthropic request's "tools" array. Any entry tagged for a
// different provider is rejected so misuse fails loudly at request build.
func buildTools(tools []gocode.Tool, providerTools []gocode.ProviderTool) ([]json.RawMessage, error) {
	if len(tools) == 0 && len(providerTools) == 0 {
		return nil, nil
	}
	out := make([]json.RawMessage, 0, len(tools)+len(providerTools))
	for _, t := range tools {
		if t.Provider != "" && t.Provider != ProviderTag {
			return nil, fmt.Errorf("gocode: anthropic: tool %q is tagged for provider %q", t.Name, t.Provider)
		}
		raw, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("gocode: anthropic: marshal tool %q: %w", t.Name, err)
		}
		out = append(out, raw)
	}
	for i, pt := range providerTools {
		if pt.Provider != ProviderTag {
			return nil, fmt.Errorf("gocode: anthropic: provider tool [%d] is tagged for provider %q", i, pt.Provider)
		}
		if len(pt.Raw) == 0 {
			return nil, fmt.Errorf("gocode: anthropic: provider tool [%d] has empty Raw", i)
		}
		out = append(out, pt.Raw)
	}
	return out, nil
}

// anthropicResponse is the parsed reply from the Anthropic API.
type anthropicResponse struct {
	ID         string         `json:"id"`
	Content    []gocode.ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      gocode.Usage          `json:"usage"`
}

type apiErrorBody struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Call implements Provider.
func (p *Provider) Call(ctx context.Context, req gocode.ProviderRequest) (gocode.ProviderResponse, error) {
	tools, err := buildTools(req.Tools, req.ProviderTools)
	if err != nil {
		return gocode.ProviderResponse{}, err
	}
	wireReq := anthropicRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		System:    anthropicSystem(req.System, req.SystemCache, p.useOAuth()),
		Messages:  req.Messages,
		Tools:     tools,
	}

	body, err := json.Marshal(wireReq)
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: anthropic: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	p.applyAuthHeaders(httpReq.Header)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: anthropic: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody apiErrorBody
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return gocode.ProviderResponse{}, &gocode.APIError{
			StatusCode: resp.StatusCode,
			Type:       errBody.Error.Type,
			Message:    errBody.Error.Message,
		}
	}

	var result anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: anthropic: decode response: %w", err)
	}

	return gocode.ProviderResponse{
		Content:    result.Content,
		StopReason: result.StopReason,
		Usage:      result.Usage,
	}, nil
}

// Stream implements Provider.Stream for Anthropic using its SSE streaming API.
// Parses common events (content_block_start, content_block_delta for text/partial_json,
// message_delta) via map unmarshaling. Calls onDelta synchronously for text deltas
// and partial tool_use blocks. Accumulates full content/usage for the final
// gocode.ProviderResponse (handles one tool per turn for simplicity). Mirrors Call's
// error handling, headers, and request shape (with stream=true).
func (p *Provider) Stream(ctx context.Context, req gocode.ProviderRequest, onDelta func(gocode.ContentBlock)) (gocode.ProviderResponse, error) {
	tools, err := buildTools(req.Tools, req.ProviderTools)
	if err != nil {
		return gocode.ProviderResponse{}, err
	}
	wireReq := anthropicRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		System:    anthropicSystem(req.System, req.SystemCache, p.useOAuth()),
		Messages:  req.Messages,
		Tools:     tools,
		Stream:    true,
	}

	body, err := json.Marshal(wireReq)
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: anthropic: marshal stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: anthropic: build stream request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	p.applyAuthHeaders(httpReq.Header)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("accept", "text/event-stream")

	resp, err := p.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: anthropic: stream http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody apiErrorBody
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return gocode.ProviderResponse{}, &gocode.APIError{
			StatusCode: resp.StatusCode,
			Type:       errBody.Error.Type,
			Message:    errBody.Error.Message,
		}
	}

	var textBuilder strings.Builder
	var toolInputBuilder strings.Builder
	var toolID, toolName string
	var stopReason string
	var usage gocode.Usage
	var content []gocode.ContentBlock

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if strings.TrimSpace(data) == "[DONE]" {
			break
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if typ, ok := event["type"].(string); ok {
			switch typ {
			case "message_start":
				// Input token count and cache stats are sent in message_start.
				if msg, ok := event["message"].(map[string]interface{}); ok {
					if u, ok := msg["usage"].(map[string]interface{}); ok {
						readAnthropicUsage(u, &usage)
					}
				}
			case "error":
				if errMap, ok := event["error"].(map[string]interface{}); ok {
					errType, _ := errMap["type"].(string)
					errMsg, _ := errMap["message"].(string)
					return gocode.ProviderResponse{}, &gocode.APIError{Type: errType, Message: errMsg}
				}
			case "content_block_start":
				if cb, ok := event["content_block"].(map[string]interface{}); ok {
					if t, _ := cb["type"].(string); t == gocode.TypeToolUse {
						if id, _ := cb["id"].(string); id != "" {
							toolID = id
						}
						if name, _ := cb["name"].(string); name != "" {
							toolName = name
						}
						toolInputBuilder.Reset()
						onDelta(gocode.ContentBlock{
							Type: gocode.TypeToolUse,
							ID:   toolID,
							Name: toolName,
						})
					}
				}
			case "content_block_delta":
				if delta, ok := event["delta"].(map[string]interface{}); ok {
					if text, ok := delta["text"].(string); ok && text != "" {
						textBuilder.WriteString(text)
						onDelta(gocode.ContentBlock{
							Type: gocode.TypeText,
							Text: text,
						})
					}
					if partial, ok := delta["partial_json"].(string); ok && partial != "" {
						toolInputBuilder.WriteString(partial)
						if toolID != "" {
							onDelta(gocode.ContentBlock{
								Type:  gocode.TypeToolUse,
								ID:    toolID,
								Name:  toolName,
								Input: json.RawMessage(toolInputBuilder.String()),
							})
						}
					}
				}
			case "message_delta":
				if delta, ok := event["delta"].(map[string]interface{}); ok {
					if reason, ok := delta["stop_reason"].(string); ok && reason != "" {
						stopReason = reason
					}
				}
				if u, ok := event["usage"].(map[string]interface{}); ok {
					readAnthropicUsage(u, &usage)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: anthropic stream read: %w", err)
	}

	if textBuilder.Len() > 0 {
		content = append(content, gocode.ContentBlock{
			Type: gocode.TypeText,
			Text: textBuilder.String(),
		})
	}
	if toolID != "" {
		input := json.RawMessage(`{}`)
		if s := toolInputBuilder.String(); s != "" {
			input = json.RawMessage(s)
		}
		content = append(content, gocode.ContentBlock{
			Type:  gocode.TypeToolUse,
			ID:    toolID,
			Name:  toolName,
			Input: input,
		})
	}
	if stopReason == "" {
		if toolID != "" {
			stopReason = "tool_use"
		} else {
			stopReason = "end_turn"
		}
	}

	return gocode.ProviderResponse{
		Content:    content,
		StopReason: stopReason,
		Usage:      usage,
	}, nil
}

// readAnthropicUsage extracts token counts from a streaming usage object.
// Anthropic only sends fields that are non-zero, so missing keys leave the
// usage struct unchanged. Cache stats (cache_creation_input_tokens and
// cache_read_input_tokens) are merged in alongside input/output tokens.
func readAnthropicUsage(u map[string]interface{}, usage *gocode.Usage) {
	if v, ok := u["input_tokens"].(float64); ok {
		usage.InputTokens = int(v)
	}
	if v, ok := u["output_tokens"].(float64); ok {
		usage.OutputTokens = int(v)
	}
	if v, ok := u["cache_creation_input_tokens"].(float64); ok {
		usage.CacheCreationTokens = int(v)
	}
	if v, ok := u["cache_read_input_tokens"].(float64); ok {
		usage.CacheReadTokens = int(v)
	}
}
