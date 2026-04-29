package research

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/lukemuz/gocode/agent"
)

// scriptedProvider is a tiny agent.Provider whose Call returns the next
// scripted response on each invocation. It lets us drive Loop deterministically
// without hitting a real API.
type scriptedProvider struct {
	mu        sync.Mutex
	responses []agent.ProviderResponse
	calls     int
}

func (p *scriptedProvider) Call(ctx context.Context, req agent.ProviderRequest) (agent.ProviderResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls >= len(p.responses) {
		// Default end_turn so tests don't hang on miscount.
		return agent.ProviderResponse{
			Content:    []agent.ContentBlock{{Type: agent.TypeText, Text: "(default end)"}},
			StopReason: "end_turn",
		}, nil
	}
	r := p.responses[p.calls]
	p.calls++
	return r, nil
}

func (p *scriptedProvider) Stream(ctx context.Context, req agent.ProviderRequest, onDelta func(agent.ContentBlock)) (agent.ProviderResponse, error) {
	return p.Call(ctx, req)
}

func newClient(t *testing.T, p agent.Provider) *agent.Client {
	t.Helper()
	c, err := agent.New(agent.Config{Provider: p, Model: "test", MaxTokens: 512})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	return c
}

// toolUse builds an assistant tool_use response that calls toolName with the
// given JSON-encodable input.
func toolUse(toolName string, input any) agent.ProviderResponse {
	raw, _ := json.Marshal(input)
	return agent.ProviderResponse{
		Content: []agent.ContentBlock{{
			Type:  agent.TypeToolUse,
			ID:    "tu_1",
			Name:  toolName,
			Input: raw,
		}},
		StopReason: "tool_use",
	}
}

func endTurn(text string) agent.ProviderResponse {
	return agent.ProviderResponse{
		Content:    []agent.ContentBlock{{Type: agent.TypeText, Text: text}},
		StopReason: "end_turn",
	}
}

func TestDecompose(t *testing.T) {
	planArgs := map[string]any{
		"reasoning": "Cover history and present.",
		"subtasks": []map[string]string{
			{"question": "What is X's history?", "rationale": "context"},
			{"question": "What is X today?", "rationale": "current state"},
		},
	}
	prov := &scriptedProvider{responses: []agent.ProviderResponse{
		toolUse("submit_plan", planArgs),
		endTurn("done"),
	}}

	plan, _, err := Decompose(context.Background(), newClient(t, prov), "Tell me about X.", 5)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if len(plan.Subtasks) != 2 {
		t.Fatalf("want 2 subtasks, got %d", len(plan.Subtasks))
	}
	if plan.Subtasks[0].ID != "s1" || plan.Subtasks[1].ID != "s2" {
		t.Fatalf("unexpected ids: %+v", plan.Subtasks)
	}
	if plan.Subtasks[0].Question == "" {
		t.Fatalf("empty subtask question")
	}
}

func TestDecompose_TooManySubtasks(t *testing.T) {
	planArgs := map[string]any{
		"subtasks": []map[string]string{
			{"question": "a"}, {"question": "b"}, {"question": "c"},
		},
	}
	// First call: tool_use that violates max=2; the tool returns is_error=true
	// to the model. Second call: model retries with a smaller plan.
	smaller := map[string]any{"subtasks": []map[string]string{{"question": "a"}}}
	prov := &scriptedProvider{responses: []agent.ProviderResponse{
		toolUse("submit_plan", planArgs),
		toolUse("submit_plan", smaller),
		endTurn("done"),
	}}

	plan, _, err := Decompose(context.Background(), newClient(t, prov), "q", 2)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if len(plan.Subtasks) != 1 {
		t.Fatalf("want 1 subtask after retry, got %d", len(plan.Subtasks))
	}
}

func TestDecompose_NeverSubmits(t *testing.T) {
	prov := &scriptedProvider{responses: []agent.ProviderResponse{
		endTurn("I refuse to use the tool."),
	}}
	_, _, err := Decompose(context.Background(), newClient(t, prov), "q", 5)
	if err == nil || !strings.Contains(err.Error(), "submit_plan") {
		t.Fatalf("want missing-submit error, got %v", err)
	}
}

func TestInvestigate(t *testing.T) {
	findings := map[string]any{
		"summary": "X is a thing. It does Y.",
		"citations": []map[string]string{
			{"url": "https://example.com/x", "title": "X explained"},
		},
	}
	prov := &scriptedProvider{responses: []agent.ProviderResponse{
		toolUse("submit_findings", findings),
		endTurn("done"),
	}}
	subtask := Subtask{ID: "s1", Question: "What is X?"}
	note, _, err := Investigate(context.Background(), newClient(t, prov), subtask, agent.Toolset{}, 5)
	if err != nil {
		t.Fatalf("Investigate: %v", err)
	}
	if note.Summary == "" || len(note.Citations) != 1 {
		t.Fatalf("bad note: %+v", note)
	}
	if note.Citations[0].URL != "https://example.com/x" {
		t.Fatalf("bad URL: %s", note.Citations[0].URL)
	}
}

func TestRun_AllWorkersFail_ReturnsError(t *testing.T) {
	// Planner emits 1 subtask. Worker never calls submit_findings.
	plan := map[string]any{"subtasks": []map[string]string{{"question": "q1"}}}
	prov := &scriptedProvider{responses: []agent.ProviderResponse{
		toolUse("submit_plan", plan),
		endTurn("planner done"),
		// Worker run starts here; it just ends without submitting.
		endTurn("I'm not going to search."),
	}}
	c := newClient(t, prov)
	cfg := Config{
		Planner: c, Worker: c, Synthesizer: c,
		MaxSubtasks: 3, WorkerMaxIter: 2,
	}
	_, err := Run(context.Background(), cfg, "q")
	if err == nil || !strings.Contains(err.Error(), "every worker failed") {
		t.Fatalf("want all-failed error, got %v", err)
	}
}

func TestBuildSynthesisPrompt(t *testing.T) {
	notes := []Note{
		{SubtaskID: "s1", Question: "q1", Summary: "A", Citations: []Citation{{URL: "u1"}}},
		{SubtaskID: "s2", Question: "q2", Err: "boom"},
	}
	out := buildSynthesisPrompt("Why?", notes)
	for _, want := range []string{"Why?", "Note 1", "Note 2", "u1", "boom"} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}
