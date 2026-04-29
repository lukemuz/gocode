package openai

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

	"github.com/lukemuz/gocode"
)

const (
	openAIDefaultBaseURL = "https://api.openai.com"
	defaultHTTPTimeout   = 60 * time.Second
)

// ---------------------------------------------------------------------------
// OpenAI-compatible wire types (reused by OpenRouterProvider as well)
// ---------------------------------------------------------------------------

// openAIChatRequest is the JSON body for OpenAI-compatible chat completions.
type openAIChatRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens,omitempty"`
	Messages  []openAIMessage `json:"messages"`
	Tools     []openAIToolDef `json:"tools,omitempty"`
	Stream    bool            `json:"stream,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // always "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIToolDef struct {
	Type     string `json:"type"` // always "function"
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openAIErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content,omitempty"`
			ToolCalls []struct {
				Index int `json:"index,omitempty"`
				ID    string `json:"id,omitempty"`
				Type  string `json:"type,omitempty"`
				Function struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				} `json:"function,omitempty"`
			} `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens,omitempty"`
		CompletionTokens int `json:"completion_tokens,omitempty"`
	} `json:"usage,omitempty"`
}

// ---------------------------------------------------------------------------
// Shared conversion helpers
// ---------------------------------------------------------------------------

// toOpenAIMessages converts canonical Messages to OpenAI wire format.
// system is prepended as a system-role message if non-empty.
func toOpenAIMessages(system string, messages []gocode.Message) []openAIMessage {
	var out []openAIMessage

	if system != "" {
		out = append(out, openAIMessage{Role: "system", Content: system})
	}

	for _, msg := range messages {
		switch msg.Role {
		case gocode.RoleAssistant:
			m := openAIMessage{Role: gocode.RoleAssistant}
			var textParts []string
			for _, block := range msg.Content {
				switch block.Type {
				case gocode.TypeText:
					textParts = append(textParts, block.Text)
				case gocode.TypeToolUse:
					args := string(block.Input)
					if args == "" {
						args = "{}"
					}
					tc := openAIToolCall{
						ID:   block.ID,
						Type: "function",
					}
					tc.Function.Name = block.Name
					tc.Function.Arguments = args
					m.ToolCalls = append(m.ToolCalls, tc)
				}
			}
			if len(textParts) > 0 {
				m.Content = strings.Join(textParts, "")
			}
			out = append(out, m)

		case gocode.RoleUser:
			// Separate tool_result blocks into individual tool-role messages;
			// collect remaining text blocks into a single user message.
			var textParts []string
			for _, block := range msg.Content {
				if block.Type == gocode.TypeToolResult {
					out = append(out, openAIMessage{
						Role:       "tool",
						ToolCallID: block.ToolUseID,
						Content:    block.Content,
					})
				} else if block.Type == gocode.TypeText {
					textParts = append(textParts, block.Text)
				}
			}
			if len(textParts) > 0 {
				out = append(out, openAIMessage{
					Role:    gocode.RoleUser,
					Content: strings.Join(textParts, ""),
				})
			}
		}
	}

	return out
}

// toOpenAITools converts canonical Tools to OpenAI function-calling format.
func toOpenAITools(tools []gocode.Tool) []openAIToolDef {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openAIToolDef, len(tools))
	for i, t := range tools {
		var td openAIToolDef
		td.Type = "function"
		td.Function.Name = t.Name
		td.Function.Description = t.Description
		td.Function.Parameters = t.InputSchema
		out[i] = td
	}
	return out
}

// openAIFinishReason maps an OpenAI finish_reason to canonical StopReason.
func openAIFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return reason
	}
}

