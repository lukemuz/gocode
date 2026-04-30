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

	"github.com/lukemuz/gocode"
)

// ResponsesProvider targets POST /v1/responses, the successor to the Chat
// Completions endpoint. Use it when you need OpenAI's hosted tools
// (web_search, file_search, code_interpreter, image_generation) — they are
// not available on Chat Completions.
//
// Wire shape differs from Chat Completions in three significant ways:
//
//   1. Conversation lives in `input` items, not `messages`. Each turn (text,
//      function call, function call output) is a top-level item.
//   2. Tools are flat: `{"type":"function","name":...,"parameters":...}`
//      rather than `{"type":"function","function":{...}}`.
//   3. Hosted tools are first-class items typed by `web_search`,
//      `file_search`, etc. — advertised via gocode.ProviderTool tagged
//      ResponsesProviderTag.

// ResponsesProviderTag is the value gocode.ProviderTool.Provider must carry
// for a hosted-tool entry to be accepted by ResponsesProvider.
const ResponsesProviderTag = "openai-responses"

// ResponsesConfig holds configuration for the Responses provider.
type ResponsesConfig struct {
	APIKey     string       // required
	BaseURL    string       // defaults to https://api.openai.com
	HTTPClient *http.Client // defaults to a 60-second timeout client
}

// ResponsesProvider implements gocode.Provider for OpenAI's Responses API.
type ResponsesProvider struct {
	cfg ResponsesConfig
}

// NewResponsesProvider creates a ResponsesProvider, filling in defaults.
func NewResponsesProvider(cfg ResponsesConfig) (*ResponsesProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("gocode: ResponsesConfig.APIKey is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = openAIDefaultBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &ResponsesProvider{cfg: cfg}, nil
}

// NewResponsesProviderFromEnv reads OPENAI_API_KEY.
func NewResponsesProviderFromEnv() (*ResponsesProvider, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("gocode: OPENAI_API_KEY environment variable is not set")
	}
	return NewResponsesProvider(ResponsesConfig{APIKey: key})
}

// NewResponsesClientFromEnv builds a Client backed by Responses.
func NewResponsesClientFromEnv(model string) (*gocode.Client, error) {
	provider, err := NewResponsesProviderFromEnv()
	if err != nil {
		return nil, err
	}
	return gocode.New(gocode.Config{Provider: provider, Model: model})
}

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

type openAIResponsesRequest struct {
	Model           string            `json:"model"`
	Input           []json.RawMessage `json:"input"`
	Instructions    string            `json:"instructions,omitempty"`
	Tools           []json.RawMessage `json:"tools,omitempty"`
	MaxOutputTokens int               `json:"max_output_tokens,omitempty"`
	Stream          bool              `json:"stream,omitempty"`
}

type openAIResponsesResponse struct {
	ID                string                       `json:"id"`
	Status            string                       `json:"status"`
	Output            []json.RawMessage            `json:"output"`
	Usage             *openAIResponsesUsage        `json:"usage"`
	IncompleteDetails *openAIResponsesIncomplete   `json:"incomplete_details"`
	Error             *openAIResponsesAPIErrorBody `json:"error"`
}

type openAIResponsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type openAIResponsesIncomplete struct {
	Reason string `json:"reason"`
}

type openAIResponsesAPIErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

// ---------------------------------------------------------------------------
// Translation: canonical Messages → Responses input items
// ---------------------------------------------------------------------------

