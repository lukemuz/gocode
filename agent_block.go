package luft

import (
	"context"
	"fmt"
)

// Hooks contains optional observer callbacks for an Agent step. All fields
// are nil by default and are safe to leave unset. A zero-value Hooks struct
// disables all observation.
//
// Call order within a Step or StepStream call:
//
//  1. Context.Trim is called once on the input history.
//  2. OnStep is called with the trimmed history (skipped if Trim fails).
//  3. The internal loop runs. Before every model call inside the loop,
//     Context.Trim is re-applied (when MaxTokens > 0) and then OnIteration
//     is invoked with the iteration index and the trimmed history.
//  4. OnStepDone is called with the result and any error from the loop.
//
// OnStepDone is NOT called when the initial Trim fails — only when the loop
// itself returns. Per-tool-call observation is available via Config.Recorder
// (see EventToolCallStart / EventToolCallEnd).
type Hooks struct {
	// OnStep is called once per Step / StepStream, after the initial context
	// trim and before the loop, with the trimmed history that will be sent
	// to the model on the first iteration. Useful for logging effective
	// context size or recording that a step started. Not called when Trim
	// returns an error.
	OnStep func(ctx context.Context, history []Message)

	// OnIteration is called before every model call inside the loop, with
	// the zero-based iteration index and the history that will actually be
	// sent (post-trim, when a ContextManager is configured). Useful for
	// observing long autonomous runs without dropping to Recorder.
	OnIteration func(ctx context.Context, iter int, history []Message)

	// OnStepDone is called after the loop returns, with the full LoopResult
	// and any error. Called even when the loop returns an error so callers
	// can record failures uniformly. Not called when the initial Trim fails.
	OnStepDone func(ctx context.Context, result LoopResult, err error)
}

// Agent is an assembled primitive: a Client, system prompt, Toolset,
// ContextManager, and optional Hooks wired together into a single Step call.
// It is the practical assembly point for a tool-using agent — for one-shot
// autonomous tasks pass a single user message containing the goal; for
// multi-turn conversations call Step once per human turn.
//
// Step trims history before the loop and again before every model call inside
// the loop (when ContextManager.MaxTokens > 0), so long autonomous runs do not
// blow the context window. Tool-use / tool-result integrity is preserved by
// ContextManager.Trim.
//
// A zero-value Agent is not valid — Client must be set. All other fields have
// safe zero values (no tools, no context trimming, no iteration limit, no hooks).
//
// Usage:
//
//	a := luft.Agent{
//	    Client:  client,
//	    System:  "You are a helpful assistant.",
//	    Tools:   myToolset,
//	    Context: luft.ContextManager{MaxTokens: 8000, KeepRecent: 20},
//	    MaxIter: 10,
//	}
//
//	// One-shot task: pass a single user message with the goal.
//	result, err := a.Step(ctx, []luft.Message{luft.NewUserMessage("do the thing")})
//	fmt.Println(result.FinalText())
//
//	// Multi-turn: call Step once per human turn, threading history.
//	history := []luft.Message{luft.NewUserMessage("first question")}
//	result, err = a.Step(ctx, history)
//	history = result.Messages
type Agent struct {
	// Client is the LLM client used for all model calls. Required.
	Client *Client

	// System is the system prompt passed to every model call.
	System string

	// Tools is the set of tools advertised to the model and dispatched when
	// called. A zero-value Toolset means no tools are offered.
	Tools Toolset

	// Context trims history before the first model call and again before each
	// subsequent model call inside the loop. A zero-value ContextManager
	// (MaxTokens == 0) disables trimming entirely.
	Context ContextManager

	// MaxIter caps the number of model calls per Step. Zero means no limit.
	MaxIter int

	// Hooks contains optional observer callbacks. A zero-value Hooks is safe.
	Hooks Hooks
}

// Step runs one user request through the agent loop. history is the
// conversation so far; it is not modified. The returned LoopResult contains
// the full updated conversation (trimmed history + new turns) and aggregate
// token usage for this step.
//
// Step trims history once up front (so OnStep sees the iter-0 history) and
// then again before every model call inside the loop when a ContextManager
// is configured. The primitives Loop and ContextManager.Trim remain available
// for callers who want a different policy.
func (a Agent) Step(ctx context.Context, history []Message) (LoopResult, error) {
	if a.Client == nil {
		return LoopResult{}, fmt.Errorf("luft: Agent.Client is required")
	}

	trimmed, err := a.Context.Trim(ctx, history)
	if err != nil {
		return LoopResult{}, err
	}

	if a.Hooks.OnStep != nil {
		a.Hooks.OnStep(ctx, trimmed)
	}

	result, err := a.Client.runLoop(
		ctx,
		a.System,
		trimmed,
		a.Tools,
		a.MaxIter,
		a.loopOpts(),
		nil,
		nil,
	)

	if a.Hooks.OnStepDone != nil {
		a.Hooks.OnStepDone(ctx, result, err)
	}

	return result, err
}

// StepStream is the streaming variant of Step. It delivers token deltas via
// onToken and tool results via onToolResult as they arrive. Both callbacks
// may be nil.
//
// Retry interaction: onToken may fire for partial content on a failed attempt
// before a successful retry. Wire a StreamBuffer via RetryConfig.OnRetry to
// reset partial output between attempts.
func (a Agent) StepStream(
	ctx context.Context,
	history []Message,
	onToken func(ContentBlock),
	onToolResult func([]ToolResult),
) (LoopResult, error) {
	if a.Client == nil {
		return LoopResult{}, fmt.Errorf("luft: Agent.Client is required")
	}
	if onToken == nil {
		onToken = func(ContentBlock) {}
	}
	if onToolResult == nil {
		onToolResult = func([]ToolResult) {}
	}

	trimmed, err := a.Context.Trim(ctx, history)
	if err != nil {
		return LoopResult{}, err
	}

	if a.Hooks.OnStep != nil {
		a.Hooks.OnStep(ctx, trimmed)
	}

	result, err := a.Client.runLoop(
		ctx,
		a.System,
		trimmed,
		a.Tools,
		a.MaxIter,
		a.loopOpts(),
		onToken,
		onToolResult,
	)

	if a.Hooks.OnStepDone != nil {
		a.Hooks.OnStepDone(ctx, result, err)
	}

	return result, err
}

// loopOpts builds the per-iteration hook bundle for the internal loop body.
// beforeIter is omitted when no ContextManager is configured so the no-op
// Trim path is not even entered each iteration.
func (a Agent) loopOpts() loopOpts {
	var beforeIter func(context.Context, []Message) ([]Message, error)
	if a.Context.MaxTokens > 0 {
		beforeIter = a.Context.Trim
	}
	return loopOpts{
		beforeIter: beforeIter,
		onIter:     a.Hooks.OnIteration,
	}
}