// fromOpenAIResponse converts an openAIChatResponse to a gocode.ProviderResponse.
func fromOpenAIResponse(r openAIChatResponse) gocode.ProviderResponse {
	if len(r.Choices) == 0 {
		return gocode.ProviderResponse{}
	}

	choice := r.Choices[0]
	var content []gocode.ContentBlock

	if choice.Message.Content != "" {
		content = append(content, gocode.ContentBlock{
			Type: gocode.TypeText,
			Text: choice.Message.Content,
		})
	}

	for _, tc := range choice.Message.ToolCalls {
		content = append(content, gocode.ContentBlock{
			Type:  gocode.TypeToolUse,
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(tc.Function.Arguments),
		})
	}

	return gocode.ProviderResponse{
		Content:    content,
		StopReason: openAIFinishReason(choice.FinishReason),
		Usage: gocode.Usage{
			InputTokens:  r.Usage.PromptTokens,
			OutputTokens: r.Usage.CompletionTokens,
		},
	}
}

// ---------------------------------------------------------------------------
// Config and Provider
// ---------------------------------------------------------------------------

// Config holds configuration for the OpenAI provider.
type Config struct {
	APIKey     string       // required
	BaseURL    string       // defaults to https://api.openai.com
	HTTPClient *http.Client // defaults to a 60-second timeout client
}

// Provider implements Provider for the OpenAI Chat Completions API.
type Provider struct {
	cfg Config
}

// NewProvider creates an Provider, filling in defaults.
func NewProvider(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("gocode: Config.APIKey is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = openAIDefaultBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Provider{cfg: cfg}, nil
}

// NewProviderFromEnv creates an Provider using the OPENAI_API_KEY
// environment variable. Returns an error if the variable is unset or empty.
func NewProviderFromEnv() (*Provider, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("gocode: OPENAI_API_KEY environment variable is not set")
	}
	return NewProvider(Config{APIKey: key})
}

// NewClientFromEnv creates a Client backed by the OpenAI provider,
// reading the API key from OPENAI_API_KEY. model is the model identifier
// (e.g. "gpt-4o", "gpt-4o-mini").
func NewClientFromEnv(model string) (*gocode.Client, error) {
	provider, err := NewProviderFromEnv()
	if err != nil {
		return nil, err
	}
	return gocode.New(gocode.Config{Provider: provider, Model: model})
}

// Call implements Provider for OpenAI.
func (p *Provider) Call(ctx context.Context, req gocode.ProviderRequest) (gocode.ProviderResponse, error) {
	return CompatibleCall(ctx, p.cfg.HTTPClient, p.cfg.APIKey, p.cfg.BaseURL+"/v1/chat/completions", req)
}

// Stream implements Provider.Stream for OpenAI by delegating to the shared
// streaming helper (mirrors the Call pattern).
func (p *Provider) Stream(ctx context.Context, req gocode.ProviderRequest, onDelta func(gocode.ContentBlock)) (gocode.ProviderResponse, error) {
	return CompatibleStream(ctx, p.cfg.HTTPClient, p.cfg.APIKey, p.cfg.BaseURL+"/v1/chat/completions", req, onDelta)
}

// ---------------------------------------------------------------------------
// Shared HTTP call helper for OpenAI-compatible endpoints
// ---------------------------------------------------------------------------

// Call marshals req into an OpenAI chat completions body,
// POSTs it to url with the given Bearer key, and decodes the response.
func CompatibleCall(
	ctx context.Context,
	httpClient *http.Client,
	apiKey string,
	url string,
	req gocode.ProviderRequest,
) (gocode.ProviderResponse, error) {
	chatReq := openAIChatRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Messages:  toOpenAIMessages(req.System, req.Messages),
		Tools:     toOpenAITools(req.Tools),
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: marshal openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: build openai request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: openai http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody openAIErrorBody
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return gocode.ProviderResponse{}, &gocode.APIError{
			StatusCode: resp.StatusCode,
			Type:       errBody.Error.Type,
			Message:    errBody.Error.Message,
		}
	}

	var chatResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: decode openai response: %w", err)
	}

	return fromOpenAIResponse(chatResp), nil
}

