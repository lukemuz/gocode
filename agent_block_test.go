package luft

import (
	"context"
	"errors"
	"testing"
)

func TestAgentStep(t *testing.T) {
	ctx := context.Background()

	t.Run("nil Client returns error", func(t *testing.T) {
		a := Agent{}
		_, err := a.Step(ctx, nil)
		if err == nil {
			t.Fatal("expected error for nil Client")
		}
	})

	t.Run("basic step returns assistant reply", func(t *testing.T) {
		p := newTestProvider()
		c, _ := New(Config{Provider: p, Model: "test"})
		a := Agent{Client: c, System: "be helpful"}

		history := []Message{NewUserMessage("hello")}
		result, err := a.Step(ctx, history)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(result.Messages))
		}
		if result.Messages[1].Role != RoleAssistant {
			t.Errorf("expected assistant reply, got %s", result.Messages[1].Role)
		}
		if p.CallCount != 1 {
			t.Errorf("expected 1 Call, got %d", p.CallCount)
		}
	})

	t.Run("context manager trims before loop", func(t *testing.T) {
		// Build a history of 6 messages; budget 4 keeps only the last 4.
		var received []Message
		inner := &testProvider{Resp: ProviderResponse{
			Content:    []ContentBlock{{Type: TypeText, Text: "ok"}},
			StopReason: "end_turn",
			Usage:      Usage{InputTokens: 1},
		}}
		spy := &spyProvider{inner: inner, onCall: func(req ProviderRequest) {
			received = req.Messages
		}}
		c2, _ := New(Config{Provider: spy, Model: "test"})

		h := []Message{
			NewUserMessage("msg0"), NewUserMessage("msg1"),
			NewUserMessage("msg2"), NewUserMessage("msg3"),
			NewUserMessage("msg4"), NewUserMessage("msg5"),
		}
		a := Agent{
			Client:  c2,
			Context: ContextManager{MaxTokens: 4, KeepRecent: 4, TokenCounter: countMessages},
		}
		_, err := a.Step(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		// Trim should have dropped the first 2, leaving 4.
		if len(received) != 4 {
			t.Errorf("loop received %d messages, want 4 (trimmed)", len(received))
		}
		if TextContent(received[0]) != "msg2" {
			t.Errorf("first received message = %q, want msg2", TextContent(received[0]))
		}
	})

	t.Run("trim error is returned before loop", func(t *testing.T) {
		p := newTestProvider()
		c, _ := New(Config{Provider: p, Model: "test"})
		boom := errors.New("counter exploded")
		a := Agent{
			Client: c,
			Context: ContextManager{
				MaxTokens:    1,
				TokenCounter: func(_ []Message) (int, error) { return 0, boom },
			},
		}
		_, err := a.Step(ctx, []Message{NewUserMessage("hi")})
		if !errors.Is(err, boom) {
			t.Errorf("expected trim error, got %v", err)
		}
		if p.CallCount != 0 {
			t.Error("Loop should not be called when Trim fails")
		}
	})

	t.Run("tools from Toolset are passed to loop", func(t *testing.T) {
		toolUse := ProviderResponse{
			Content:    []ContentBlock{{Type: TypeToolUse, ID: "tu1", Name: "ping"}},
			StopReason: "tool_use",
			Usage:      Usage{InputTokens: 5},
		}
		end := ProviderResponse{
			Content:    []ContentBlock{{Type: TypeText, Text: "pong"}},
			StopReason: "end_turn",
			Usage:      Usage{InputTokens: 5},
		}
		p := &testProvider{Resp: end, Responses: []ProviderResponse{toolUse}}
		c, _ := New(Config{Provider: p, Model: "test"})

		var called bool
		tool, fn := NewTypedTool[struct{}](
			"ping", "ping tool",
			Object(),
			func(_ context.Context, _ struct{}) (string, error) {
				called = true
				return "pong", nil
			},
		)
		ts := Toolset{Bindings: []ToolBinding{{Tool: tool, Func: fn}}}
		a := Agent{Client: c, Tools: ts}

		_, err := a.Step(ctx, []Message{NewUserMessage("go")})
		if err != nil {
			t.Fatal(err)
		}
		if !called {
			t.Error("tool was not called through Toolset dispatch")
		}
	})

	t.Run("OnStep hook receives trimmed history", func(t *testing.T) {
		p := newTestProvider()
		c, _ := New(Config{Provider: p, Model: "test"})

		h := []Message{
			NewUserMessage("old0"), NewUserMessage("old1"),
			NewUserMessage("keep0"), NewUserMessage("keep1"),
		}
		var hookHistory []Message
		a := Agent{
			Client:  c,
			Context: ContextManager{MaxTokens: 2, KeepRecent: 2, TokenCounter: countMessages},
			Hooks: Hooks{
				OnStep: func(_ context.Context, history []Message) {
					hookHistory = history
				},
			},
		}
		_, err := a.Step(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if len(hookHistory) != 2 {
			t.Errorf("OnStep received %d messages, want 2 (trimmed)", len(hookHistory))
		}
	})

	t.Run("OnStepDone hook receives result and nil error on success", func(t *testing.T) {
		p := newTestProvider()
		c, _ := New(Config{Provider: p, Model: "test"})

		var doneResult LoopResult
		var doneErr error
		var doneCalled bool
		a := Agent{
			Client: c,
			Hooks: Hooks{
				OnStepDone: func(_ context.Context, r LoopResult, err error) {
					doneResult = r
					doneErr = err
					doneCalled = true
				},
			},
		}
		result, err := a.Step(ctx, []Message{NewUserMessage("hi")})
		if err != nil {
			t.Fatal(err)
		}
		if !doneCalled {
			t.Fatal("OnStepDone was not called")
		}
		if doneErr != nil {
			t.Errorf("OnStepDone received error %v, want nil", doneErr)
		}
		if len(doneResult.Messages) != len(result.Messages) {
			t.Errorf("OnStepDone result has %d messages, want %d", len(doneResult.Messages), len(result.Messages))
		}
	})

	t.Run("OnStepDone called with error on loop failure", func(t *testing.T) {
		boom := errors.New("provider down")
		p := &testProvider{Err: boom}
		c, _ := New(Config{Provider: p, Model: "test", Retry: RetryConfig{Disabled: true}})

		var doneErr error
		a := Agent{
			Client: c,
			Hooks:  Hooks{OnStepDone: func(_ context.Context, _ LoopResult, err error) { doneErr = err }},
		}
		_, _ = a.Step(ctx, []Message{NewUserMessage("hi")})
		if doneErr == nil {
			t.Error("OnStepDone should receive the loop error")
		}
	})

	t.Run("OnIteration fires once per model call with effective history", func(t *testing.T) {
		toolUse := ProviderResponse{
			Content:    []ContentBlock{{Type: TypeToolUse, ID: "tu1", Name: "ping"}},
			StopReason: "tool_use",
			Usage:      Usage{InputTokens: 1},
		}
		end := ProviderResponse{
			Content:    []ContentBlock{{Type: TypeText, Text: "done"}},
			StopReason: "end_turn",
			Usage:      Usage{InputTokens: 1},
		}
		p := &testProvider{Resp: end, Responses: []ProviderResponse{toolUse}}
		c, _ := New(Config{Provider: p, Model: "test"})

		tool, fn := NewTypedTool[struct{}](
			"ping", "ping tool",
			Object(),
			func(_ context.Context, _ struct{}) (string, error) { return "pong", nil },
		)
		ts := Toolset{Bindings: []ToolBinding{{Tool: tool, Func: fn}}}

		var seen [][]Message
		var iters []int
		a := Agent{
			Client: c,
			Tools:  ts,
			Hooks: Hooks{
				OnIteration: func(_ context.Context, iter int, h []Message) {
					iters = append(iters, iter)
					cp := make([]Message, len(h))
					copy(cp, h)
					seen = append(seen, cp)
				},
			},
		}
		_, err := a.Step(ctx, []Message{NewUserMessage("go")})
		if err != nil {
			t.Fatal(err)
		}
		if len(iters) != 2 || iters[0] != 0 || iters[1] != 1 {
			t.Errorf("iters = %v, want [0 1]", iters)
		}
		if len(seen[0]) != 1 {
			t.Errorf("iter 0 history len = %d, want 1", len(seen[0]))
		}
		// iter 1 sees: original user + assistant tool_use + user tool_result = 3.
		if len(seen[1]) != 3 {
			t.Errorf("iter 1 history len = %d, want 3", len(seen[1]))
		}
	})

	t.Run("Trim is invoked before each model call", func(t *testing.T) {
		// Whether or not Trim can find a clean cut in any given iteration,
		// the new contract is that ContextManager.Trim runs *before every*
		// model call when MaxTokens > 0. Counting TokenCounter invocations
		// equals counting Trim invocations (Trim calls the counter once per
		// call after the empty / zero-budget early returns).
		toolUse := ProviderResponse{
			Content:    []ContentBlock{{Type: TypeToolUse, ID: "a", Name: "ping"}},
			StopReason: "tool_use",
			Usage:      Usage{InputTokens: 1},
		}
		end := ProviderResponse{
			Content:    []ContentBlock{{Type: TypeText, Text: "done"}},
			StopReason: "end_turn",
			Usage:      Usage{InputTokens: 1},
		}
		p := &testProvider{Resp: end, Responses: []ProviderResponse{toolUse}}
		c, _ := New(Config{Provider: p, Model: "test"})

		tool, fn := NewTypedTool[struct{}](
			"ping", "ping tool",
			Object(),
			func(_ context.Context, _ struct{}) (string, error) { return "pong", nil },
		)
		ts := Toolset{Bindings: []ToolBinding{{Tool: tool, Func: fn}}}

		var trimCalls int
		a := Agent{
			Client: c,
			Tools:  ts,
			Context: ContextManager{
				MaxTokens: 100,
				TokenCounter: func(msgs []Message) (int, error) {
					trimCalls++
					return len(msgs), nil
				},
			},
		}
		_, err := a.Step(ctx, []Message{NewUserMessage("go")})
		if err != nil {
			t.Fatal(err)
		}
		// 1 upfront + 2 in-loop (iter 0 + iter 1) = 3.
		if trimCalls != 3 {
			t.Errorf("Trim invoked %d times, want 3 (1 upfront + 2 in-loop)", trimCalls)
		}
	})

	t.Run("mid-loop trim shrinks history when clean cuts exist", func(t *testing.T) {
		// Multi-turn starting history with multiple plain-user messages
		// gives Trim cut points it can use during the loop. Budget=5 with
		// countMessages: starts under budget; mid-loop the tool cycle pushes
		// it over and the older turns get dropped.
		toolUse := ProviderResponse{
			Content:    []ContentBlock{{Type: TypeToolUse, ID: "a", Name: "ping"}},
			StopReason: "tool_use",
			Usage:      Usage{InputTokens: 1},
		}
		end := ProviderResponse{
			Content:    []ContentBlock{{Type: TypeText, Text: "done"}},
			StopReason: "end_turn",
			Usage:      Usage{InputTokens: 1},
		}
		var sentLengths []int
		inner := &testProvider{Resp: end, Responses: []ProviderResponse{toolUse}}
		spy := &spyProvider{inner: inner, onCall: func(req ProviderRequest) {
			sentLengths = append(sentLengths, len(req.Messages))
		}}
		c, _ := New(Config{Provider: spy, Model: "test"})

		tool, fn := NewTypedTool[struct{}](
			"ping", "ping tool",
			Object(),
			func(_ context.Context, _ struct{}) (string, error) { return "pong", nil },
		)
		ts := Toolset{Bindings: []ToolBinding{{Tool: tool, Func: fn}}}

		// 5 starting messages (under budget=5). After iter 0 the loop adds
		// assistant tool_use + user tool_result, taking total to 7 — over
		// budget. KeepRecent=2 + tool-cycle integrity expands the tail to
		// the most recent plain-user message; the older turns drop out.
		h := []Message{
			NewUserMessage("u0"),
			{Role: RoleAssistant, Content: []ContentBlock{{Type: TypeText, Text: "a0"}}},
			NewUserMessage("u1"),
			{Role: RoleAssistant, Content: []ContentBlock{{Type: TypeText, Text: "a1"}}},
			NewUserMessage("current task"),
		}
		a := Agent{
			Client:  c,
			Tools:   ts,
			Context: ContextManager{MaxTokens: 5, KeepRecent: 2, TokenCounter: countMessages},
		}
		_, err := a.Step(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if len(sentLengths) != 2 {
			t.Fatalf("expected 2 model calls, got %d", len(sentLengths))
		}
		if sentLengths[0] != 5 {
			t.Errorf("iter 0 sent %d messages, want 5 (no trim, fits budget)", sentLengths[0])
		}
		// iter 1: untrimmed would be 7. After mid-loop trim the older turns
		// drop and only the current task + tool cycle remain (3 messages).
		if sentLengths[1] >= 7 {
			t.Errorf("iter 1 sent %d messages; mid-loop trim did not shrink", sentLengths[1])
		}
	})
}

func TestAgentStepStream(t *testing.T) {
	ctx := context.Background()

	t.Run("nil Client returns error", func(t *testing.T) {
		a := Agent{}
		_, err := a.StepStream(ctx, nil, nil, nil)
		if err == nil {
			t.Fatal("expected error for nil Client")
		}
	})

	t.Run("delivers token deltas via callback", func(t *testing.T) {
		p := newTestProvider()
		p.Deltas = []ContentBlock{
			{Type: TypeText, Text: "hello"},
			{Type: TypeText, Text: " world"},
		}
		c, _ := New(Config{Provider: p, Model: "test"})
		a := Agent{Client: c}

		var tokens []string
		_, err := a.StepStream(ctx, []Message{NewUserMessage("hi")},
			func(b ContentBlock) {
				if b.Type == TypeText {
					tokens = append(tokens, b.Text)
				}
			},
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(tokens) != 2 || tokens[0] != "hello" || tokens[1] != " world" {
			t.Errorf("unexpected tokens: %v", tokens)
		}
	})

	t.Run("nil callbacks do not panic", func(t *testing.T) {
		p := newTestProvider()
		p.Deltas = []ContentBlock{{Type: TypeText, Text: "hi"}}
		c, _ := New(Config{Provider: p, Model: "test"})
		a := Agent{Client: c}

		_, err := a.StepStream(ctx, []Message{NewUserMessage("hi")}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("hooks fire on StepStream", func(t *testing.T) {
		p := newTestProvider()
		c, _ := New(Config{Provider: p, Model: "test"})

		var stepCalled, doneCalled, iterCalled bool
		a := Agent{
			Client: c,
			Hooks: Hooks{
				OnStep:      func(_ context.Context, _ []Message) { stepCalled = true },
				OnIteration: func(_ context.Context, _ int, _ []Message) { iterCalled = true },
				OnStepDone:  func(_ context.Context, _ LoopResult, _ error) { doneCalled = true },
			},
		}
		_, err := a.StepStream(ctx, []Message{NewUserMessage("hi")}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !stepCalled || !iterCalled || !doneCalled {
			t.Errorf("hooks not called: OnStep=%v OnIteration=%v OnStepDone=%v",
				stepCalled, iterCalled, doneCalled)
		}
	})
}

// spyProvider wraps a testProvider and calls onCall before each Call.
type spyProvider struct {
	inner  *testProvider
	onCall func(ProviderRequest)
}

func (s *spyProvider) Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	if s.onCall != nil {
		s.onCall(req)
	}
	return s.inner.Call(ctx, req)
}

func (s *spyProvider) Stream(ctx context.Context, req ProviderRequest, onDelta func(ContentBlock)) (ProviderResponse, error) {
	return s.inner.Stream(ctx, req, onDelta)
}
