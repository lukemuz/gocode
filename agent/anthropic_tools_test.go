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

// captureRequest stands up an httptest server that records the JSON body of
// the most recent POST and replies with a stub Anthropic response.
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

func newAnthropicProviderForTest(t *testing.T, baseURL string) *AnthropicProvider {
	t.Helper()
	p, err := NewAnthropicProvider(AnthropicConfig{APIKey: "test", BaseURL: baseURL})
	if err != nil {
		t.Fatalf("NewAnthropicProvider: %v", err)
	}
	return p
}

func TestAnthropicWebSearch_WireForm(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	p := newAnthropicProviderForTest(t, srv.URL)

	pt := AnthropicWebSearch(WebSearchOpts{
		MaxUses:        3,
		AllowedDomains: []string{"example.com"},
	})
	if pt.Provider != providerTagAnthropic {
		t.Fatalf("Provider = %q, want %q", pt.Provider, providerTagAnthropic)
	}

	_, err := p.Call(context.Background(), ProviderRequest{
		Model:         "claude-test",
		Messages:      []Message{NewUserMessage("hi")},
		ProviderTools: []ProviderTool{pt},
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
	if got["type"] != "web_search_20250305" {
		t.Errorf("type = %v, want web_search_20250305", got["type"])
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

func TestAnthropicCodeExecution_WireForm(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newAnthropicProviderForTest(t, srv.URL)

	_, err := p.Call(context.Background(), ProviderRequest{
		Model:         "claude-test",
		Messages:      []Message{NewUserMessage("hi")},
		ProviderTools: []ProviderTool{AnthropicCodeExecution()},
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

func TestAnthropicBashTool_WireFormAndDispatch(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newAnthropicProviderForTest(t, srv.URL)

	binding := AnthropicBashTool(func(_ context.Context, _ json.RawMessage) (string, error) {
		return "ran", nil
	})
	if binding.Tool.Name != "bash" {
		t.Errorf("Name = %q, want bash", binding.Tool.Name)
	}
	if binding.Tool.Provider != providerTagAnthropic {
		t.Errorf("Provider = %q, want %q", binding.Tool.Provider, providerTagAnthropic)
	}

	_, err := p.Call(context.Background(), ProviderRequest{
		Model:    "claude-test",
		Messages: []Message{NewUserMessage("hi")},
		Tools:    []Tool{binding.Tool},
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

func TestAnthropicTextEditorTool_WireForm(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newAnthropicProviderForTest(t, srv.URL)

	binding := AnthropicTextEditorTool(func(_ context.Context, _ json.RawMessage) (string, error) {
		return "", nil
	})
	if binding.Tool.Name != "str_replace_editor" {
		t.Errorf("Name = %q, want str_replace_editor", binding.Tool.Name)
	}

	_, err := p.Call(context.Background(), ProviderRequest{
		Model:    "claude-test",
		Messages: []Message{NewUserMessage("hi")},
		Tools:    []Tool{binding.Tool},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	got := sentToolsFirst(t, cap.body)
	if got["type"] != "text_editor_20250124" || got["name"] != "str_replace_editor" {
		t.Errorf("text_editor wire form wrong: %v", got)
	}
}

func TestAnthropicComputerTool_WireForm(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newAnthropicProviderForTest(t, srv.URL)

	binding := AnthropicComputerTool(
		ComputerOpts{DisplayWidthPx: 1024, DisplayHeightPx: 768, DisplayNumber: 1},
		func(_ context.Context, _ json.RawMessage) (string, error) { return "", nil },
	)
	_, err := p.Call(context.Background(), ProviderRequest{
		Model:    "claude-test",
		Messages: []Message{NewUserMessage("hi")},
		Tools:    []Tool{binding.Tool},
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

func TestAnthropicToolMixWithStandardTool(t *testing.T) {
	srv, cap := newCaptureServer(t,
		`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{}}`)
	p := newAnthropicProviderForTest(t, srv.URL)

	std := NewTool("calc", "do math", Object(Number("a", "", Required())))
	bash := AnthropicBashTool(func(_ context.Context, _ json.RawMessage) (string, error) { return "", nil })

	_, err := p.Call(context.Background(), ProviderRequest{
		Model:         "claude-test",
		Messages:      []Message{NewUserMessage("hi")},
		Tools:         []Tool{std, bash.Tool},
		ProviderTools: []ProviderTool{AnthropicWebSearch(WebSearchOpts{})},
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
	// std tool keeps input_schema; category-2 tool drops it.
	first := tools[0].(map[string]any)
	if _, has := first["input_schema"]; !has {
		t.Errorf("standard tool should include input_schema, got %v", first)
	}
	second := tools[1].(map[string]any)
	if second["type"] != "bash_20250124" {
		t.Errorf("expected bash declaration second, got %v", second)
	}
	third := tools[2].(map[string]any)
	if third["type"] != "web_search_20250305" {
		t.Errorf("expected web_search third, got %v", third)
	}
}

func TestAnthropicProvider_RejectsForeignProviderTool(t *testing.T) {
	srv, _ := newCaptureServer(t, `{"id":"msg","content":[],"stop_reason":"end_turn","usage":{}}`)
	p := newAnthropicProviderForTest(t, srv.URL)

	bad := ProviderTool{Provider: "openai", Raw: json.RawMessage(`{"type":"web_search"}`)}
	_, err := p.Call(context.Background(), ProviderRequest{
		Model:         "claude-test",
		Messages:      []Message{NewUserMessage("hi")},
		ProviderTools: []ProviderTool{bad},
	})
	if err == nil || !strings.Contains(err.Error(), "tagged for provider") {
		t.Fatalf("expected provider mismatch error, got %v", err)
	}
}

func TestOpenAIProvider_RejectsAnthropicProviderTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	p, err := NewOpenAIProvider(OpenAIConfig{APIKey: "test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}
	_, err = p.Call(context.Background(), ProviderRequest{
		Model:         "gpt-test",
		Messages:      []Message{NewUserMessage("hi")},
		ProviderTools: []ProviderTool{AnthropicWebSearch(WebSearchOpts{})},
	})
	if err == nil || !strings.Contains(err.Error(), "Responses-API") {
		t.Fatalf("expected Responses-API error from OpenAI provider, got %v", err)
	}

	_, err = p.Call(context.Background(), ProviderRequest{
		Model:    "gpt-test",
		Messages: []Message{NewUserMessage("hi")},
		Tools:    []Tool{AnthropicBashTool(nil).Tool},
	})
	if err == nil || !strings.Contains(err.Error(), "tagged for provider") {
		t.Fatalf("expected category-2 mismatch error from OpenAI provider, got %v", err)
	}
}

func TestContentBlockOpaqueRoundTrip(t *testing.T) {
	// Anthropic returns server_tool_use and web_search_tool_result blocks
	// when the hosted web_search runs. They are not in the canonical set;
	// our ContentBlock must capture them verbatim and re-emit them on the
	// next request so multi-turn conversations preserve provider history.
	wire := `[
        {"type":"text","text":"hello"},
        {"type":"server_tool_use","id":"srvtu_1","name":"web_search","input":{"query":"go"}},
        {"type":"web_search_tool_result","tool_use_id":"srvtu_1","content":[{"type":"web_search_result","title":"Go"}]}
    ]`
	var blocks []ContentBlock
	if err := json.Unmarshal([]byte(wire), &blocks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks, got %d", len(blocks))
	}
	if blocks[0].Type != TypeText || blocks[0].Text != "hello" {
		t.Errorf("text block decoded wrong: %+v", blocks[0])
	}
	if blocks[1].Type != "server_tool_use" || len(blocks[1].Raw) == 0 {
		t.Errorf("server_tool_use should be opaque: %+v", blocks[1])
	}
	if blocks[2].Type != "web_search_tool_result" || len(blocks[2].Raw) == 0 {
		t.Errorf("web_search_tool_result should be opaque: %+v", blocks[2])
	}

	// Round-trip: re-encoding the slice must reproduce semantically equal
	// JSON for opaque blocks.
	out, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	var roundTripped []map[string]any
	if err := json.Unmarshal(out, &roundTripped); err != nil {
		t.Fatalf("decode round-trip: %v", err)
	}
	if roundTripped[1]["type"] != "server_tool_use" || roundTripped[1]["id"] != "srvtu_1" {
		t.Errorf("server_tool_use lost data: %v", roundTripped[1])
	}
	if roundTripped[2]["type"] != "web_search_tool_result" || roundTripped[2]["tool_use_id"] != "srvtu_1" {
		t.Errorf("web_search_tool_result lost data: %v", roundTripped[2])
	}
}

func TestExtractToolUsesIgnoresOpaqueBlocks(t *testing.T) {
	// The agent loop must not try to dispatch server_tool_use blocks
	// locally — they are executed by the provider.
	blocks := []ContentBlock{
		{Type: TypeText, Text: "thinking"},
		{Type: "server_tool_use", Raw: json.RawMessage(`{"type":"server_tool_use","id":"x"}`)},
		{Type: TypeToolUse, ID: "tu_local", Name: "local_tool", Input: json.RawMessage(`{}`)},
	}
	uses := extractToolUses(blocks)
	if len(uses) != 1 || uses[0].ID != "tu_local" {
		t.Errorf("extractToolUses returned %v, want only local tool", uses)
	}
}

// sentToolsFirst is a helper that decodes a captured Anthropic request body
// and returns the first element of the tools array as a map.
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