// Stream marshals req (with Stream=true), POSTs to the
// OpenAI-compatible endpoint with Accept: text/event-stream header, handles
// non-200 errors identically to the non-stream version, then uses
// bufio.Scanner to parse "data: " JSON chunks (skipping "[DONE]").
// It calls onDelta for each text delta and each tool_call delta (accumulating
// arguments per index from the deltas), accumulates full text and tool calls
// for the final gocode.ProviderResponse.Content (mirroring fromOpenAIResponse logic),
// maps finish_reason via openAIFinishReason and captures usage, then returns
// the aggregated response (or a stream read error).
func CompatibleStream(
	ctx context.Context,
	httpClient *http.Client,
	apiKey string,
	url string,
	req gocode.ProviderRequest,
	onDelta func(gocode.ContentBlock),
) (gocode.ProviderResponse, error) {
	chatReq := openAIChatRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Messages:  toOpenAIMessages(req.System, req.Messages),
		Tools:     toOpenAITools(req.Tools),
		Stream:    true,
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: marshal openai stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: build openai stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: openai stream http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody openAIErrorBody
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return gocode.ProviderResponse{}, &gocode.APIError{
			StatusCode: resp.StatusCode,
			Type:       errBody.Error.Type,
			Message:    errBody.Error.Message,
		}
	}

	var fullText strings.Builder
	toolCallAccum := make(map[int]openAIToolCall)
	var stopReason string
	var usage gocode.Usage

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

		// Some providers embed error objects in the stream body even on HTTP 200.
		var errCheck openAIErrorBody
		if err := json.Unmarshal([]byte(data), &errCheck); err == nil && errCheck.Error.Message != "" {
			return gocode.ProviderResponse{}, &gocode.APIError{Type: errCheck.Error.Type, Message: errCheck.Error.Message}
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
				usage.InputTokens = chunk.Usage.PromptTokens
				usage.OutputTokens = chunk.Usage.CompletionTokens
			}
			continue
		}

		choice := chunk.Choices[0]

		if choice.Delta.Content != "" {
			delta := choice.Delta.Content
			fullText.WriteString(delta)
			onDelta(gocode.ContentBlock{
				Type: gocode.TypeText,
				Text: delta,
			})
		}

		for _, tcd := range choice.Delta.ToolCalls {
			idx := tcd.Index
			tc := toolCallAccum[idx]
			if tcd.ID != "" {
				tc.ID = tcd.ID
			}
			if tcd.Type != "" {
				tc.Type = tcd.Type
			}
			if tcd.Function.Name != "" {
				tc.Function.Name = tcd.Function.Name
			}
			if tcd.Function.Arguments != "" {
				tc.Function.Arguments += tcd.Function.Arguments
			}
			toolCallAccum[idx] = tc

			cb := gocode.ContentBlock{
				Type: gocode.TypeToolUse,
				ID:   tc.ID,
				Name: tc.Function.Name,
			}
			if tc.Function.Arguments != "" {
				cb.Input = json.RawMessage(tc.Function.Arguments)
			}
			onDelta(cb)
		}

		if choice.FinishReason != nil && *choice.FinishReason != "" {
			stopReason = openAIFinishReason(*choice.FinishReason)
		}

		if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			usage.InputTokens = chunk.Usage.PromptTokens
			usage.OutputTokens = chunk.Usage.CompletionTokens
		}
	}

	if err := scanner.Err(); err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: openai stream read: %w", err)
	}

	// Build final content (mirrors fromOpenAIResponse).
	var content []gocode.ContentBlock
	if fullText.Len() > 0 {
		content = append(content, gocode.ContentBlock{
			Type: gocode.TypeText,
			Text: fullText.String(),
		})
	}
	for _, tc := range toolCallAccum {
		var input json.RawMessage
		if tc.Function.Arguments != "" {
			input = json.RawMessage(tc.Function.Arguments)
		}
		content = append(content, gocode.ContentBlock{
			Type:  gocode.TypeToolUse,
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	if stopReason == "" {
		if len(toolCallAccum) > 0 {
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
