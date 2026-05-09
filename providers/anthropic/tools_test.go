package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lukemuz/luft"
)

// captureRequest records the JSON body of the most recent POST so tests can
// assert on the wire shape. Shared by tools_test and cache-related tests.
type captureRequest struct {
	body []byte
}

func newCaptureServer(t *testing.T, reply string) (*httptest.Server, *captureRequest) {
	t.Helper()
	cap := &captureRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		cap.body = b
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(reply))
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

// ---------------------------------------------------------------------------
// Category-1 (server-executed) wire emission
// ---------------------------------------------------------------------------

func TestWebSearch_WireForm(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	p := newProviderForTest(t, srv.URL)

	pt := WebSearch(WebSearchOpts{
		MaxUses:        3,
		AllowedDomains: []string{"example.com"},
	})
	if pt.Provider != ProviderTag {
		t.Fatalf("Provider = %q, want %q", pt.Provider, ProviderTag)
	}

	_, err := p.Call(context.Background(), luft.ProviderRequest{
		Model:         "claude-test",
		Messages:      []luft.Message{luft.NewUserMessage("hi")},
		ProviderTools: []luft.ProviderTool{pt},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(cap.body, &sent); err != nil {
		t.Fatalf("decode wire body: %v", err)
	}
	tools, ok := sent["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools missing or wrong length: %v", sent["tools"])
	}
	got := tools[0].(map[string]any)
	if got["type"] != "web_search_20260209" {
		t.Errorf("type = %v, want web_search_20260209", got["type"])
	}
	if got["name"] != "web_search" {
		t.Errorf("name = %v, want web_search", got["name"])
	}
	if mu, _ := got["max_uses"].(float64); int(mu) != 3 {
		t.Errorf("max_uses = %v, want 3", got["max_uses"])
	}
	if dom, _ := got["allowed_domains"].([]any); len(dom) != 1 || dom[0] != "example.com" {
		t.Errorf("allowed_domains = %v, want [example.com]", got["allowed_domains"])
	}
}

func TestCodeExecution_WireForm(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newProviderForTest(t, srv.URL)

	_, err := p.Call(context.Background(), luft.ProviderRequest{
		Model:         "claude-test",
		Messages:      []luft.Message{luft.NewUserMessage("hi")},
		ProviderTools: []luft.ProviderTool{CodeExecution()},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var sent map[string]any
	json.Unmarshal(cap.body, &sent)
	tools := sent["tools"].([]any)
	got := tools[0].(map[string]any)
	if got["type"] != "code_execution_20250522" {
		t.Errorf("type = %v", got["type"])
	}
	if got["name"] != "code_execution" {
		t.Errorf("name = %v", got["name"])
	}
}

// ---------------------------------------------------------------------------
// Category-2 (provider-defined schema, client-executed) wire emission
// ---------------------------------------------------------------------------

func TestBashTool_WireFormAndDispatch(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newProviderForTest(t, srv.URL)

	binding := BashTool(func(_ context.Context, _ json.RawMessage) (string, error) {
		return "ran", nil
	})
	if binding.Tool.Name != "bash" {
		t.Errorf("Name = %q, want bash", binding.Tool.Name)
	}
	if binding.Tool.Provider != ProviderTag {
		t.Errorf("Provider = %q, want %q", binding.Tool.Provider, ProviderTag)
	}

	_, err := p.Call(context.Background(), luft.ProviderRequest{
		Model:    "claude-test",
		Messages: []luft.Message{luft.NewUserMessage("hi")},
		Tools:    []luft.Tool{binding.Tool},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var sent map[string]any
	json.Unmarshal(cap.body, &sent)
	got := sent["tools"].([]any)[0].(map[string]any)
	if got["type"] != "bash_20250124" {
		t.Errorf("type = %v, want bash_20250124", got["type"])
	}
	if got["name"] != "bash" {
		t.Errorf("name = %v", got["name"])
	}
	// Category-2 declarations must NOT carry input_schema or description.
	if _, has := got["input_schema"]; has {
		t.Errorf("category-2 wire form should omit input_schema, got %v", got)
	}
	if _, has := got["description"]; has {
		t.Errorf("category-2 wire form should omit description, got %v", got)
	}
}

func TestTextEditorTool_WireForm(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newProviderForTest(t, srv.URL)

	binding := TextEditorTool(func(_ context.Context, _ json.RawMessage) (string, error) {
		return "", nil
	})
	if binding.Tool.Name != "str_replace_editor" {
		t.Errorf("Name = %q, want str_replace_editor", binding.Tool.Name)
	}

	_, err := p.Call(context.Background(), luft.ProviderRequest{
		Model:    "claude-test",
		Messages: []luft.Message{luft.NewUserMessage("hi")},
		Tools:    []luft.Tool{binding.Tool},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	got := sentToolsFirst(t, cap.body)
	if got["type"] != "text_editor_20250124" || got["name"] != "str_replace_editor" {
		t.Errorf("text_editor wire form wrong: %v", got)
	}
}

func TestComputerTool_WireForm(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newProviderForTest(t, srv.URL)

	binding := ComputerTool(
		ComputerOpts{DisplayWidthPx: 1024, DisplayHeightPx: 768, DisplayNumber: 1},
		func(_ context.Context, _ json.RawMessage) (string, error) { return "", nil },
	)
	_, err := p.Call(context.Background(), luft.ProviderRequest{
		Model:    "claude-test",
		Messages: []luft.Message{luft.NewUserMessage("hi")},
		Tools:    []luft.Tool{binding.Tool},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	got := sentToolsFirst(t, cap.body)
	if got["type"] != "computer_20250124" {
		t.Errorf("type = %v", got["type"])
	}
	if w, _ := got["display_width_px"].(float64); int(w) != 1024 {
		t.Errorf("display_width_px = %v, want 1024", got["display_width_px"])
	}
	if h, _ := got["display_height_px"].(float64); int(h) != 768 {
		t.Errorf("display_height_px = %v, want 768", got["display_height_px"])
	}
}

func TestToolMixWithStandardTool(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newProviderForTest(t, srv.URL)

	std := luft.NewTool("calc", "do math",
		luft.Object(luft.Number("a", "", luft.Required())))
	bash := BashTool(func(_ context.Context, _ json.RawMessage) (string, error) { return "", nil })

	_, err := p.Call(context.Background(), luft.ProviderRequest{
		Model:         "claude-test",
		Messages:      []luft.Message{luft.NewUserMessage("hi")},
		Tools:         []luft.Tool{std, bash.Tool},
		ProviderTools: []luft.ProviderTool{WebSearch(WebSearchOpts{})},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var sent map[string]any
	json.Unmarshal(cap.body, &sent)
	tools := sent["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("want 3 tools (std + bash + web_search), got %d: %v", len(tools), tools)
	}
	first := tools[0].(map[string]any)
	if _, has := first["input_schema"]; !has {
		t.Errorf("standard tool should include input_schema, got %v", first)
	}
	second := tools[1].(map[string]any)
	if second["type"] != "bash_20250124" {
		t.Errorf("expected bash declaration second, got %v", second)
	}
	third := tools[2].(map[string]any)
	if third["type"] != "web_search_20260209" {
		t.Errorf("expected web_search third, got %v", third)
	}
}

func TestProvider_RejectsForeignProviderTool(t *testing.T) {
	srv, _ := newCaptureServer(t, `{"id":"msg","content":[],"stop_reason":"end_turn","usage":{}}`)
	p := newProviderForTest(t, srv.URL)

	bad := luft.ProviderTool{Provider: "openai", Raw: json.RawMessage(`{"type":"web_search"}`)}
	_, err := p.Call(context.Background(), luft.ProviderRequest{
		Model:         "claude-test",
		Messages:      []luft.Message{luft.NewUserMessage("hi")},
		ProviderTools: []luft.ProviderTool{bad},
	})
	if err == nil || !strings.Contains(err.Error(), "tagged for provider") {
		t.Fatalf("expected provider mismatch error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Cache emission (system prompt, tools, message blocks) + cache-usage decode.
// ---------------------------------------------------------------------------

func TestSystemCacheEmitsArrayForm(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newProviderForTest(t, srv.URL)

	_, err := p.Call(context.Background(), luft.ProviderRequest{
		Model:       "claude-test",
		System:      "stable instructions",
		Messages:    []luft.Message{luft.NewUserMessage("hi")},
		SystemCache: luft.Ephemeral(),
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

func TestNoSystemCacheKeepsString(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newProviderForTest(t, srv.URL)

	_, err := p.Call(context.Background(), luft.ProviderRequest{
		Model:    "claude-test",
		System:   "stable instructions",
		Messages: []luft.Message{luft.NewUserMessage("hi")},
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

func TestToolCacheControlEmitted(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newProviderForTest(t, srv.URL)

	tool := luft.NewTool("calc", "do math",
		luft.Object(luft.Number("a", "v", luft.Required())))
	tool.CacheControl = luft.EphemeralExtended()

	_, err := p.Call(context.Background(), luft.ProviderRequest{
		Model:    "claude-test",
		Messages: []luft.Message{luft.NewUserMessage("hi")},
		Tools:    []luft.Tool{tool},
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

func TestMessageCacheControlEmitted(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newProviderForTest(t, srv.URL)

	msg := luft.Message{Role: luft.RoleUser, Content: []luft.ContentBlock{
		{Type: luft.TypeText, Text: "long context", CacheControl: luft.Ephemeral()},
	}}
	_, err := p.Call(context.Background(), luft.ProviderRequest{
		Model:    "claude-test",
		Messages: []luft.Message{msg},
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

func TestDecodesCacheUsage(t *testing.T) {
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
	p := newProviderForTest(t, srv.URL)

	out, err := p.Call(context.Background(), luft.ProviderRequest{
		Model:    "claude-test",
		Messages: []luft.Message{luft.NewUserMessage("hi")},
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

// sentToolsFirst decodes a captured Anthropic request body and returns the
// first element of the tools array as a map.
func sentToolsFirst(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode wire body: %v", err)
	}
	tools, ok := sent["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("no tools in wire body: %v", sent)
	}
	return tools[0].(map[string]any)
}
