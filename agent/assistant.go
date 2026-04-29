package agent

import (
	"context"
	"fmt"
)

// Hooks contains optional observer callbacks for an Assistant step. All fields
// are nil by default and are safe to leave unset. A zero-value Hooks struct
// disables all observation.
//
// Call order within a Step or StepStream call:
//
//  1. Context.Trim is called.
//  2. OnStep is called with the trimmed history (skipped if Trim fails).
//  3. Client.Loop or Client.LoopStream runs.
//  4. OnStepDone is called with the result and any error from the loop.
//
// OnStepDone is NOT called when Trim fails — only when the loop itself returns.
// Per-iteration and per-tool-call hooks are planned for a future release and
// will be added to this struct without breaking existing code.
type Hooks struct {
	// OnStep is called after context trimming and before the loop, with the
	// trimmed history that will actually be sent to the model. Useful for
	// logging effective context size or recording that a step started.
	// Not called when Trim returns an error.
	OnStep func(ctx context.Context, history []Message)

	// OnStepDone is called after the loop returns, with the full LoopResult
	// and any error. Called even when the loop returns an error so callers
	// can record failures uniformly. Not called when Trim itself fails.
	OnStepDone func(ctx context.Context, result LoopResult, err error)
}

// Assistant is an assembled agent primitive: a Client, system prompt, Toolset,
// ContextManager, and optional Hooks wired together into a single Step call.
//
// It is equivalent to calling ContextManager.Trim followed by Client.Loop. The
// desugared form is always available; users can drop to those primitives at any
// time without changing their data model.
//
// A zero-value Assistant is not valid — Client must be set. All other fields
// have safe zero values (no tools, no context trimming, no iteration limit, no
// hooks).
//
// Usage:
//
//	a := agent.Assistant{
//	    Client:  client,
//	    System:  "You are a helpful assistant.",
//	    Tools:   myToolset,
//	    Context: agent.ContextManager{MaxTokens: 8000, KeepRecent: 20},
//	    MaxIter: 10,
//	}
//
//	// Each call is one user request → model response (possibly with tool cycles).
//	result, err := a.Step(ctx, history)
//	history = result.Messages
type Assistant struct {
	// Client is the LLM client used for all model calls. Required.
	Client *Client

	// System is the system prompt passed to every Loop call.
	System string

	// Tools is the set of tools advertised to the model and dispatched when
	// called. A zero-value Toolset means no tools are offered.
	Tools Toolset

	// Context trims history before each Loop call. A zero-value
	// ContextManager (MaxTokens == 0) disables trimming.
	Context ContextManager

	// MaxIter caps the number of model calls per Step. Zero means no limit.
	MaxIter int

	// Hooks contains optional observer callbacks. A zero-value Hooks is safe.
	Hooks Hooks
}

// Step runs one user request through context trimming and then the agent loop.
// history is the conversation so far; it is not modified. The returned
// LoopResult contains the full updated conversation (trimmed history + new
// turns) and aggregate token usage for this step.
//
// Step is equivalent to (omitting hooks for brevity):
//
//	trimmed, err := a.Context.Trim(ctx, history)
//	if err != nil {
//	    return LoopResult{}, err
//	}
//	return a.Client.Loop(ctx, a.System, trimmed, a.Tools, a.MaxIter)
//
// Drop to these primitives at any time without changing your data model.
func (a Assistant) Step(ctx context.Context, history []Message) (LoopResult, error) {
	if a.Client == nil {
		return LoopResult{}, fmt.Errorf("agent: Assistant.Client is required")
	}

	trimmed, err := a.Context.Trim(ctx, history)
	if err != nil {
		return LoopResult{}, err
	}

	if a.Hooks.OnStep != nil {
		a.Hooks.OnStep(ctx, trimmed)
	}

	result, err := a.Client.Loop(
		ctx,
		a.System,
		trimmed,
		a.Tools,
		a.MaxIter,
	)

	if a.Hooks.OnStepDone != nil {
		a.Hooks.OnStepDone(ctx, result, err)
	}

	return result, err
}

// StepStream is the streaming variant of Step. It runs context trimming and
// then Client.LoopStream, delivering token deltas via onToken and tool results
// via onToolResult as they arrive. Both callbacks may be nil.
//
// Retry interaction: onToken may fire for partial content on a failed attempt
// before a successful retry. Wire a StreamBuffer via RetryConfig.OnRetry to
// reset partial output between attempts.
//
// StepStream is equivalent to (omitting hooks and nil-callback guards):
//
//	trimmed, err := a.Context.Trim(ctx, history)
//	if err != nil {
//	    return LoopResult{}, err
//	}
//	return a.Client.LoopStream(ctx, a.System, trimmed,
//	    a.Tools, a.MaxIter, onToken, onToolResult)
//
// Drop to these primitives at any time without changing your data model.
func (a Assistant) StepStream(
	ctx context.Context,
	history []Message,
	onToken func(ContentBlock),
	onToolResult func([]ToolResult),
) (LoopResult, error) {
	if a.Client == nil {
		return LoopResult{}, fmt.Errorf("agent: Assistant.Client is required")
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

	result, err := a.Client.LoopStream(
		ctx,
		a.System,
		trimmed,
		a.Tools,
		a.MaxIter,
		onToken,
		onToolResult,
	)

	if a.Hooks.OnStepDone != nil {
		a.Hooks.OnStepDone(ctx, result, err)
	}

	return result, err
}
