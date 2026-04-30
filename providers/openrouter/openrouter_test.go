package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lukemuz/gocode"
)

type captureRequest struct {
	body []byte
}

// newCaptureServer returns an httptest server that records the JSON body of
// the most recent POST and replies with a stub OpenAI-compatible response.
func newCaptureServer(t *testing.T) (*httptest.Server, *captureRequest) {
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

func newProviderForTest(t *testing.T, baseURL string) *Provider {
	t.Helper()
	p, err := NewProvider(Config{APIKey: "test", BaseURL: baseURL})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p
}

func TestSystemCacheEmitsTypedParts(t *testing.T) {
	srv, cap := newCaptureServer(t)
	p := newProviderForTest(t, srv.URL)

	_, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:       "anthropic/claude-test",
		System:      "stable instructions",
		Messages:    []gocode.Message{gocode.NewUserMessage("hi")},
		SystemCache: gocode.Ephemeral(),
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

func TestNoCacheMarkerKeepsString(t *testing.T) {
	srv, cap := newCaptureServer(t)
	p := newProviderForTest(t, srv.URL)

	_, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    "anthropic/claude-test",
		System:   "stable",
		Messages: []gocode.Message{gocode.NewUserMessage("hi")},
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

func TestToolCacheControlEmitted(t *testing.T) {
	srv, cap := newCaptureServer(t)
	p := newProviderForTest(t, srv.URL)

	tool := gocode.NewTool("calc", "do math",
		gocode.Object(gocode.Number("a", "v", gocode.Required())))
	tool.CacheControl = gocode.Ephemeral()

	_, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    "anthropic/claude-test",
		Messages: []gocode.Message{gocode.NewUserMessage("hi")},
		Tools:    []gocode.Tool{tool},
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

func TestDecodesCachedTokens(t *testing.T) {
	srv, _ := newCaptureServer(t)
	p := newProviderForTest(t, srv.URL)

	out, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    "anthropic/claude-test",
		Messages: []gocode.Message{gocode.NewUserMessage("hi")},
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
