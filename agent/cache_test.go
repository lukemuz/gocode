package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Anthropic cache emission
// ---------------------------------------------------------------------------

func TestAnthropic_SystemCacheEmitsArrayForm(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newAnthropicProviderForTest(t, srv.URL)

	_, err := p.Call(context.Background(), ProviderRequest{
		Model:       "claude-test",
		System:      "stable instructions",
		Messages:    []Message{NewUserMessage("hi")},
		SystemCache: Ephemeral(),
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var sent map[string]any
	if err := json.Unmarshal(cap.body, &sent); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sys, ok := sent["system"].([]any)
	if !ok {
		t.Fatalf("system should be array form, got %T: %v", sent["system"], sent["system"])
	}
	first := sys[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "stable instructions" {
		t.Errorf("system block fields wrong: %v", first)
	}
	cc, ok := first["cache_control"].(map[string]any)
	if !ok || cc["type"] != "ephemeral" {
		t.Errorf("cache_control missing/wrong: %v", first["cache_control"])
	}
}

func TestAnthropic_NoSystemCacheKeepsString(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newAnthropicProviderForTest(t, srv.URL)

	_, err := p.Call(context.Background(), ProviderRequest{
		Model:    "claude-test",
		System:   "stable instructions",
		Messages: []Message{NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var sent map[string]any
	json.Unmarshal(cap.body, &sent)
	if s, ok := sent["system"].(string); !ok || s != "stable instructions" {
		t.Errorf("system = %v, want plain string", sent["system"])
	}
}

func TestAnthropic_ToolCacheControlEmitted(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newAnthropicProviderForTest(t, srv.URL)

	tool := NewTool("calc", "do math", Object(Number("a", "v", Required())))
	tool.CacheControl = EphemeralExtended()

	_, err := p.Call(context.Background(), ProviderRequest{
		Model:    "claude-test",
		Messages: []Message{NewUserMessage("hi")},
		Tools:    []Tool{tool},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var sent map[string]any
	json.Unmarshal(cap.body, &sent)
	tools := sent["tools"].([]any)
	got := tools[0].(map[string]any)
	cc, ok := got["cache_control"].(map[string]any)
	if !ok {
		t.Fatalf("tool cache_control missing: %v", got)
	}
	if cc["type"] != "ephemeral" || cc["ttl"] != "1h" {
		t.Errorf("cache_control = %v, want type=ephemeral ttl=1h", cc)
	}
}

func TestAnthropic_MessageCacheControlEmitted(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newAnthropicProviderForTest(t, srv.URL)

	msg := Message{Role: RoleUser, Content: []ContentBlock{
		{Type: TypeText, Text: "long context", CacheControl: Ephemeral()},
	}}
	_, err := p.Call(context.Background(), ProviderRequest{
		Model:    "claude-test",
		Messages: []Message{msg},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var sent map[string]any
	json.Unmarshal(cap.body, &sent)
	msgs := sent["messages"].([]any)
	first := msgs[0].(map[string]any)
	parts := first["content"].([]any)
	part := parts[0].(map[string]any)
	if part["cache_control"] == nil {
		t.Errorf("expected cache_control on content block, got %v", part)
	}
}

func TestAnthropic_DecodesCacheUsage(t *testing.T) {
	reply := `{
        "id":"m",
        "content":[{"type":"text","text":"ok"}],
        "stop_reason":"end_turn",
        "usage":{
            "input_tokens":50,
            "output_tokens":10,
            "cache_creation_input_tokens":1000,
            "cache_read_input_tokens":500
        }
    }`
	srv, _ := newCaptureServer(t, reply)
	p := newAnthropicProviderForTest(t, srv.URL)

	out, err := p.Call(context.Background(), ProviderRequest{
		Model:    "claude-test",
		Messages: []Message{NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.Usage.CacheCreationTokens != 1000 {
		t.Errorf("CacheCreationTokens = %d, want 1000", out.Usage.CacheCreationTokens)
	}
	if out.Usage.CacheReadTokens != 500 {
		t.Errorf("CacheReadTokens = %d, want 500", out.Usage.CacheReadTokens)
	}
}

// ---------------------------------------------------------------------------
// OpenRouter cache emission (array-of-parts content shape)
// ---------------------------------------------------------------------------

func newOpenRouterCaptureServer(t *testing.T) (*httptest.Server, *captureRequest) {
	t.Helper()
	cap := &captureRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.body = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
            "id":"r",
            "choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
            "usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":80}}
        }`))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

func newOpenRouterForTest(t *testing.T, baseURL string) *OpenRouterProvider {
	t.Helper()
	p, err := NewOpenRouterProvider(OpenRouterConfig{APIKey: "test", BaseURL: baseURL})
	if err != nil {
		t.Fatalf("NewOpenRouterProvider: %v", err)
	}
	return p
}

func TestOpenRouter_SystemCacheEmitsTypedParts(t *testing.T) {
	srv, cap := newOpenRouterCaptureServer(t)
	p := newOpenRouterForTest(t, srv.URL)

	_, err := p.Call(context.Background(), ProviderRequest{
		Model:       "anthropic/claude-test",
		System:      "stable instructions",
		Messages:    []Message{NewUserMessage("hi")},
		SystemCache: Ephemeral(),
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var sent map[string]any
	if err := json.Unmarshal(cap.body, &sent); err != nil {
		t.Fatalf("decode: %v", err)
	}
	msgs := sent["messages"].([]any)
	systemMsg := msgs[0].(map[string]any)
	if systemMsg["role"] != "system" {
		t.Fatalf("first message role = %v", systemMsg["role"])
	}
	parts, ok := systemMsg["content"].([]any)
	if !ok {
		t.Fatalf("system content should be typed-parts array, got %T: %v", systemMsg["content"], systemMsg["content"])
	}
	part := parts[0].(map[string]any)
	if part["type"] != "text" || part["text"] != "stable instructions" {
		t.Errorf("system text part wrong: %v", part)
	}
	if part["cache_control"] == nil {
		t.Errorf("cache_control missing on system part: %v", part)
	}
}

func TestOpenRouter_NoCacheMarkerKeepsString(t *testing.T) {
	srv, cap := newOpenRouterCaptureServer(t)
	p := newOpenRouterForTest(t, srv.URL)

	_, err := p.Call(context.Background(), ProviderRequest{
		Model:    "anthropic/claude-test",
		System:   "stable",
		Messages: []Message{NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var sent map[string]any
	json.Unmarshal(cap.body, &sent)
	msgs := sent["messages"].([]any)
	systemMsg := msgs[0].(map[string]any)
	if s, ok := systemMsg["content"].(string); !ok || s != "stable" {
		t.Errorf("expected plain-string content with no cache marker, got %v", systemMsg["content"])
	}
}

func TestOpenRouter_ToolCacheControlEmitted(t *testing.T) {
	srv, cap := newOpenRouterCaptureServer(t)
	p := newOpenRouterForTest(t, srv.URL)

	tool := NewTool("calc", "do math", Object(Number("a", "v", Required())))
	tool.CacheControl = Ephemeral()

	_, err := p.Call(context.Background(), ProviderRequest{
		Model:    "anthropic/claude-test",
		Messages: []Message{NewUserMessage("hi")},
		Tools:    []Tool{tool},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var sent map[string]any
	json.Unmarshal(cap.body, &sent)
	tools := sent["tools"].([]any)
	got := tools[0].(map[string]any)
	if got["cache_control"] == nil {
		t.Errorf("expected cache_control on tool, got %v", got)
	}
}

func TestOpenRouter_DecodesCachedTokens(t *testing.T) {
	srv, _ := newOpenRouterCaptureServer(t)
	p := newOpenRouterForTest(t, srv.URL)

	out, err := p.Call(context.Background(), ProviderRequest{
		Model:    "anthropic/claude-test",
		Messages: []Message{NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.Usage.CacheReadTokens != 80 {
		t.Errorf("CacheReadTokens = %d, want 80", out.Usage.CacheReadTokens)
	}
	if out.Usage.InputTokens != 100 || out.Usage.OutputTokens != 20 {
		t.Errorf("usage InputTokens=%d OutputTokens=%d, want 100 / 20", out.Usage.InputTokens, out.Usage.OutputTokens)
	}
}

// ---------------------------------------------------------------------------
// OpenAI Chat Completions: cache markers must NOT be emitted on the wire,
// because OpenAI doesn't recognize the field and may validate strictly.
// ---------------------------------------------------------------------------

func TestOpenAIChatCompletions_DropsCacheMarkers(t *testing.T) {
	cap := &captureRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.body = body
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer srv.Close()
	p, _ := NewOpenAIProvider(OpenAIConfig{APIKey: "t", BaseURL: srv.URL})

	tool := NewTool("calc", "do math", Object(Number("a", "v", Required())))
	tool.CacheControl = Ephemeral()
	_, err := p.Call(context.Background(), ProviderRequest{
		Model:       "gpt-test",
		System:      "sys",
		SystemCache: Ephemeral(),
		Messages:    []Message{NewUserMessage("hi")},
		Tools:       []Tool{tool},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	body := string(cap.body)
	if strings.Contains(body, "cache_control") {
		t.Errorf("OpenAI Chat Completions request should not contain cache_control, got body: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Toolset.CacheLast helper
// ---------------------------------------------------------------------------

func TestToolset_CacheLast(t *testing.T) {
	a := NewTool("a", "", Object())
	b := NewTool("b", "", Object())
	ts := Tools(Bind(a, nil), Bind(b, nil)).CacheLast(Ephemeral())
	if ts.Bindings[0].Tool.CacheControl != nil {
		t.Errorf("first tool should not be marked: %v", ts.Bindings[0].Tool.CacheControl)
	}
	if ts.Bindings[1].Tool.CacheControl == nil {
		t.Error("last tool should be marked as cache breakpoint")
	}
}

func TestToolset_CacheLastEmptyIsNoOp(t *testing.T) {
	ts := Tools().CacheLast(Ephemeral())
	if len(ts.Bindings) != 0 {
		t.Errorf("expected empty toolset, got %d", len(ts.Bindings))
	}
}

