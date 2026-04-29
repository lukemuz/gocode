package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMemoryRecorder_AssignsMonotonicSeq(t *testing.T) {
	rec := NewMemoryRecorder()
	rec.Record(context.Background(), Event{Type: EventTurnStart})
	rec.Record(context.Background(), Event{Type: EventModelRequest})
	rec.Record(context.Background(), Event{Type: EventTurnEnd})
	evs := rec.Events()
	if len(evs) != 3 {
		t.Fatalf("want 3 events, got %d", len(evs))
	}
	for i, ev := range evs {
		if ev.Seq != int64(i+1) {
			t.Errorf("event %d: want Seq=%d, got %d", i, i+1, ev.Seq)
		}
		if ev.Time.IsZero() {
			// emit() sets Time, but Record alone does not. That's fine —
			// callers go through emit() in production.
		}
	}
}

func TestMemoryRecorder_ConcurrentSafe(t *testing.T) {
	rec := NewMemoryRecorder()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec.Record(context.Background(), Event{Type: EventToolCallStart})
		}()
	}
	wg.Wait()
	if len(rec.Events()) != 100 {
		t.Fatalf("want 100 events, got %d", len(rec.Events()))
	}
}

func TestJSONLRecorder_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	rec := NewJSONLRecorder(&buf)
	in := []Event{
		{Type: EventTurnStart, TurnID: "t1"},
		{Type: EventModelRequest, TurnID: "t1", Iter: 0},
		{Type: EventTurnEnd, TurnID: "t1"},
	}
	for _, ev := range in {
		rec.Record(context.Background(), ev)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d", len(lines))
	}
	for i, line := range lines {
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if ev.Seq != int64(i+1) {
			t.Errorf("line %d: want Seq=%d, got %d", i, i+1, ev.Seq)
		}
		if ev.Type != in[i].Type {
			t.Errorf("line %d: want Type=%s, got %s", i, in[i].Type, ev.Type)
		}
	}
}

func TestMultiRecorder_Fanout(t *testing.T) {
	a := NewMemoryRecorder()
	b := NewMemoryRecorder()
	m := MultiRecorder{a, b, nil} // nil child must be tolerated
	for i := 0; i < 3; i++ {
		m.Record(context.Background(), Event{Type: EventTurnStart})
	}
	if len(a.Events()) != 3 || len(b.Events()) != 3 {
		t.Fatalf("each child should have 3 events; got %d / %d", len(a.Events()), len(b.Events()))
	}
}

func TestRecorderToSession_AppendsToSession(t *testing.T) {
	sess := &Session{ID: "s1"}
	rec := RecorderToSession(sess)
	rec.Record(context.Background(), Event{Type: EventTurnStart, TurnID: "t1"})
	rec.Record(context.Background(), Event{Type: EventTurnEnd, TurnID: "t1"})
	if len(sess.Events) != 2 {
		t.Fatalf("want 2 events on session, got %d", len(sess.Events))
	}
	if sess.Events[0].Seq != 1 || sess.Events[1].Seq != 2 {
		t.Errorf("Seq not assigned monotonically: %d, %d", sess.Events[0].Seq, sess.Events[1].Seq)
	}
}

