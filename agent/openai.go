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

// openAIMessage is a wire message. Content is `any` so request paths can
// emit either a plain string or a typed-parts array (used to attach
// cache_control on cache-aware backends like OpenRouter); response paths
// receive a string and use messageContentString to coerce.
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

// openAIContentPart is one element in the array form of openAIMessage.Content.
// Only the text variant is supported; cache_control rides along when the
// message was marked at the canonical layer.
type openAIContentPart struct {
	Type         string        `json:"type"` // always "text"
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// messageContentString coerces the polymorphic Content field back to a
// plain string when decoding responses (which always carry string content).
func messageContentString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
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
	// CacheControl, when set, becomes a sibling field on the tool definition.
	// Only emitted when the caller passes cacheCompatible=true (e.g. OpenRouter).
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			// CachedTokens is the OpenAI-compatible report of how many of
			// the prompt tokens were served from the prompt cache. OpenRouter
			// surfaces the same field for routes that support caching.
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
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
		PromptTokens        int `json:"prompt_tokens,omitempty"`
		CompletionTokens    int `json:"completion_tokens,omitempty"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
	} `json:"usage,omitempty"`
}

// ---------------------------------------------------------------------------
// Shared conversion helpers
// ---------------------------------------------------------------------------

// toOpenAIMessages converts canonical Messages to OpenAI wire format.
// system is prepended as a system-role message if non-empty.
//
// cacheCompatible controls whether cache_control markers ride along on the
// wire. When true (e.g. OpenRouter), text content is emitted as a
// typed-parts array so cache_control can attach to the right spot. When
// false (e.g. OpenAI Chat Completions, which auto-caches and may reject
// unknown fields), markers are dropped silently.
func toOpenAIMessages(system string, systemCache *CacheControl, messages []Message, cacheCompatible bool) []openAIMessage {
	var out []openAIMessage

	if system != "" {
		out = append(out, openAIMessage{Role: "system", Content: wireTextContent(system, systemCache, cacheCompatible)})
	}

	for _, msg := range messages {
		switch msg.Role {
		case RoleAssistant:
			m := openAIMessage{Role: RoleAssistant}
			text, cache := joinTextBlocks(msg.Content)
			for _, block := range msg.Content {
				if block.Type != TypeToolUse {
					continue
				}
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
			if text != "" {
				m.Content = wireTextContent(text, cache, cacheCompatible)
			}
			out = append(out, m)

		case RoleUser:
			// Separate tool_result blocks into individual tool-role messages;
			// collect remaining text blocks into a single user message.
			for _, block := range msg.Content {
				if block.Type == TypeToolResult {
					out = append(out, openAIMessage{
						Role:       "tool",
						ToolCallID: block.ToolUseID,
						Content:    block.Content,
					})
				}
			}
			text, cache := joinTextBlocks(msg.Content)
			if text != "" {
				out = append(out, openAIMessage{
					Role:    RoleUser,
					Content: wireTextContent(text, cache, cacheCompatible),
				})
			}
		}
	}

	return out
}

// joinTextBlocks concatenates the text blocks within a Message and returns
// the highest-precedence cache marker among them. If multiple text blocks
// carry markers we keep the last one — for cumulative cache semantics the
// later marker subsumes earlier ones at the same level.
func joinTextBlocks(blocks []ContentBlock) (string, *CacheControl) {
	var b strings.Builder
	var cache *CacheControl
	for _, block := range blocks {
		if block.Type != TypeText {
			continue
		}
		b.WriteString(block.Text)
		if block.CacheControl != nil {
			cache = block.CacheControl
		}
	}
	return b.String(), cache
}

// wireTextContent picks the wire shape: plain string when no cache marker
// applies (or the backend can't carry it), or a single-element typed-parts
// array when we need to attach cache_control.
func wireTextContent(text string, cache *CacheControl, cacheCompatible bool) any {
	if cache == nil || !cacheCompatible {
		return text
	}
	return []openAIContentPart{{Type: "text", Text: text, CacheControl: cache}}
}

// toOpenAITools converts canonical Tools to OpenAI function-calling format.
// Provider-tagged tools (category 2 — Anthropic-only declaration shapes) and
// any ProviderTools (category 1 — server-executed) are rejected: the Chat
// Completions endpoint does not expose hosted tools, and provider-defined
// declaration shapes cannot be translated faithfully. Callers who want
// OpenAI-hosted tools (web_search, file_search, code_interpreter) need a
// Responses-API provider, not Chat Completions.
// toOpenAITools converts canonical Tools to OpenAI function-calling format.
// Provider-tagged tools (category 2 — Anthropic-only declaration shapes) and
// any ProviderTools (category 1 — server-executed) are rejected: the Chat
// Completions endpoint does not expose hosted tools, and provider-defined
// declaration shapes cannot be translated faithfully. Callers who want
// OpenAI-hosted tools (web_search, file_search, code_interpreter) need a
// Responses-API provider, not Chat Completions.
//
// cacheCompatible controls whether Tool.CacheControl markers are emitted
// as a sibling cache_control field on the wire. False for stock OpenAI;
// true for OpenRouter, which routes the marker to Anthropic backends.
func toOpenAITools(tools []Tool, providerTools []ProviderTool, cacheCompatible bool) ([]openAIToolDef, error) {
	if len(providerTools) > 0 {
		return nil, fmt.Errorf("agent: openai (chat completions) does not support ProviderTools; use a Responses-API provider")
	}
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]openAIToolDef, len(tools))
	for i, t := range tools {
		if t.Provider != "" && t.Provider != "openai" {
			return nil, fmt.Errorf("agent: openai: tool %q is tagged for provider %q", t.Name, t.Provider)
		}
		var td openAIToolDef
		td.Type = "function"
		td.Function.Name = t.Name
		td.Function.Description = t.Description
		td.Function.Parameters = t.InputSchema
		if cacheCompatible {
			td.CacheControl = t.CacheControl
		}
		out[i] = td
	}
	return out, nil
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

	if text := messageContentString(choice.Message.Content); text != "" {
		content = append(content, ContentBlock{
			Type: TypeText,
			Text: text,
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
			InputTokens:     r.Usage.PromptTokens,
			OutputTokens:    r.Usage.CompletionTokens,
			CacheReadTokens: r.Usage.PromptTokensDetails.CachedTokens,
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

// NewOpenAIProviderFromEnv creates an OpenAIProvider using the OPENAI_API_KEY
// environment variable. Returns an error if the variable is unset or empty.
func NewOpenAIProviderFromEnv() (*OpenAIProvider, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("agent: OPENAI_API_KEY environment variable is not set")
	}
	return NewOpenAIProvider(OpenAIConfig{APIKey: key})
}

// NewOpenAIClientFromEnv creates a Client backed by the OpenAI provider,
// reading the API key from OPENAI_API_KEY. model is the model identifier
// (e.g. "gpt-4o", "gpt-4o-mini").
func NewOpenAIClientFromEnv(model string) (*Client, error) {
	provider, err := NewOpenAIProviderFromEnv()
	if err != nil {
		return nil, err
	}
	return New(Config{Provider: provider, Model: model})
}

// Call implements Provider for OpenAI.
func (p *OpenAIProvider) Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	return doOpenAICompatibleCall(ctx, p.cfg.HTTPClient, p.cfg.APIKey, p.cfg.BaseURL+"/v1/chat/completions", req, false)
}

// Stream implements Provider.Stream for OpenAI by delegating to the shared
// streaming helper (mirrors the Call pattern).
func (p *OpenAIProvider) Stream(ctx context.Context, req ProviderRequest, onDelta func(ContentBlock)) (ProviderResponse, error) {
	return doOpenAICompatibleStream(ctx, p.cfg.HTTPClient, p.cfg.APIKey, p.cfg.BaseURL+"/v1/chat/completions", req, onDelta, false)
}

// ---------------------------------------------------------------------------
// Shared HTTP call helper for OpenAI-compatible endpoints
// ---------------------------------------------------------------------------

// doOpenAICompatibleCall marshals req into an OpenAI chat completions body,
// POSTs it to url with the given Bearer key, and decodes the response.
//
// cacheCompatible controls whether cache_control markers in canonical types
// are emitted on the wire (OpenRouter), or dropped (stock OpenAI).
func doOpenAICompatibleCall(
	ctx context.Context,
	httpClient *http.Client,
	apiKey string,
	url string,
	req ProviderRequest,
	cacheCompatible bool,
) (ProviderResponse, error) {
	openaiTools, err := toOpenAITools(req.Tools, req.ProviderTools, cacheCompatible)
	if err != nil {
		return ProviderResponse{}, err
	}
	chatReq := openAIChatRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Messages:  toOpenAIMessages(req.System, req.SystemCache, req.Messages, cacheCompatible),
		Tools:     openaiTools,
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
	cacheCompatible bool,
) (ProviderResponse, error) {
	openaiTools, err := toOpenAITools(req.Tools, req.ProviderTools, cacheCompatible)
	if err != nil {
		return ProviderResponse{}, err
	}
	chatReq := openAIChatRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Messages:  toOpenAIMessages(req.System, req.SystemCache, req.Messages, cacheCompatible),
		Tools:     openaiTools,
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

		// Some providers embed error objects in the stream body even on HTTP 200.
		var errCheck openAIErrorBody
		if err := json.Unmarshal([]byte(data), &errCheck); err == nil && errCheck.Error.Message != "" {
			return ProviderResponse{}, &APIError{Type: errCheck.Error.Type, Message: errCheck.Error.Message}
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
				usage.InputTokens = chunk.Usage.PromptTokens
				usage.OutputTokens = chunk.Usage.CompletionTokens
				usage.CacheReadTokens = chunk.Usage.PromptTokensDetails.CachedTokens
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
			usage.CacheReadTokens = chunk.Usage.PromptTokensDetails.CachedTokens
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
