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

// responsesCapture records the request body for assertion.
type responsesCapture struct {
	body []byte
}

// stubResponsesServer captures the request body and replies with the given
// pre-built JSON response.
func stubResponsesServer(t *testing.T, reply string) (*httptest.Server, *responsesCapture) {
	t.Helper()
	cap := &responsesCapture{}
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

func newResponsesProviderForTest(t *testing.T, baseURL string) *ResponsesProvider {
	t.Helper()
	p, err := NewResponsesProvider(ResponsesConfig{APIKey: "test", BaseURL: baseURL})
	if err != nil {
		t.Fatalf("NewResponsesProvider: %v", err)
	}
	return p
}

func TestResponses_BasicTextRoundTrip(t *testing.T) {
	reply := `{
        "id":"resp_1",
        "status":"completed",
        "output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello!"}]}],
        "usage":{"input_tokens":12,"output_tokens":3}
    }`
	srv, cap := stubResponsesServer(t, reply)
	p := newResponsesProviderForTest(t, srv.URL)

	out, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    "gpt-test",
		System:   "be brief",
		Messages: []gocode.Message{gocode.NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.StopReason != "end_turn" {
		t.Errorf("stop = %q, want end_turn", out.StopReason)
	}
	if out.Usage.InputTokens != 12 || out.Usage.OutputTokens != 3 {
		t.Errorf("usage = %+v", out.Usage)
	}
	if len(out.Content) != 1 || out.Content[0].Type != gocode.TypeText || out.Content[0].Text != "Hello!" {
		t.Errorf("content = %+v", out.Content)
	}

	var sent map[string]any
	if err := json.Unmarshal(cap.body, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent["model"] != "gpt-test" {
		t.Errorf("model = %v", sent["model"])
	}
	if sent["instructions"] != "be brief" {
		t.Errorf("instructions = %v", sent["instructions"])
	}
	input, ok := sent["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input = %v", sent["input"])
	}
	first := input[0].(map[string]any)
	if first["role"] != "user" {
		t.Errorf("role = %v", first["role"])
	}
	parts := first["content"].([]any)
	if parts[0].(map[string]any)["type"] != "input_text" {
		t.Errorf("user text part type = %v", parts[0])
	}
}

func TestResponses_FunctionCallTriggersToolUse(t *testing.T) {
	reply := `{
        "id":"resp_2",
        "status":"completed",
        "output":[{"id":"fc_1","type":"function_call","call_id":"call_abc","name":"calc","arguments":"{\"a\":1}"}],
        "usage":{"input_tokens":5,"output_tokens":7}
    }`
	srv, _ := stubResponsesServer(t, reply)
	p := newResponsesProviderForTest(t, srv.URL)

	out, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    "gpt-test",
		Messages: []gocode.Message{gocode.NewUserMessage("compute")},
		Tools: []gocode.Tool{gocode.NewTool("calc", "compute", gocode.Object(
			gocode.Number("a", "value", gocode.Required()),
		))},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.StopReason != "tool_use" {
		t.Fatalf("stop = %q, want tool_use", out.StopReason)
	}
	if len(out.Content) != 1 || out.Content[0].Type != gocode.TypeToolUse {
		t.Fatalf("content = %+v", out.Content)
	}
	got := out.Content[0]
	if got.ID != "call_abc" || got.Name != "calc" || string(got.Input) != `{"a":1}` {
		t.Errorf("tool_use = %+v", got)
	}
}

func TestResponses_ToolResultRoundTripsAsFunctionCallOutput(t *testing.T) {
	srv, cap := stubResponsesServer(t,
		`{"id":"r","status":"completed","output":[],"usage":{}}`)
	p := newResponsesProviderForTest(t, srv.URL)

	history := []gocode.Message{
		gocode.NewUserMessage("hi"),
		{Role: gocode.RoleAssistant, Content: []gocode.ContentBlock{{
			Type:  gocode.TypeToolUse,
			ID:    "call_1",
			Name:  "calc",
			Input: json.RawMessage(`{"a":1}`),
		}}},
		gocode.NewToolResultMessage([]gocode.ToolResult{{ToolUseID: "call_1", Content: "42"}}),
	}
	_, err := p.Call(context.Background(), gocode.ProviderRequest{Model: "gpt-test", Messages: history})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var sent map[string]any
	json.Unmarshal(cap.body, &sent)
	input := sent["input"].([]any)
	if len(input) != 3 {
		t.Fatalf("expected 3 input items, got %d: %v", len(input), input)
	}
	fc := input[1].(map[string]any)
	if fc["type"] != "function_call" || fc["call_id"] != "call_1" || fc["name"] != "calc" {
		t.Errorf("function_call wrong: %v", fc)
	}
	if fc["arguments"] != `{"a":1}` {
		t.Errorf("arguments = %v", fc["arguments"])
	}
	out := input[2].(map[string]any)
	if out["type"] != "function_call_output" || out["call_id"] != "call_1" || out["output"] != "42" {
		t.Errorf("function_call_output wrong: %v", out)
	}
}

func TestResponses_HostedToolWireForm(t *testing.T) {
	srv, cap := stubResponsesServer(t,
		`{"id":"r","status":"completed","output":[],"usage":{}}`)
	p := newResponsesProviderForTest(t, srv.URL)

	_, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    "gpt-test",
		Messages: []gocode.Message{gocode.NewUserMessage("search")},
		ProviderTools: []gocode.ProviderTool{
			WebSearch(),
			CodeInterpreter(CodeInterpreterOpts{}),
			FileSearch(FileSearchOpts{
				VectorStoreIDs: []string{"vs_1", "vs_2"},
				MaxNumResults:  10,
			}),
			ImageGeneration(),
		},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var sent map[string]any
	json.Unmarshal(cap.body, &sent)
	tools := sent["tools"].([]any)
	if len(tools) != 4 {
		t.Fatalf("want 4 tools, got %d: %v", len(tools), tools)
	}
	if tools[0].(map[string]any)["type"] != "web_search" {
		t.Errorf("web_search wrong: %v", tools[0])
	}
	ci := tools[1].(map[string]any)
	if ci["type"] != "code_interpreter" {
		t.Errorf("code_interpreter type wrong: %v", ci)
	}
	if c, ok := ci["container"].(map[string]any); !ok || c["type"] != "auto" {
		t.Errorf("code_interpreter container = %v, want {type: auto}", ci["container"])
	}
	fs := tools[2].(map[string]any)
	if fs["type"] != "file_search" {
		t.Errorf("file_search type wrong: %v", fs)
	}
	ids := fs["vector_store_ids"].([]any)
	if len(ids) != 2 || ids[0] != "vs_1" || ids[1] != "vs_2" {
		t.Errorf("vector_store_ids = %v", ids)
	}
	if mr, _ := fs["max_num_results"].(float64); int(mr) != 10 {
		t.Errorf("max_num_results = %v", fs["max_num_results"])
	}
	if tools[3].(map[string]any)["type"] != "image_generation" {
		t.Errorf("image_generation wrong: %v", tools[3])
	}
}

func TestResponses_FunctionToolFlatShape(t *testing.T) {
	srv, cap := stubResponsesServer(t,
		`{"id":"r","status":"completed","output":[],"usage":{}}`)
	p := newResponsesProviderForTest(t, srv.URL)

	_, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    "gpt-test",
		Messages: []gocode.Message{gocode.NewUserMessage("hi")},
		Tools: []gocode.Tool{gocode.NewTool("calc", "compute", gocode.Object(
			gocode.Number("a", "v", gocode.Required()),
		))},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var sent map[string]any
	json.Unmarshal(cap.body, &sent)
	tools := sent["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("want 1 tool, got %d", len(tools))
	}
	got := tools[0].(map[string]any)
	if got["type"] != "function" {
		t.Errorf("type = %v", got["type"])
	}
	if _, has := got["function"]; has {
		t.Errorf("Responses API expects flat function tools, got nested: %v", got)
	}
	if got["name"] != "calc" {
		t.Errorf("name = %v", got["name"])
	}
	if got["description"] != "compute" {
		t.Errorf("description = %v", got["description"])
	}
	if _, ok := got["parameters"].(map[string]any); !ok {
		t.Errorf("parameters missing or wrong shape: %v", got["parameters"])
	}
}

func TestResponses_OpaqueOutputRoundTrip(t *testing.T) {
	reply := `{
        "id":"resp_3",
        "status":"completed",
        "output":[
            {"id":"ws_1","type":"web_search_call","status":"completed"},
            {"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}
        ],
        "usage":{"input_tokens":5,"output_tokens":2}
    }`
	srv, _ := stubResponsesServer(t, reply)
	p := newResponsesProviderForTest(t, srv.URL)
	out, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    "gpt-test",
		Messages: []gocode.Message{gocode.NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(out.Content) != 2 {
		t.Fatalf("want 2 blocks, got %d: %+v", len(out.Content), out.Content)
	}
	if out.Content[0].Type != "web_search_call" || len(out.Content[0].Raw) == 0 {
		t.Errorf("web_search_call should be opaque: %+v", out.Content[0])
	}
	if out.Content[1].Type != gocode.TypeText || out.Content[1].Text != "done" {
		t.Errorf("message text wrong: %+v", out.Content[1])
	}

	srv2, cap := stubResponsesServer(t,
		`{"id":"r","status":"completed","output":[],"usage":{}}`)
	p2 := newResponsesProviderForTest(t, srv2.URL)
	history := []gocode.Message{
		gocode.NewUserMessage("hi"),
		{Role: gocode.RoleAssistant, Content: out.Content},
	}
	_, err = p2.Call(context.Background(), gocode.ProviderRequest{Model: "gpt-test", Messages: history})
	if err != nil {
		t.Fatalf("Call (round-trip): %v", err)
	}
	var sent map[string]any
	json.Unmarshal(cap.body, &sent)
	input := sent["input"].([]any)
	if len(input) != 3 {
		t.Fatalf("expected 3 input items on round-trip, got %d: %v", len(input), input)
	}
	if input[1].(map[string]any)["type"] != "web_search_call" || input[1].(map[string]any)["id"] != "ws_1" {
		t.Errorf("opaque round-trip lost data: %v", input[1])
	}
}

func TestResponses_StatusIncompleteMaxTokens(t *testing.T) {
	reply := `{
        "id":"resp",
        "status":"incomplete",
        "incomplete_details":{"reason":"max_output_tokens"},
        "output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}],
        "usage":{"input_tokens":1,"output_tokens":1}
    }`
	srv, _ := stubResponsesServer(t, reply)
	p := newResponsesProviderForTest(t, srv.URL)
	out, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    "gpt-test",
		Messages: []gocode.Message{gocode.NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.StopReason != "max_tokens" {
		t.Errorf("stop = %q, want max_tokens", out.StopReason)
	}
}

func TestResponses_RejectsForeignProviderTool(t *testing.T) {
	srv, _ := stubResponsesServer(t,
		`{"id":"r","status":"completed","output":[],"usage":{}}`)
	p := newResponsesProviderForTest(t, srv.URL)

	bad := gocode.ProviderTool{
		Provider: "anthropic",
		Raw:      json.RawMessage(`{"type":"web_search_20250305","name":"web_search"}`),
	}
	_, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:         "gpt-test",
		Messages:      []gocode.Message{gocode.NewUserMessage("hi")},
		ProviderTools: []gocode.ProviderTool{bad},
	})
	if err == nil || !strings.Contains(err.Error(), "tagged for provider") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func TestResponses_ChatCompletionsRejectsResponsesTool(t *testing.T) {
	// Symmetric: a Responses-tagged provider tool is rejected by the
	// stock Chat Completions Provider.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	p, _ := NewProvider(Config{APIKey: "t", BaseURL: srv.URL})
	_, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:         "gpt",
		Messages:      []gocode.Message{gocode.NewUserMessage("hi")},
		ProviderTools: []gocode.ProviderTool{WebSearch()},
	})
	if err == nil || !strings.Contains(err.Error(), "Responses-API") {
		t.Fatalf("expected guidance about Responses-API, got %v", err)
	}
}

func TestResponses_StreamTextDeltas(t *testing.T) {
	lines := []string{
		`data: {"type":"response.created","response":{"id":"r","status":"in_progress"}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant"}}`,
		`data: {"type":"response.output_text.delta","output_index":0,"item_id":"msg_1","delta":"Hel"}`,
		`data: {"type":"response.output_text.delta","output_index":0,"item_id":"msg_1","delta":"lo"}`,
		`data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}],"usage":{"input_tokens":4,"output_tokens":2}}}`,
		`data: [DONE]`,
	}
	srv := testServerForStream(t, lines)
	defer srv.Close()
	p := newResponsesProviderForTest(t, srv.URL)

	var deltas []gocode.ContentBlock
	out, err := p.Stream(context.Background(),
		gocode.ProviderRequest{Model: "gpt-test", Messages: []gocode.Message{gocode.NewUserMessage("hi")}},
		func(b gocode.ContentBlock) { deltas = append(deltas, b) },
	)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	textDeltas := 0
	for _, d := range deltas {
		if d.Type == gocode.TypeText {
			textDeltas++
		}
	}
	if textDeltas != 2 {
		t.Errorf("want 2 text deltas, got %d (%+v)", textDeltas, deltas)
	}
	if out.StopReason != "end_turn" {
		t.Errorf("stop = %q", out.StopReason)
	}
	if out.Usage.InputTokens != 4 || out.Usage.OutputTokens != 2 {
		t.Errorf("usage = %+v", out.Usage)
	}
	if len(out.Content) != 1 || out.Content[0].Type != gocode.TypeText || out.Content[0].Text != "Hello" {
		t.Errorf("content = %+v", out.Content)
	}
}

func TestResponses_StreamFunctionCallArgs(t *testing.T) {
	lines := []string{
		`data: {"type":"response.created","response":{"id":"r","status":"in_progress"}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_x","name":"calc"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"a\":"}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"1}"}`,
		`data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[{"id":"fc_1","type":"function_call","call_id":"call_x","name":"calc","arguments":"{\"a\":1}"}],"usage":{"input_tokens":3,"output_tokens":4}}}`,
		`data: [DONE]`,
	}
	srv := testServerForStream(t, lines)
	defer srv.Close()
	p := newResponsesProviderForTest(t, srv.URL)

	var deltas []gocode.ContentBlock
	out, err := p.Stream(context.Background(),
		gocode.ProviderRequest{Model: "gpt-test", Messages: []gocode.Message{gocode.NewUserMessage("compute")}},
		func(b gocode.ContentBlock) { deltas = append(deltas, b) },
	)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if out.StopReason != "tool_use" {
		t.Errorf("stop = %q, want tool_use", out.StopReason)
	}
	if len(out.Content) != 1 || out.Content[0].Type != gocode.TypeToolUse {
		t.Fatalf("content = %+v", out.Content)
	}
	got := out.Content[0]
	if got.ID != "call_x" || got.Name != "calc" || string(got.Input) != `{"a":1}` {
		t.Errorf("tool_use = %+v", got)
	}

	sawAccum := false
	for _, d := range deltas {
		if d.Type == gocode.TypeToolUse && string(d.Input) == `{"a":1}` {
			sawAccum = true
		}
	}
	if !sawAccum {
		t.Errorf("expected accumulated tool_use delta, got %+v", deltas)
	}
}

// TestResponses_ChatCompletions_DropsCacheMarkers verifies the existing
// Chat Completions Provider strips cache markers (which OpenAI doesn't
// recognize) before sending. Lives here because it exercises Provider in
// the same package.
func TestResponses_ChatCompletions_DropsCacheMarkers(t *testing.T) {
	cap := &responsesCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.body = body
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer srv.Close()
	p, _ := NewProvider(Config{APIKey: "t", BaseURL: srv.URL})

	tool := gocode.NewTool("calc", "do math",
		gocode.Object(gocode.Number("a", "v", gocode.Required())))
	tool.CacheControl = gocode.Ephemeral()
	_, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:       "gpt-test",
		System:      "sys",
		SystemCache: gocode.Ephemeral(),
		Messages:    []gocode.Message{gocode.NewUserMessage("hi")},
		Tools:       []gocode.Tool{tool},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	body := string(cap.body)
	if strings.Contains(body, "cache_control") {
		t.Errorf("OpenAI Chat Completions request should not contain cache_control, got body: %s", body)
	}
}