func TestLoop_EmitsExpectedEvents(t *testing.T) {
	// Two-iteration loop: first response uses a tool, second ends the turn.
	prov := &testProvider{
		Responses: []ProviderResponse{
			{
				Content: []ContentBlock{
					{Type: TypeToolUse, ID: "u1", Name: "echo", Input: json.RawMessage(`{"x":1}`)},
				},
				StopReason: "tool_use",
				Usage:      Usage{InputTokens: 5, OutputTokens: 2},
			},
			{
				Content:    []ContentBlock{{Type: TypeText, Text: "done"}},
				StopReason: "end_turn",
				Usage:      Usage{InputTokens: 7, OutputTokens: 3},
			},
		},
	}
	rec := NewMemoryRecorder()
	c, err := New(Config{Provider: prov, Model: "test", Recorder: rec})
	if err != nil {
		t.Fatal(err)
	}
	tool := Tool{Name: "echo", Description: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}
	dispatch := ToolFunc(func(ctx context.Context, in json.RawMessage) (string, error) {
		return "ok", nil
	})
	ts := Toolset{Bindings: []ToolBinding{{Tool: tool, Func: dispatch}}}

	_, err = c.Loop(context.Background(), "", []Message{NewUserMessage("hi")}, ts, 5)
	if err != nil {
		t.Fatal(err)
	}

	evs := rec.Events()
	gotTypes := make([]EventType, len(evs))
	for i, ev := range evs {
		gotTypes[i] = ev.Type
	}
	want := []EventType{
		EventTurnStart,
		EventModelRequest,
		EventModelResponse,
		EventToolCallStart,
		EventToolCallEnd,
		EventModelRequest,
		EventModelResponse,
		EventTurnEnd,
	}
	if len(gotTypes) != len(want) {
		t.Fatalf("event count: want %d, got %d (%v)", len(want), len(gotTypes), gotTypes)
	}
	for i := range want {
		if gotTypes[i] != want[i] {
			t.Errorf("event %d: want %s, got %s", i, want[i], gotTypes[i])
		}
	}
	// All events share the same TurnID.
	tid := evs[0].TurnID
	if tid == "" {
		t.Error("TurnID should be non-empty")
	}
	for _, ev := range evs {
		if ev.TurnID != tid {
			t.Errorf("inconsistent TurnID: %q vs %q", ev.TurnID, tid)
		}
	}
	// Tool call events carry tool metadata.
	for _, ev := range evs {
		if ev.Type == EventToolCallStart {
			if ev.ToolName != "echo" || ev.ToolUseID != "u1" {
				t.Errorf("tool start missing metadata: %+v", ev)
			}
			if string(ev.ToolInput) != `{"x":1}` {
				t.Errorf("tool start ToolInput: got %s", ev.ToolInput)
			}
		}
		if ev.Type == EventToolCallEnd && ev.ToolOutput != "ok" {
			t.Errorf("tool end ToolOutput: got %q", ev.ToolOutput)
		}
	}
}

func TestLoop_ParallelToolEventsOrdered(t *testing.T) {
	// One iteration with two parallel tool_use blocks.
	prov := &testProvider{
		Responses: []ProviderResponse{
			{
				Content: []ContentBlock{
					{Type: TypeToolUse, ID: "u1", Name: "slow", Input: json.RawMessage(`{}`)},
					{Type: TypeToolUse, ID: "u2", Name: "slow", Input: json.RawMessage(`{}`)},
				},
				StopReason: "tool_use",
			},
			{
				Content:    []ContentBlock{{Type: TypeText, Text: "done"}},
				StopReason: "end_turn",
			},
		},
	}
	rec := NewMemoryRecorder()
	c, _ := New(Config{Provider: prov, Model: "test", Recorder: rec})
	tool := Tool{Name: "slow", Description: "", InputSchema: json.RawMessage(`{"type":"object"}`)}
	fn := ToolFunc(func(ctx context.Context, in json.RawMessage) (string, error) {
		time.Sleep(5 * time.Millisecond)
		return "x", nil
	})
	ts := Toolset{Bindings: []ToolBinding{{Tool: tool, Func: fn}}}

	_, err := c.Loop(context.Background(), "", nil, ts, 5)
	if err != nil {
		t.Fatal(err)
	}

	// Count tool start/end events; require 2 of each at the same Iter.
	var starts, ends int
	for _, ev := range rec.Events() {
		switch ev.Type {
		case EventToolCallStart:
			starts++
			if ev.Iter != 0 {
				t.Errorf("tool start at unexpected Iter %d", ev.Iter)
			}
		case EventToolCallEnd:
			ends++
		}
	}
	if starts != 2 || ends != 2 {
		t.Errorf("want 2 starts / 2 ends, got %d / %d", starts, ends)
	}
	// Seq strictly increasing.
	prev := int64(0)
	for _, ev := range rec.Events() {
		if ev.Seq <= prev {
			t.Errorf("Seq not monotonic: %d after %d", ev.Seq, prev)
		}
		prev = ev.Seq
	}
}