// toResponsesInput flattens canonical Messages into the input-item array.
// Each ContentBlock becomes one item: text turns into a {role, content}
// message item; tool_use turns into a function_call item; tool_result turns
// into a function_call_output item; opaque blocks (round-tripped from a
// previous response) are spliced verbatim from ContentBlock.Raw.
func toResponsesInput(messages []gocode.Message) ([]json.RawMessage, error) {
	var items []json.RawMessage
	for _, m := range messages {
		for _, b := range m.Content {
			if len(b.Raw) > 0 {
				items = append(items, b.Raw)
				continue
			}
			switch b.Type {
			case gocode.TypeText:
				// Use the structured content-part form so both user and
				// assistant text are encoded consistently. The Responses
				// API accepts {role, content: [{type, text}]} on input for
				// either role; the part type differs by role.
				partType := "input_text"
				if m.Role == gocode.RoleAssistant {
					partType = "output_text"
				}
				item := map[string]any{
					"type": "message",
					"role": m.Role,
					"content": []map[string]any{
						{"type": partType, "text": b.Text},
					},
				}
				raw, err := json.Marshal(item)
				if err != nil {
					return nil, fmt.Errorf("gocode: openai-responses: marshal text item: %w", err)
				}
				items = append(items, raw)
			case gocode.TypeToolUse:
				args := string(b.Input)
				if args == "" {
					args = "{}"
				}
				item := map[string]any{
					"type":      "function_call",
					"call_id":   b.ID,
					"name":      b.Name,
					"arguments": args,
				}
				raw, err := json.Marshal(item)
				if err != nil {
					return nil, fmt.Errorf("gocode: openai-responses: marshal function_call: %w", err)
				}
				items = append(items, raw)
			case gocode.TypeToolResult:
				item := map[string]any{
					"type":    "function_call_output",
					"call_id": b.ToolUseID,
					"output":  b.Content,
				}
				raw, err := json.Marshal(item)
				if err != nil {
					return nil, fmt.Errorf("gocode: openai-responses: marshal function_call_output: %w", err)
				}
				items = append(items, raw)
			}
		}
	}
	return items, nil
}

// buildResponsesTools serializes Tools and ProviderTools into the wire array.
// Function tools use the flat Responses-API shape (no nested "function" key).
// Provider-tagged tools and ProviderTools are validated against the
// ResponsesProviderTag tag so misuse fails loudly.
func buildResponsesTools(tools []gocode.Tool, providerTools []gocode.ProviderTool) ([]json.RawMessage, error) {
	if len(tools) == 0 && len(providerTools) == 0 {
		return nil, nil
	}
	out := make([]json.RawMessage, 0, len(tools)+len(providerTools))
	for _, t := range tools {
		if t.Provider != "" && t.Provider != ResponsesProviderTag {
			return nil, fmt.Errorf("gocode: openai-responses: tool %q is tagged for provider %q", t.Name, t.Provider)
		}
		// Category-2 tools (Tool.Raw set) ship verbatim.
		if len(t.Raw) > 0 {
			out = append(out, t.Raw)
			continue
		}
		// Standard function tool — flat shape.
		def := map[string]any{
			"type": "function",
			"name": t.Name,
		}
		if t.Description != "" {
			def["description"] = t.Description
		}
		if len(t.InputSchema) > 0 {
			def["parameters"] = json.RawMessage(t.InputSchema)
		}
		raw, err := json.Marshal(def)
		if err != nil {
			return nil, fmt.Errorf("gocode: openai-responses: marshal tool %q: %w", t.Name, err)
		}
		out = append(out, raw)
	}
	for i, pt := range providerTools {
		if pt.Provider != ResponsesProviderTag {
			return nil, fmt.Errorf("gocode: openai-responses: provider tool [%d] is tagged for provider %q", i, pt.Provider)
		}
		if len(pt.Raw) == 0 {
			return nil, fmt.Errorf("gocode: openai-responses: provider tool [%d] has empty Raw", i)
		}
		out = append(out, pt.Raw)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Translation: Responses output items → canonical ContentBlocks
// ---------------------------------------------------------------------------

// parseResponsesOutput walks each output item and produces a flat slice of
// gocode.ContentBlocks. message items expand into one TypeText block per
// output_text content part; function_call items become TypeToolUse;
// everything else (web_search_call, code_interpreter_call, reasoning,
// refusals, image_generation_call, ...) is captured opaquely so multi-turn
// history round-trips faithfully.
func parseResponsesOutput(items []json.RawMessage) ([]gocode.ContentBlock, error) {
	var out []gocode.ContentBlock
	for _, raw := range items {
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			return nil, fmt.Errorf("gocode: openai-responses: decode output item type: %w", err)
		}
		switch head.Type {
		case "message":
			var msg struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.Unmarshal(raw, &msg); err != nil {
				return nil, fmt.Errorf("gocode: openai-responses: decode message item: %w", err)
			}
			for _, part := range msg.Content {
				if part.Type == "output_text" {
					out = append(out, gocode.ContentBlock{Type: gocode.TypeText, Text: part.Text})
				}
			}
			// If the message had non-output_text parts, preserve it opaquely.
			textOnly := true
			for _, part := range msg.Content {
				if part.Type != "output_text" {
					textOnly = false
					break
				}
			}
			if !textOnly {
				out = append(out, gocode.ContentBlock{Type: head.Type, Raw: append(json.RawMessage(nil), raw...)})
			}
		case "function_call":
			var fc struct {
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}
			if err := json.Unmarshal(raw, &fc); err != nil {
				return nil, fmt.Errorf("gocode: openai-responses: decode function_call: %w", err)
			}
			args := json.RawMessage(fc.Arguments)
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			out = append(out, gocode.ContentBlock{
				Type:  gocode.TypeToolUse,
				ID:    fc.CallID,
				Name:  fc.Name,
				Input: args,
			})
		default:
			out = append(out, gocode.ContentBlock{Type: head.Type, Raw: append(json.RawMessage(nil), raw...)})
		}
	}
	return out, nil
}

// responsesStopReason maps Responses status + output to the canonical loop
// stop reason. A function_call in output dominates: the loop must run tools
// even on a "completed" response.
func responsesStopReason(status string, blocks []gocode.ContentBlock, incomplete *openAIResponsesIncomplete) string {
	for _, b := range blocks {
		if b.Type == gocode.TypeToolUse {
			return "tool_use"
		}
	}
	if status == "incomplete" && incomplete != nil && incomplete.Reason == "max_output_tokens" {
		return "max_tokens"
	}
	return "end_turn"
}

// ---------------------------------------------------------------------------
// Provider methods
// ---------------------------------------------------------------------------

// Call implements gocode.Provider.
func (p *ResponsesProvider) Call(ctx context.Context, req gocode.ProviderRequest) (gocode.ProviderResponse, error) {
	body, err := buildResponsesRequest(req, false)
	if err != nil {
		return gocode.ProviderResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.BaseURL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: openai-responses: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)

	resp, err := p.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: openai-responses: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return gocode.ProviderResponse{}, decodeResponsesError(resp)
	}

	var wire openAIResponsesResponse
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: openai-responses: decode response: %w", err)
	}
	if wire.Error != nil && wire.Error.Message != "" {
		return gocode.ProviderResponse{}, &gocode.APIError{Type: wire.Error.Type, Message: wire.Error.Message}
	}

	blocks, err := parseResponsesOutput(wire.Output)
	if err != nil {
		return gocode.ProviderResponse{}, err
	}

	pr := gocode.ProviderResponse{
		Content:    blocks,
		StopReason: responsesStopReason(wire.Status, blocks, wire.IncompleteDetails),
	}
	if wire.Usage != nil {
		pr.Usage = gocode.Usage{InputTokens: wire.Usage.InputTokens, OutputTokens: wire.Usage.OutputTokens}
	}
	return pr, nil
}

