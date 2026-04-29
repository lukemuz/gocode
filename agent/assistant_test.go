package agent

import (
	"context"
	"errors"
	"testing"
)

func TestAssistantStep(t *testing.T) {
	ctx := context.Background()

	t.Run("nil Client returns error", func(t *testing.T) {
		a := Assistant{}
		_, err := a.Step(ctx, nil)
		if err == nil {
			t.Fatal("expected error for nil Client")
		}
	})

	t.Run("basic step returns assistant reply", func(t *testing.T) {
		p := newTestProvider()
		c, _ := New(Config{Provider: p, Model: "test"})
		a := Assistant{Client: c, System: "be helpful"}

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
		a := Assistant{
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
		a := Assistant{
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
		tool, fn, _ := NewTypedTool[struct{}](
			"ping", "ping tool",
			Object(),
			func(_ context.Context, _ struct{}) (string, error) {
				called = true
				return "pong", nil
			},
		)
		ts := Toolset{Bindings: []ToolBinding{{Tool: tool, Func: fn}}}
		a := Assistant{Client: c, Tools: ts}

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
		a := Assistant{
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
		a := Assistant{
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
		a := Assistant{
			Client: c,
			Hooks:  Hooks{OnStepDone: func(_ context.Context, _ LoopResult, err error) { doneErr = err }},
		}
		_, _ = a.Step(ctx, []Message{NewUserMessage("hi")})
		if doneErr == nil {
			t.Error("OnStepDone should receive the loop error")
		}
	})
}

func TestAssistantStepStream(t *testing.T) {
	ctx := context.Background()

	t.Run("nil Client returns error", func(t *testing.T) {
		a := Assistant{}
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
		a := Assistant{Client: c}

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
		a := Assistant{Client: c}

		// Both nil — should not panic.
		_, err := a.StepStream(ctx, []Message{NewUserMessage("hi")}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("hooks fire on StepStream", func(t *testing.T) {
		p := newTestProvider()
		c, _ := New(Config{Provider: p, Model: "test"})

		var stepCalled, doneCalled bool
		a := Assistant{
			Client: c,
			Hooks: Hooks{
				OnStep:     func(_ context.Context, _ []Message) { stepCalled = true },
				OnStepDone: func(_ context.Context, _ LoopResult, _ error) { doneCalled = true },
			},
		}
		_, err := a.StepStream(ctx, []Message{NewUserMessage("hi")}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !stepCalled || !doneCalled {
			t.Errorf("hooks not called: OnStep=%v OnStepDone=%v", stepCalled, doneCalled)
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
