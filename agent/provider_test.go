package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testServerForStream returns a test server that streams the given lines as
// text/event-stream (for both Anthropic SSE and OpenAI NDJSON-style chunks).
func testServerForStream(t *testing.T, lines []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		for _, line := range lines {
			if _, err := w.Write([]byte(line + "\n")); err != nil {
				t.Logf("write error: %v", err)
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
}

func TestAnthropicProvider_Stream(t *testing.T) {
	tests := []struct {
		name           string
		streamLines    []string
		wantDeltas     []ContentBlock
		wantStopReason string
		wantUsage      Usage
		wantErr        string
	}{
		{
			name: "text deltas only",
			streamLines: []string{
				`data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":10,"output_tokens":0}}}`,
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`,
				`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":" world"}}`,
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
				`data: [DONE]`,
			},
			wantDeltas: []ContentBlock{
				{Type: TypeText, Text: "Hello"},
				{Type: TypeText, Text: " world"},
			},
			wantStopReason: "end_turn",
			wantUsage:      Usage{InputTokens: 10, OutputTokens: 5},
		},
		{
			name: "tool use with partial_json",
			streamLines: []string{
				`data: {"type":"content_block_start","content_block":{"type":"tool_use","id":"tu_123","name":"calculator"}}`,
				`data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"op\":\"add\"}"}}`,
				`data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":",\"num\":42}"}}`,
				`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":8}}`,
				`data: [DONE]`,
			},
			wantDeltas: []ContentBlock{
				{Type: TypeToolUse, ID: "tu_123", Name: "calculator"},
				{Type: TypeToolUse, ID: "tu_123", Name: "calculator", Input: json.RawMessage(`{"op":"add"`)},
				{Type: TypeToolUse, ID: "tu_123", Name: "calculator", Input: json.RawMessage(`{"op":"add","num":42}`)},
			},
			wantStopReason: "tool_use",
			wantUsage:      Usage{OutputTokens: 8},
		},
		{
			name: "error status",
			streamLines: []string{
				`data: {"type":"error","error":{"type":"overloaded_error","message":"too busy"}}`,
			},
			wantErr: "overloaded_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := testServerForStream(t, tt.streamLines)
			defer srv.Close()

			p := &AnthropicProvider{
				APIKey:     "test-key",
				HTTPClient: srv.Client(),
				BaseURL:    srv.URL,
			}

			var gotDeltas []ContentBlock
			onDelta := func(b ContentBlock) {
				gotDeltas = append(gotDeltas, b)
			}

			req := ProviderRequest{
				Model:    ModelSonnet,
				Messages: []Message{NewUserMessage("ping")},
			}
			resp, err := p.Stream(context.Background(), req, onDelta)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.StopReason != tt.wantStopReason {
				t.Errorf("stop reason = %q, want %q", resp.StopReason, tt.wantStopReason)
			}
			if resp.Usage != tt.wantUsage {
				t.Errorf("usage = %+v, want %+v", resp.Usage, tt.wantUsage)
			}

			if len(gotDeltas) != len(tt.wantDeltas) {
				t.Errorf("got %d deltas, want %d: %+v", len(gotDeltas), len(tt.wantDeltas), gotDeltas)
			}
			for i, want := range tt.wantDeltas {
				if i >= len(gotDeltas) || gotDeltas[i].Type != want.Type || gotDeltas[i].Text != want.Text ||
					gotDeltas[i].ID != want.ID || gotDeltas[i].Name != want.Name ||
					string(gotDeltas[i].Input) != string(want.Input) {
					t.Errorf("delta[%d] = %+v, want %+v", i, gotDeltas[i], want)
				}
			}
		})
	}
}

func TestOpenAICompatibleStream(t *testing.T) {
	// Tests the shared NDJSON streaming logic used by OpenAI and OpenRouter providers.
	tests := []struct {
		name           string
		streamLines    []string
		wantDeltas     []ContentBlock
		wantStopReason string
		wantUsage      Usage
		wantErr        string
	}{
		{
			name: "text deltas and finish",
			streamLines: []string{
				`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello"},"index":0,"finish_reason":null}]}`,
				`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":" world"},"index":0,"finish_reason":null}]}`,
				`data: {"id":"chatcmpl-1","choices":[{"delta":{},"index":0,"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
				`data: [DONE]`,
			},
			wantDeltas: []ContentBlock{
				{Type: TypeText, Text: "Hello"},
				{Type: TypeText, Text: " world"},
			},
			wantStopReason: "end_turn",
			wantUsage:      Usage{InputTokens: 10, OutputTokens: 5},
		},
		{
			name: "tool calls",
			streamLines: []string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\""}}]},"index":0}]}`,
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"hello\"}"}}]},"index":0}]}`,
				`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":8,"completion_tokens":12}}`,
				`data: [DONE]`,
			},
			wantDeltas: []ContentBlock{
				{Type: TypeToolUse, ID: "call_1", Name: "echo", Input: json.RawMessage(`{"`)},
				{Type: TypeToolUse, ID: "call_1", Name: "echo", Input: json.RawMessage(`{"hello"}`)},
			},
			wantStopReason: "tool_use",
			wantUsage:      Usage{InputTokens: 8, OutputTokens: 12},
		},
		{
			name: "error response",
			streamLines: []string{
				`data: {"error":{"message":"invalid api key","type":"invalid_request_error"}}`,
			},
			wantErr: "invalid api key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := testServerForStream(t, tt.streamLines)
			defer srv.Close()

			// Use OpenAI provider as it exercises doOpenAICompatibleStream
			p := &OpenAIProvider{
				APIKey:     "test-key",
				HTTPClient: srv.Client(),
				BaseURL:    srv.URL,
				IsOpenRouter: false,
			}

			var gotDeltas []ContentBlock
			onDelta := func(b ContentBlock) { gotDeltas = append(gotDeltas, b) }

			req := ProviderRequest{
				Model:    "gpt-4o-mini",
				Messages: []Message{NewUserMessage("ping")},
			}
			resp, err := p.Stream(context.Background(), req, onDelta)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error with %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.StopReason != tt.wantStopReason {
				t.Errorf("stop reason = %q, want %q", resp.StopReason, tt.wantStopReason)
			}
			if resp.Usage != tt.wantUsage {
				t.Errorf("usage = %+v, want %+v", resp.Usage, tt.wantUsage)
			}

			if len(gotDeltas) != len(tt.wantDeltas) {
				t.Fatalf("got %d deltas, want %d", len(gotDeltas), len(tt.wantDeltas))
			}
			for i := range tt.wantDeltas {
				g, w := gotDeltas[i], tt.wantDeltas[i]
				if g.Type != w.Type || g.Text != w.Text || g.ID != w.ID || g.Name != w.Name ||
					string(g.Input) != string(w.Input) {
					t.Errorf("delta[%d] got %+v, want %+v", i, g, w)
				}
			}
		})
	}
}

// TestProviderErrorHandling ensures API errors are normalized across providers.
func TestProviderErrorHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))
	}))
	defer srv.Close()

	p := &AnthropicProvider{
		APIKey:     "test",
		HTTPClient: srv.Client(),
		BaseURL:    srv.URL,
	}

	_, err := p.Call(context.Background(), ProviderRequest{Model: "test"})
	if err == nil {
		t.Fatal("expected APIError")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 429 {
		t.Errorf("expected *APIError(429), got %T: %v", err, err)
	}
}
