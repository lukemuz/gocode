package openai

import (
	"context"
	"encoding/json"
	"io"
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

func TestToOpenAIToolsRejectsProviderToolsByDefault(t *testing.T) {
	pt := gocode.ProviderTool{Provider: "openrouter", Raw: json.RawMessage(`{"type":"openrouter:web_search"}`)}
	_, err := toOpenAITools(nil, []gocode.ProviderTool{pt}, false, false)
	if err == nil {
		t.Fatal("expected error when allowProviderTools=false, got nil")
	}
	if !strings.Contains(err.Error(), "use a Responses-API provider") {
		t.Errorf("error message changed; expected the existing 'use a Responses-API provider' guidance, got %q", err.Error())
	}
}

func TestToOpenAIToolsSplicesProviderToolsWhenAllowed(t *testing.T) {
	fn := gocode.NewTool("calc", "do math",
		gocode.Object(gocode.Number("a", "v", gocode.Required())))
	pt := gocode.ProviderTool{
		Provider: "openrouter",
		Raw:      json.RawMessage(`{"type":"openrouter:web_search","parameters":{"max_results":3}}`),
	}

	out, err := toOpenAITools([]gocode.Tool{fn}, []gocode.ProviderTool{pt}, false, true)
	if err != nil {
		t.Fatalf("toOpenAITools: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}

	var first map[string]any
	if err := json.Unmarshal(out[0], &first); err != nil {
		t.Fatalf("decode first tool: %v", err)
	}
	if first["type"] != "function" {
		t.Errorf("first tool type = %v, want function", first["type"])
	}

	var second map[string]any
	if err := json.Unmarshal(out[1], &second); err != nil {
		t.Fatalf("decode second tool: %v", err)
	}
	if second["type"] != "openrouter:web_search" {
		t.Errorf("second tool type = %v, want openrouter:web_search", second["type"])
	}
	params, ok := second["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters not preserved: %v", second)
	}
	if params["max_results"].(float64) != 3 {
		t.Errorf("max_results = %v, want 3", params["max_results"])
	}
}

func TestProviderRejectsProviderToolsAtChatCompletions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be reached when ProviderTools are rejected")
	}))
	defer srv.Close()

	p := &Provider{cfg: Config{APIKey: "test", BaseURL: srv.URL, HTTPClient: srv.Client()}}
	_, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    "gpt-4o-mini",
		Messages: []gocode.Message{gocode.NewUserMessage("hi")},
		ProviderTools: []gocode.ProviderTool{{
			Provider: "openrouter",
			Raw:      json.RawMessage(`{"type":"openrouter:web_search"}`),
		}},
	})
	if err == nil {
		t.Fatal("expected error from Provider.Call with ProviderTools, got nil")
	}
	if !strings.Contains(err.Error(), "Responses-API") {
		t.Errorf("error message changed: %q", err.Error())
	}
}

func TestFromOpenAIResponseSurfacesAnnotations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Discard the request body — we're testing response decoding only.
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{
				"message":{
					"role":"assistant",
					"content":"Go was released in 2009.",
					"annotations":[
						{"type":"url_citation","url_citation":{"url":"https://go.dev","title":"Go","start_index":0,"end_index":2}}
					]
				},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":1,"completion_tokens":1}
		}`))
	}))
	defer srv.Close()

	p := &Provider{cfg: Config{APIKey: "test", BaseURL: srv.URL, HTTPClient: srv.Client()}}
	resp, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    "openai/anything",
		Messages: []gocode.Message{gocode.NewUserMessage("when was go released?")},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("got %d content blocks, want 2 (text + url_citation): %+v", len(resp.Content), resp.Content)
	}
	if resp.Content[0].Type != gocode.TypeText {
		t.Errorf("first block type = %q, want text", resp.Content[0].Type)
	}
	if resp.Content[1].Type != "url_citation" {
		t.Errorf("second block type = %q, want url_citation", resp.Content[1].Type)
	}
	if len(resp.Content[1].Raw) == 0 {
		t.Errorf("url_citation block missing Raw payload")
	}
}
