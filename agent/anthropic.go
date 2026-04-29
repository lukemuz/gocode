package agent

import (
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
)

// AnthropicConfig holds configuration for the Anthropic provider.
type AnthropicConfig struct {
	APIKey     string       // required
	BaseURL    string       // defaults to https://api.anthropic.com
	HTTPClient *http.Client // defaults to a 60-second timeout client
}

// AnthropicProvider implements Provider for the Anthropic Messages API.
type AnthropicProvider struct {
	cfg AnthropicConfig
}

// NewAnthropicProvider creates an AnthropicProvider, filling in defaults.
// Returns an error if APIKey is empty.
func NewAnthropicProvider(cfg AnthropicConfig) (*AnthropicProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("agent: AnthropicConfig.APIKey is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = anthropicDefaultBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &AnthropicProvider{cfg: cfg}, nil
}

// NewAnthropicProviderFromEnv creates an AnthropicProvider using the
// ANTHROPIC_API_KEY environment variable. Returns an error if the variable
// is unset or empty. Use this when you need to supply a custom Config to
// agent.New; otherwise prefer NewAnthropicClientFromEnv.
func NewAnthropicProviderFromEnv() (*AnthropicProvider, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("agent: ANTHROPIC_API_KEY environment variable is not set")
	}
	return NewAnthropicProvider(AnthropicConfig{APIKey: key})
}

// NewAnthropicClientFromEnv creates a Client backed by the Anthropic provider,
// reading the API key from ANTHROPIC_API_KEY. model is the model identifier;
// pass ModelOpus, ModelSonnet, ModelHaiku, or any Anthropic model string.
// For custom retry, MaxTokens, or HTTP client settings, use
// NewAnthropicProviderFromEnv + agent.New instead.
func NewAnthropicClientFromEnv(model string) (*Client, error) {
	provider, err := NewAnthropicProviderFromEnv()
	if err != nil {
		return nil, err
	}
	return New(Config{Provider: provider, Model: model})
}

// anthropicRequest is the JSON body sent to POST /v1/messages.
// Tools is a pre-serialized slice so we can mix standard tool declarations
// (Tool.MarshalJSON output) with the typed declaration form used by category-1
// server tools (web_search, code_execution) and category-2 client-executed
// tools (bash, text_editor, computer) — both encoded as opaque JSON that the
// Anthropic API accepts directly.
type anthropicRequest struct {
	Model     string            `json:"model"`
	MaxTokens int               `json:"max_tokens"`
	System    string            `json:"system,omitempty"`
	Messages  []Message         `json:"messages"`
	Tools     []json.RawMessage `json:"tools,omitempty"`
	Stream    bool              `json:"stream,omitempty"`
}

// providerTagAnthropic is the value Tool.Provider / ProviderTool.Provider
// must carry for an entry to be accepted by the Anthropic provider.
const providerTagAnthropic = "anthropic"

// buildAnthropicTools merges local Tool declarations and provider-side
// (category-1) ProviderTool entries into the wire []json.RawMessage that
// becomes the Anthropic request's "tools" array. Any entry tagged for a
// different provider is rejected so misuse fails loudly at request build.
func buildAnthropicTools(tools []Tool, providerTools []ProviderTool) ([]json.RawMessage, error) {
	if len(tools) == 0 && len(providerTools) == 0 {
		return nil, nil
	}
	out := make([]json.RawMessage, 0, len(tools)+len(providerTools))
	for _, t := range tools {
		if t.Provider != "" && t.Provider != providerTagAnthropic {
			return nil, fmt.Errorf("agent: anthropic: tool %q is tagged for provider %q", t.Name, t.Provider)
		}
		raw, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("agent: anthropic: marshal tool %q: %w", t.Name, err)
		}
		out = append(out, raw)
	}
	for i, pt := range providerTools {
		if pt.Provider != providerTagAnthropic {
			return nil, fmt.Errorf("agent: anthropic: provider tool [%d] is tagged for provider %q", i, pt.Provider)
		}
		if len(pt.Raw) == 0 {
			return nil, fmt.Errorf("agent: anthropic: provider tool [%d] has empty Raw", i)
		}
		out = append(out, pt.Raw)
	}
	return out, nil
}

// anthropicResponse is the parsed reply from the Anthropic API.
type anthropicResponse struct {
	ID         string         `json:"id"`
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
}

