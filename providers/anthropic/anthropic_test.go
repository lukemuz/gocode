package anthropic

import (
	"context"
	"encoding/json"
	"errors"
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

func TestProvider_Stream(t *testing.T) {
	tests := []struct {
		name           string
		streamLines    []string
		wantDeltas     []gocode.ContentBlock
		wantStopReason string
		wantUsage      gocode.Usage
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
			wantDeltas: []gocode.ContentBlock{
				{Type: gocode.TypeText, Text: "Hello"},
				{Type: gocode.TypeText, Text: " world"},
			},
			wantStopReason: "end_turn",
			wantUsage:      gocode.Usage{InputTokens: 10, OutputTokens: 5},
		},
		{
			name: "tool use with partial_json",
			streamLines: []string{
				`data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":0,"output_tokens":0}}}`,
				`data: {"type":"content_block_start","content_block":{"type":"tool_use","id":"tu_123","name":"calculator"}}`,
				`data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"op\":\"add\""}}`,
				`data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":",\"num\":42}"}}`,
				`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":8}}`,
				`data: [DONE]`,
			},
			wantDeltas: []gocode.ContentBlock{
				{Type: gocode.TypeToolUse, ID: "tu_123", Name: "calculator"},
				{Type: gocode.TypeToolUse, ID: "tu_123", Name: "calculator", Input: json.RawMessage(`{"op":"add"`)},
				{Type: gocode.TypeToolUse, ID: "tu_123", Name: "calculator", Input: json.RawMessage(`{"op":"add","num":42}`)},
			},
			wantStopReason: "tool_use",
			wantUsage:      gocode.Usage{OutputTokens: 8},
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

			p := &Provider{
				cfg: Config{
					APIKey:     "test-key",
					HTTPClient: srv.Client(),
					BaseURL:    srv.URL,
				},
			}

			var gotDeltas []gocode.ContentBlock
			onDelta := func(b gocode.ContentBlock) {
				gotDeltas = append(gotDeltas, b)
			}

			req := gocode.ProviderRequest{
				Model:    gocode.ModelSonnet,
				Messages: []gocode.Message{gocode.NewUserMessage("ping")},
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

func TestNewProvider_AuthValidation(t *testing.T) {
	if _, err := NewProvider(Config{}); err == nil {
		t.Errorf("expected error when neither APIKey nor OAuthToken is set")
	}
	if _, err := NewProvider(Config{APIKey: "k", OAuthToken: "t"}); err == nil {
		t.Errorf("expected error when both APIKey and OAuthToken are set")
	}
	if _, err := NewProvider(Config{APIKey: "k"}); err != nil {
		t.Errorf("APIKey-only config should be valid: %v", err)
	}
	if _, err := NewProvider(Config{OAuthToken: "t"}); err != nil {
		t.Errorf("OAuthToken-only config should be valid: %v", err)
	}
}

func TestProvider_OAuthHeadersAndSystemPrompt(t *testing.T) {
	type captured struct {
		auth        string
		apiKey      string
		anthBeta    string
		body        []byte
	}
	var got captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.auth = r.Header.Get("Authorization")
		got.apiKey = r.Header.Get("X-Api-Key")
		got.anthBeta = r.Header.Get("Anthropic-Beta")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		got.body = buf
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := &Provider{cfg: Config{
		OAuthToken: "sk-ant-oat-test",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}}

	_, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    gocode.ModelSonnet,
		System:   "You are gocode.",
		Messages: []gocode.Message{gocode.NewUserMessage("ping")},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if got.auth != "Bearer sk-ant-oat-test" {
		t.Errorf("Authorization header = %q, want Bearer sk-ant-oat-test", got.auth)
	}
	if got.apiKey != "" {
		t.Errorf("X-Api-Key header should be empty when using OAuth, got %q", got.apiKey)
	}
	if got.anthBeta != oauthBetaHeader {
		t.Errorf("Anthropic-Beta header = %q, want %q", got.anthBeta, oauthBetaHeader)
	}

	// System must serialize as an array whose first block carries the
	// Claude Code identity, with the caller's prompt as a second block.
	var body struct {
		System []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"system"`
	}
	if err := json.Unmarshal(got.body, &body); err != nil {
		t.Fatalf("unmarshal request body: %v\n%s", err, got.body)
	}
	if len(body.System) != 2 {
		t.Fatalf("expected 2 system blocks, got %d: %+v", len(body.System), body.System)
	}
	if body.System[0].Text != claudeCodeIdentityPrompt {
		t.Errorf("system[0] = %q, want %q", body.System[0].Text, claudeCodeIdentityPrompt)
	}
	if body.System[1].Text != "You are gocode." {
		t.Errorf("system[1] = %q, want %q", body.System[1].Text, "You are gocode.")
	}
}

func TestProvider_APIKeyHeaders(t *testing.T) {
	var auth, apiKey, anthBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		apiKey = r.Header.Get("X-Api-Key")
		anthBeta = r.Header.Get("Anthropic-Beta")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := &Provider{cfg: Config{APIKey: "test-key", BaseURL: srv.URL, HTTPClient: srv.Client()}}
	if _, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    gocode.ModelSonnet,
		System:   "hi",
		Messages: []gocode.Message{gocode.NewUserMessage("ping")},
	}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if apiKey != "test-key" {
		t.Errorf("X-Api-Key header = %q, want test-key", apiKey)
	}
	if auth != "" {
		t.Errorf("Authorization header should be empty when using API key, got %q", auth)
	}
	if anthBeta != "" {
		t.Errorf("Anthropic-Beta header should be empty when using API key, got %q", anthBeta)
	}
}

func TestAnthropicSystem_OAuthShape(t *testing.T) {
	// OAuth + non-empty caller prompt: identity block + caller block,
	// cache breakpoint attached to the caller block.
	cache := &gocode.CacheControl{Type: "ephemeral"}
	out := anthropicSystem("You are gocode.", cache, true)
	blocks, ok := out.([]anthropicSystemBlock)
	if !ok {
		t.Fatalf("expected []anthropicSystemBlock, got %T", out)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Text != claudeCodeIdentityPrompt || blocks[0].CacheControl != nil {
		t.Errorf("identity block = %+v", blocks[0])
	}
	if blocks[1].Text != "You are gocode." || blocks[1].CacheControl != cache {
		t.Errorf("caller block = %+v", blocks[1])
	}

	// OAuth + empty caller prompt: only the identity block; cache
	// breakpoint attaches to it so an empty-system OAuth request is still
	// a valid cache anchor.
	out = anthropicSystem("", cache, true)
	blocks, _ = out.([]anthropicSystemBlock)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Text != claudeCodeIdentityPrompt || blocks[0].CacheControl != cache {
		t.Errorf("oauth-empty block = %+v", blocks[0])
	}

	// Non-OAuth + empty caller: nil (omitted on the wire).
	if got := anthropicSystem("", nil, false); got != nil {
		t.Errorf("expected nil, got %v", got)
	}

	// Non-OAuth + cache: single block (existing behaviour).
	out = anthropicSystem("hi", cache, false)
	blocks, ok = out.([]anthropicSystemBlock)
	if !ok || len(blocks) != 1 || blocks[0].Text != "hi" {
		t.Errorf("non-oauth+cache shape = %+v", out)
	}

	// Non-OAuth + no cache: plain string (existing behaviour).
	if got := anthropicSystem("hi", nil, false); got != "hi" {
		t.Errorf("non-oauth+no-cache = %v, want \"hi\"", got)
	}
}

func TestProvider_ErrorHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))
	}))
	defer srv.Close()

	p := &Provider{
		cfg: Config{
			APIKey:     "test",
			HTTPClient: srv.Client(),
			BaseURL:    srv.URL,
		},
	}

	_, err := p.Call(context.Background(), gocode.ProviderRequest{Model: "test"})
	if err == nil {
		t.Fatal("expected APIError")
	}
	var apiErr *gocode.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 429 {
		t.Errorf("expected *APIError(429), got %T: %v", err, err)
	}
}