func buildResponsesRequest(req gocode.ProviderRequest, stream bool) ([]byte, error) {
	input, err := toResponsesInput(req.Messages)
	if err != nil {
		return nil, err
	}
	tools, err := buildResponsesTools(req.Tools, req.ProviderTools)
	if err != nil {
		return nil, err
	}
	wire := openAIResponsesRequest{
		Model:           req.Model,
		Input:           input,
		Instructions:    req.System,
		Tools:           tools,
		MaxOutputTokens: req.MaxTokens,
		Stream:          stream,
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("gocode: openai-responses: marshal request: %w", err)
	}
	return body, nil
}

func decodeResponsesError(resp *http.Response) error {
	var body struct {
		Error openAIResponsesAPIErrorBody `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return &gocode.APIError{
		StatusCode: resp.StatusCode,
		Type:       body.Error.Type,
		Message:    body.Error.Message,
	}
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

// Stream implements gocode.Provider.Stream against the Responses SSE event
// format. Function-call output items are tracked by output_index so argument
// deltas can be paired with the right name/call_id when emitted to onDelta.
// The aggregate gocode.ProviderResponse comes from the terminal
// `response.completed` event when available; otherwise we synthesize one.
func (p *ResponsesProvider) Stream(ctx context.Context, req gocode.ProviderRequest, onDelta func(gocode.ContentBlock)) (gocode.ProviderResponse, error) {
	body, err := buildResponsesRequest(req, true)
	if err != nil {
		return gocode.ProviderResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.BaseURL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: openai-responses: build stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: openai-responses: stream http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return gocode.ProviderResponse{}, decodeResponsesError(resp)
	}

	type fcState struct {
		callID string
		name   string
		args   strings.Builder
	}
	calls := map[int]*fcState{}
	var fullText strings.Builder
	var final openAIResponsesResponse
	var sawCompleted bool

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if strings.TrimSpace(data) == "[DONE]" {
			break
		}

		var head struct {
			Type        string          `json:"type"`
			OutputIndex int             `json:"output_index"`
			ItemID      string          `json:"item_id"`
			Item        json.RawMessage `json:"item"`
			Delta       string          `json:"delta"`
			Response    json.RawMessage `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &head); err != nil {
			continue
		}

		switch head.Type {
		case "response.output_item.added":
			var item struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
				Name   string `json:"name"`
			}
			if err := json.Unmarshal(head.Item, &item); err == nil && item.Type == "function_call" {
				calls[head.OutputIndex] = &fcState{callID: item.CallID, name: item.Name}
				onDelta(gocode.ContentBlock{Type: gocode.TypeToolUse, ID: item.CallID, Name: item.Name})
			}
		case "response.output_text.delta":
			if head.Delta != "" {
				fullText.WriteString(head.Delta)
				onDelta(gocode.ContentBlock{Type: gocode.TypeText, Text: head.Delta})
			}
		case "response.function_call_arguments.delta":
			fc := calls[head.OutputIndex]
			if fc == nil {
				continue
			}
			fc.args.WriteString(head.Delta)
			onDelta(gocode.ContentBlock{
				Type:  gocode.TypeToolUse,
				ID:    fc.callID,
				Name:  fc.name,
				Input: json.RawMessage(fc.args.String()),
			})
		case "response.completed":
			var env struct {
				Response openAIResponsesResponse `json:"response"`
			}
			if err := json.Unmarshal([]byte(data), &env); err == nil {
				final = env.Response
				sawCompleted = true
			}
		case "response.failed", "response.error":
			var env struct {
				Response openAIResponsesResponse     `json:"response"`
				Error    openAIResponsesAPIErrorBody `json:"error"`
			}
			_ = json.Unmarshal([]byte(data), &env)
			if env.Error.Message != "" {
				return gocode.ProviderResponse{}, &gocode.APIError{Type: env.Error.Type, Message: env.Error.Message}
			}
			if env.Response.Error != nil && env.Response.Error.Message != "" {
				return gocode.ProviderResponse{}, &gocode.APIError{Type: env.Response.Error.Type, Message: env.Response.Error.Message}
			}
			return gocode.ProviderResponse{}, &gocode.APIError{Message: "openai-responses: stream failed"}
		}
	}
	if err := scanner.Err(); err != nil {
		return gocode.ProviderResponse{}, fmt.Errorf("gocode: openai-responses: stream read: %w", err)
	}

	// Prefer the terminal `response.completed` envelope for aggregation: it
	// carries the canonical Output, Usage, and IncompleteDetails.
	if sawCompleted {
		blocks, err := parseResponsesOutput(final.Output)
		if err != nil {
			return gocode.ProviderResponse{}, err
		}
		pr := gocode.ProviderResponse{
			Content:    blocks,
			StopReason: responsesStopReason(final.Status, blocks, final.IncompleteDetails),
		}
		if final.Usage != nil {
			pr.Usage = gocode.Usage{InputTokens: final.Usage.InputTokens, OutputTokens: final.Usage.OutputTokens}
		}
		return pr, nil
	}

	// Fallback: synthesize from accumulated streamed state.
	var content []gocode.ContentBlock
	if fullText.Len() > 0 {
		content = append(content, gocode.ContentBlock{Type: gocode.TypeText, Text: fullText.String()})
	}
	for _, fc := range calls {
		input := json.RawMessage(fc.args.String())
		if len(input) == 0 {
			input = json.RawMessage(`{}`)
		}
		content = append(content, gocode.ContentBlock{
			Type:  gocode.TypeToolUse,
			ID:    fc.callID,
			Name:  fc.name,
			Input: input,
		})
	}
	stop := "end_turn"
	if len(calls) > 0 {
		stop = "tool_use"
	}
	return gocode.ProviderResponse{Content: content, StopReason: stop}, nil
}