func TestLoop_EmitsTurnErrorOnProviderError(t *testing.T) {
	prov := &testProvider{Err: &APIError{StatusCode: 400, Message: "bad"}}
	rec := NewMemoryRecorder()
	c, _ := New(Config{Provider: prov, Model: "test", Recorder: rec})
	_, err := c.Loop(context.Background(), "", nil, Toolset{}, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	var sawError bool
	for _, ev := range rec.Events() {
		if ev.Type == EventTurnError {
			sawError = true
			if ev.Err == "" {
				t.Error("TurnError event missing Err")
			}
		}
	}
	if !sawError {
		t.Error("expected EventTurnError")
	}
}

func TestLoop_RetryEmitsRetryAttempt(t *testing.T) {
	// First call fails with 503 (retryable), second succeeds with end_turn.
	calls := 0
	prov := &fnProvider{call: func(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
		calls++
		if calls == 1 {
			return ProviderResponse{}, &APIError{StatusCode: 503, Message: "busy"}
		}
		return ProviderResponse{
			Content:    []ContentBlock{{Type: TypeText, Text: "ok"}},
			StopReason: "end_turn",
		}, nil
	}}
	rec := NewMemoryRecorder()
	c, _ := New(Config{
		Provider: prov, Model: "test", Recorder: rec,
		Retry: RetryConfig{MaxRetries: 2, InitialWait: 1 * time.Millisecond, MaxWait: 2 * time.Millisecond},
	})

	_, err := c.Loop(context.Background(), "", nil, Toolset{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	var retries int
	for _, ev := range rec.Events() {
		if ev.Type == EventRetryAttempt {
			retries++
			if ev.Attempt < 1 {
				t.Errorf("retry attempt should be 1-based, got %d", ev.Attempt)
			}
		}
	}
	if retries != 1 {
		t.Errorf("want 1 retry event, got %d", retries)
	}
}

// fnProvider is a Provider whose Call is supplied as a closure; Stream is
// unused by these tests.
type fnProvider struct {
	call func(ctx context.Context, req ProviderRequest) (ProviderResponse, error)
}

func (p *fnProvider) Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	return p.call(ctx, req)
}
func (p *fnProvider) Stream(ctx context.Context, req ProviderRequest, onDelta func(ContentBlock)) (ProviderResponse, error) {
	return p.call(ctx, req)
}

func TestSession_EventsRoundTripThroughFileStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{ID: "s1"}
	rec := RecorderToSession(sess)
	rec.Record(context.Background(), Event{
		Type: EventToolCallStart, TurnID: "t1", Iter: 0,
		ToolUseID: "u1", ToolName: "echo",
		ToolInput: json.RawMessage(`{"a":1}`),
	})
	rec.Record(context.Background(), Event{
		Type: EventToolCallEnd, TurnID: "t1", Iter: 0,
		ToolUseID: "u1", ToolName: "echo", ToolOutput: "ok",
	})
	if err := Save(context.Background(), store, sess); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 2 {
		t.Fatalf("want 2 events after roundtrip, got %d", len(got.Events))
	}
	if got.Events[0].ToolName != "echo" || got.Events[0].ToolUseID != "u1" {
		t.Errorf("event 0 lost fields: %+v", got.Events[0])
	}
	if string(got.Events[0].ToolInput) != `{"a":1}` {
		t.Errorf("event 0 ToolInput: got %s", got.Events[0].ToolInput)
	}
	if got.Events[1].ToolOutput != "ok" {
		t.Errorf("event 1 ToolOutput: got %q", got.Events[1].ToolOutput)
	}
}
