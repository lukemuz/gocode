package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const openAIDefaultBaseURL = "https://api.openai.com"

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
func toOpenAIMessages(system string, messages []Message) []openAIMessage {
	var out []openAIMessage

	if system != "" {
		out = append(out, openAIMessage{Role: "system", Content: system})
	}

	for _, msg := range messages {
		switch msg.Role {
		case RoleAssistant:
			m := openAIMessage{Role: RoleAssistant}
			var textParts []string
			for _, block := range msg.Content {
				switch block.Type {
				case TypeText:
					textParts = append(textParts, block.Text)
				case TypeToolUse:
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

		case RoleUser:
			// Separate tool_result blocks into individual tool-role messages;
			// collect remaining text blocks into a single user message.
			var textParts []string
			for _, block := range msg.Content {
				if block.Type == TypeToolResult {
					out = append(out, openAIMessage{
						Role:       "tool",
						ToolCallID: block.ToolUseID,
						Content:    block.Content,
					})
				} else if block.Type == TypeText {
					textParts = append(textParts, block.Text)
				}
			}
			if len(textParts) > 0 {
				out = append(out, openAIMessage{
					Role:    RoleUser,
					Content: strings.Join(textParts, ""),
				})
			}
		}
	}

	return out
}

// toOpenAITools converts canonical Tools to OpenAI function-calling format.
func toOpenAITools(tools []Tool) []openAIToolDef {
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

// fromOpenAIResponse converts an openAIChatResponse to a ProviderResponse.
func fromOpenAIResponse(r openAIChatResponse) ProviderResponse {
	if len(r.Choices) == 0 {
		return ProviderResponse{}
	}

	choice := r.Choices[0]
	var content []ContentBlock

	if choice.Message.Content != "" {
		content = append(content, ContentBlock{
			Type: TypeText,
			Text: choice.Message.Content,
		})
	}

	for _, tc := range choice.Message.ToolCalls {
		content = append(content, ContentBlock{
			Type:  TypeToolUse,
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(tc.Function.Arguments),
		})
	}

	return ProviderResponse{
		Content:    content,
		StopReason: openAIFinishReason(choice.FinishReason),
		Usage: Usage{
			InputTokens:  r.Usage.PromptTokens,
			OutputTokens: r.Usage.CompletionTokens,
		},
	}
}

// ---------------------------------------------------------------------------
// OpenAIConfig and OpenAIProvider
// ---------------------------------------------------------------------------

// OpenAIConfig holds configuration for the OpenAI provider.
type OpenAIConfig struct {
	APIKey     string       // required
	BaseURL    string       // defaults to https://api.openai.com
	HTTPClient *http.Client // defaults to a 60-second timeout client
}

// OpenAIProvider implements Provider for the OpenAI Chat Completions API.
type OpenAIProvider struct {
	cfg OpenAIConfig
}

// NewOpenAIProvider creates an OpenAIProvider, filling in defaults.
func NewOpenAIProvider(cfg OpenAIConfig) (*OpenAIProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("agent: OpenAIConfig.APIKey is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = openAIDefaultBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &OpenAIProvider{cfg: cfg}, nil
}

// Call implements Provider for OpenAI.
func (p *OpenAIProvider) Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	return doOpenAICompatibleCall(ctx, p.cfg.HTTPClient, p.cfg.APIKey, p.cfg.BaseURL+"/v1/chat/completions", req)
}

// Stream implements Provider.Stream for OpenAI by delegating to the shared
// streaming helper (mirrors the Call pattern).
func (p *OpenAIProvider) Stream(ctx context.Context, req ProviderRequest, onDelta func(ContentBlock)) (ProviderResponse, error) {
	return doOpenAICompatibleStream(ctx, p.cfg.HTTPClient, p.cfg.APIKey, p.cfg.BaseURL+"/v1/chat/completions", req, onDelta)
}

// ---------------------------------------------------------------------------
// Shared HTTP call helper for OpenAI-compatible endpoints
// ---------------------------------------------------------------------------

// doOpenAICompatibleCall marshals req into an OpenAI chat completions body,
// POSTs it to url with the given Bearer key, and decodes the response.
func doOpenAICompatibleCall(
	ctx context.Context,
	httpClient *http.Client,
	apiKey string,
	url string,
	req ProviderRequest,
) (ProviderResponse, error) {
	chatReq := openAIChatRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Messages:  toOpenAIMessages(req.System, req.Messages),
		Tools:     toOpenAITools(req.Tools),
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: marshal openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: build openai request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: openai http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody openAIErrorBody
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return ProviderResponse{}, &APIError{
			StatusCode: resp.StatusCode,
			Type:       errBody.Error.Type,
			Message:    errBody.Error.Message,
		}
	}

	var chatResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: decode openai response: %w", err)
	}

	return fromOpenAIResponse(chatResp), nil
}

// doOpenAICompatibleStream marshals req (with Stream=true), POSTs to the
// OpenAI-compatible endpoint with Accept: text/event-stream header, handles
// non-200 errors identically to the non-stream version, then uses
// bufio.Scanner to parse "data: " JSON chunks (skipping "[DONE]").
// It calls onDelta for each text delta and each tool_call delta (accumulating
// arguments per index from the deltas), accumulates full text and tool calls
// for the final ProviderResponse.Content (mirroring fromOpenAIResponse logic),
// maps finish_reason via openAIFinishReason and captures usage, then returns
// the aggregated response (or a stream read error).
func doOpenAICompatibleStream(
	ctx context.Context,
	httpClient *http.Client,
	apiKey string,
	url string,
	req ProviderRequest,
	onDelta func(ContentBlock),
) (ProviderResponse, error) {
	chatReq := openAIChatRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Messages:  toOpenAIMessages(req.System, req.Messages),
		Tools:     toOpenAITools(req.Tools),
		Stream:    true,
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: marshal openai stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: build openai stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("agent: openai stream http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody openAIErrorBody
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return ProviderResponse{}, &APIError{
			StatusCode: resp.StatusCode,
			Type:       errBody.Error.Type,
			Message:    errBody.Error.Message,
		}
	}

	var fullText strings.Builder
	toolCallAccum := make(map[int]openAIToolCall)
	var stopReason string
	var usage Usage

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
			onDelta(ContentBlock{
				Type: TypeText,
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

			cb := ContentBlock{
				Type: TypeToolUse,
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
		return ProviderResponse{}, fmt.Errorf("agent: openai stream read: %w", err)
	}

	// Build final content (mirrors fromOpenAIResponse).
	var content []ContentBlock
	if fullText.Len() > 0 {
		content = append(content, ContentBlock{
			Type: TypeText,
			Text: fullText.String(),
		})
	}
	for _, tc := range toolCallAccum {
		var input json.RawMessage
		if tc.Function.Arguments != "" {
			input = json.RawMessage(tc.Function.Arguments)
		}
		content = append(content, ContentBlock{
			Type:  TypeToolUse,
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

	return ProviderResponse{
		Content:    content,
		StopReason: stopReason,
		Usage:      usage,
	}, nil
}
