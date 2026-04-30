package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lukemuz/gocode"
)

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

func TestCompatibleStream(t *testing.T) {
	tests := []struct {
		name           string
		streamLines    []string
		wantDeltas     []gocode.ContentBlock
		wantStopReason string
		wantUsage      gocode.Usage
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
			wantDeltas: []gocode.ContentBlock{
				{Type: gocode.TypeText, Text: "Hello"},
				{Type: gocode.TypeText, Text: " world"},
			},
			wantStopReason: "end_turn",
			wantUsage:      gocode.Usage{InputTokens: 10, OutputTokens: 5},
		},
		{
			name: "tool calls",
			streamLines: []string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\""}}]},"index":0}]}`,
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"hello\"}"}}]},"index":0}]}`,
				`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":8,"completion_tokens":12}}`,
				`data: [DONE]`,
			},
			wantDeltas: []gocode.ContentBlock{
				{Type: gocode.TypeToolUse, ID: "call_1", Name: "echo", Input: json.RawMessage(`{"`)},
				{Type: gocode.TypeToolUse, ID: "call_1", Name: "echo", Input: json.RawMessage(`{"hello"}`)},
			},
			wantStopReason: "tool_use",
			wantUsage:      gocode.Usage{InputTokens: 8, OutputTokens: 12},
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

			p := &Provider{
				cfg: Config{
					APIKey:     "test-key",
					HTTPClient: srv.Client(),
					BaseURL:    srv.URL,
				},
			}

			var gotDeltas []gocode.ContentBlock
			onDelta := func(b gocode.ContentBlock) { gotDeltas = append(gotDeltas, b) }

			req := gocode.ProviderRequest{
				Model:    "gpt-4o-mini",
				Messages: []gocode.Message{gocode.NewUserMessage("ping")},
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
