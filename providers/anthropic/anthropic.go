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
)

// Config holds configuration for the Anthropic provider.
type Config struct {
	APIKey     string       // required
	BaseURL    string       // defaults to https://api.anthropic.com
	HTTPClient *http.Client // defaults to a 60-second timeout client
}

// Provider implements Provider for the Anthropic Messages API.
type Provider struct {
	cfg Config
}

// NewProvider creates an Provider, filling in defaults.
// Returns an error if APIKey is empty.
func NewProvider(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("gocode: Config.APIKey is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = anthropicDefaultBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Provider{cfg: cfg}, nil
}

// NewProviderFromEnv creates an Provider using the
// ANTHROPIC_API_KEY environment variable. Returns an error if the variable
// is unset or empty. Use this when you need to supply a custom Config to
// gocode.New; otherwise prefer NewClientFromEnv.
func NewProviderFromEnv() (*Provider, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("gocode: ANTHROPIC_API_KEY environment variable is not set")
	}
	return NewProvider(Config{APIKey: key})
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
type anthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []gocode.Message `json:"messages"`
	Tools     []gocode.Tool    `json:"tools,omitempty"`
	Stream    bool      `json:"stream,omitempty"`
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
	wireReq := anthropicRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		System:    req.System,
		Messages:  req.Messages,
		Tools:     req.Tools,
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
	httpReq.Header.Set("x-api-key", p.cfg.APIKey)
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
	wireReq := anthropicRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		System:    req.System,
		Messages:  req.Messages,
		Tools:     req.Tools,
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
	httpReq.Header.Set("x-api-key", p.cfg.APIKey)
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