type apiErrorBody struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Call implements Provider.
func (p *AnthropicProvider) Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	tools, err := buildAnthropicTools(req.Tools, req.ProviderTools)
	if err != nil {
		return ProviderResponse{}, err
	}
	wireReq := anthropicRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		System:    req.System,
		Messages:  req.Messages,
		Tools:     tools,
	}

	body, err := json.Marshal(wireReq)
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: anthropic: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", p.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: anthropic: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody apiErrorBody
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return ProviderResponse{}, &APIError{
			StatusCode: resp.StatusCode,
			Type:       errBody.Error.Type,
			Message:    errBody.Error.Message,
		}
	}

	var result anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: anthropic: decode response: %w", err)
	}

	return ProviderResponse{
		Content:    result.Content,
		StopReason: result.StopReason,
		Usage:      result.Usage,
	}, nil
}

// Stream implements Provider.Stream for Anthropic using its SSE streaming API.
// Parses common events (content_block_start, content_block_delta for text/partial_json,
// message_delta) via map unmarshaling. Calls onDelta synchronously for text deltas
// and partial tool_use blocks. Accumulates full content/usage for the final
// ProviderResponse (handles one tool per turn for simplicity). Mirrors Call's
// error handling, headers, and request shape (with stream=true).
func (p *AnthropicProvider) Stream(ctx context.Context, req ProviderRequest, onDelta func(ContentBlock)) (ProviderResponse, error) {
	tools, err := buildAnthropicTools(req.Tools, req.ProviderTools)
	if err != nil {
		return ProviderResponse{}, err
	}
	wireReq := anthropicRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		System:    req.System,
		Messages:  req.Messages,
		Tools:     tools,
		Stream:    true,
	}

	body, err := json.Marshal(wireReq)
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: anthropic: marshal stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: anthropic: build stream request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", p.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("accept", "text/event-stream")

	resp, err := p.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: anthropic: stream http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody apiErrorBody
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return ProviderResponse{}, &APIError{
			StatusCode: resp.StatusCode,
			Type:       errBody.Error.Type,
			Message:    errBody.Error.Message,
		}
	}

	var textBuilder strings.Builder
	var toolInputBuilder strings.Builder
	var toolID, toolName string
	var stopReason string
	var usage Usage
	var content []ContentBlock

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
				// Input token count is only sent in the message_start event.
				if msg, ok := event["message"].(map[string]interface{}); ok {
					if u, ok := msg["usage"].(map[string]interface{}); ok {
						if in, ok := u["input_tokens"].(float64); ok {
							usage.InputTokens = int(in)
						}
					}
				}
			case "error":
				if errMap, ok := event["error"].(map[string]interface{}); ok {
					errType, _ := errMap["type"].(string)
					errMsg, _ := errMap["message"].(string)
					return ProviderResponse{}, &APIError{Type: errType, Message: errMsg}
				}
			case "content_block_start":
				if cb, ok := event["content_block"].(map[string]interface{}); ok {
					if t, _ := cb["type"].(string); t == TypeToolUse {
						if id, _ := cb["id"].(string); id != "" {
							toolID = id
						}
						if name, _ := cb["name"].(string); name != "" {
							toolName = name
						}
						toolInputBuilder.Reset()
						onDelta(ContentBlock{
							Type: TypeToolUse,
							ID:   toolID,
							Name: toolName,
						})
					}
				}
			case "content_block_delta":
				if delta, ok := event["delta"].(map[string]interface{}); ok {
					if text, ok := delta["text"].(string); ok && text != "" {
						textBuilder.WriteString(text)
						onDelta(ContentBlock{
							Type: TypeText,
							Text: text,
						})
					}
					if partial, ok := delta["partial_json"].(string); ok && partial != "" {
						toolInputBuilder.WriteString(partial)
						if toolID != "" {
							onDelta(ContentBlock{
								Type:  TypeToolUse,
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
					if in, ok := u["input_tokens"].(float64); ok {
						usage.InputTokens = int(in)
					}
					if out, ok := u["output_tokens"].(float64); ok {
						usage.OutputTokens = int(out)
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: anthropic stream read: %w", err)
	}

	if textBuilder.Len() > 0 {
		content = append(content, ContentBlock{
			Type: TypeText,
			Text: textBuilder.String(),
		})
	}
	if toolID != "" {
		input := json.RawMessage(`{}`)
		if s := toolInputBuilder.String(); s != "" {
			input = json.RawMessage(s)
		}
		content = append(content, ContentBlock{
			Type:  TypeToolUse,
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

	return ProviderResponse{
		Content:    content,
		StopReason: stopReason,
		Usage:      usage,
	}, nil
}
