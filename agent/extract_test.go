package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// scriptToolUse builds an assistant tool_use response.
func scriptToolUse(toolName string, input any) ProviderResponse {
	raw, _ := json.Marshal(input)
	return ProviderResponse{
		Content: []ContentBlock{{
			Type:  TypeToolUse,
			ID:    "tu_" + toolName,
			Name:  toolName,
			Input: raw,
		}},
		StopReason: "tool_use",
	}
}

func scriptText(text string) ProviderResponse {
	return ProviderResponse{
		Content:    []ContentBlock{{Type: TypeText, Text: text}},
		StopReason: "end_turn",
	}
}

func TestTerminal_ShortCircuitsLoop(t *testing.T) {
	// First turn: model calls submit; second turn would have been "end_turn"
	// but Loop should terminate after the successful tool call.
	p := &testProvider{
		Responses: []ProviderResponse{
			scriptToolUse("submit", map[string]string{"value": "ok"}),
			// If Loop continues past the terminal tool, it will hit Resp
			// (default end_turn) — count CallCount to confirm it didn't.
		},
	}
	c, _ := New(Config{Provider: p, Model: "test"})

	called := false
	submitFn := TypedToolFunc(func(_ context.Context, in struct {
		Value string `json:"value"`
	}) (string, error) {
		called = true
		return "ok", nil
	})
	tool := NewTool("submit", "submit", Object(String("value", "")))

	tools := Tools(ToolBinding{
		Tool: tool, Func: submitFn,
		Meta: ToolMetadata{Terminal: true},
	})

	result, err := c.Loop(context.Background(), "", []Message{NewUserMessage("hi")}, tools, 5)
	if err != nil {
		t.Fatalf("Loop: %v", err)
	}
	if !called {
		t.Fatal("submit was not called")
	}
	if p.CallCount != 1 {
		t.Errorf("expected 1 model call, got %d (loop didn't short-circuit)", p.CallCount)
	}
	// Last message should be the tool_result, not an assistant text turn.
	last := result.Messages[len(result.Messages)-1]
	if last.Role != RoleUser || last.Content[0].Type != TypeToolResult {
		t.Errorf("last message should be tool_result, got %+v", last)
	}
}

func TestTerminal_ErrorResultDoesNotTerminate(t *testing.T) {
	// First turn: submit fails (validation). Second turn: model retries
	// successfully. Loop should run twice and then exit.
	p := &testProvider{
		Responses: []ProviderResponse{
			scriptToolUse("submit", map[string]string{"value": "bad"}),
			scriptToolUse("submit", map[string]string{"value": "good"}),
		},
	}
	c, _ := New(Config{Provider: p, Model: "test"})

	submitFn := TypedToolFunc(func(_ context.Context, in struct {
		Value string `json:"value"`
	}) (string, error) {
		if in.Value == "bad" {
			return "", &simpleErr{"value cannot be 'bad'"}
		}
		return "ok", nil
	})
	tool := NewTool("submit", "submit", Object(String("value", "")))
	tools := Tools(ToolBinding{Tool: tool, Func: submitFn, Meta: ToolMetadata{Terminal: true}})

	_, err := c.Loop(context.Background(), "", []Message{NewUserMessage("hi")}, tools, 5)
	if err != nil {
		t.Fatalf("Loop: %v", err)
	}
	if p.CallCount != 2 {
		t.Errorf("expected 2 calls (retry after error), got %d", p.CallCount)
	}
}

func TestExtract_Success(t *testing.T) {
	type Plan struct {
		Steps []string `json:"steps"`
	}

	args := map[string]any{"steps": []string{"a", "b", "c"}}
	resp := scriptToolUse("submit", args)
	resp.Usage = Usage{InputTokens: 7, OutputTokens: 3}
	p := &testProvider{Responses: []ProviderResponse{resp}}
	c, _ := New(Config{Provider: p, Model: "test"})

	plan, result, err := Extract[Plan](context.Background(), c, "be helpful",
		[]Message{NewUserMessage("plan it")},
		ExtractParams[Plan]{
			Description: "Submit a plan",
			Schema: Object(
				Array("steps", "ordered steps", SchemaProperty{Type: "string"}, Required()),
			),
		})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(plan.Steps) != 3 || plan.Steps[0] != "a" {
		t.Errorf("bad plan: %+v", plan)
	}
	if result.Usage.InputTokens == 0 && result.Usage.OutputTokens == 0 {
		t.Errorf("expected usage, got %+v", result.Usage)
	}
}

func TestExtract_NeverSubmits(t *testing.T) {
	type X struct {
		V string `json:"v"`
	}
	p := &testProvider{Responses: []ProviderResponse{scriptText("I refuse.")}}
	c, _ := New(Config{Provider: p, Model: "test"})

	_, _, err := Extract[X](context.Background(), c, "", []Message{NewUserMessage("go")},
		ExtractParams[X]{
			Description: "Submit X",
			Schema:      Object(String("v", "value")),
			Name:        "submit_x",
		})
	if err == nil || !strings.Contains(err.Error(), "submit_x") {
		t.Errorf("want missing-submit error mentioning submit_x, got %v", err)
	}
}

func TestExtract_ValidationRetry(t *testing.T) {
	type X struct {
		N int `json:"n"`
	}
	p := &testProvider{Responses: []ProviderResponse{
		scriptToolUse("submit", map[string]int{"n": 5}),  // rejected
		scriptToolUse("submit", map[string]int{"n": 50}), // accepted
	}}
	c, _ := New(Config{Provider: p, Model: "test"})

	got, _, err := Extract[X](context.Background(), c, "", []Message{NewUserMessage("go")},
		ExtractParams[X]{
			Description: "Submit X",
			Schema:      Object(Integer("n", "")),
			Validate: func(x X) error {
				if x.N < 10 {
					return &simpleErr{"n must be >= 10"}
				}
				return nil
			},
		})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got.N != 50 {
		t.Errorf("got n=%d, want 50", got.N)
	}
	if p.CallCount != 2 {
		t.Errorf("expected 2 model calls (retry after validation), got %d", p.CallCount)
	}
}

func TestExtract_RequiresDescription(t *testing.T) {
	type X struct{ V string }
	c, _ := New(Config{Provider: newTestProvider(), Model: "test"})
	_, _, err := Extract[X](context.Background(), c, "", nil, ExtractParams[X]{
		Schema: Object(),
	})
	if err == nil || !strings.Contains(err.Error(), "Description") {
		t.Errorf("want missing-description error, got %v", err)
	}
}

func TestTools_Constructor(t *testing.T) {
	t1 := NewTool("a", "", Object())
	t2 := NewTool("b", "", Object())
	ts := Tools(Bind(t1, nil), Bind(t2, nil))
	if len(ts.Bindings) != 2 || ts.Bindings[0].Tool.Name != "a" || ts.Bindings[1].Tool.Name != "b" {
		t.Errorf("Tools(...) didn't preserve order: %+v", ts.Bindings)
	}
}

type simpleErr struct{ msg string }

func (e *simpleErr) Error() string { return e.msg }
