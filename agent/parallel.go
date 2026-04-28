package agent

import "context"

// StepFunc is a unit of work in a directed workflow.
// It accepts the shared context and returns a typed result.
type StepFunc[T any] func(ctx context.Context) (T, error)

// Result carries the outcome of one parallel step.
type Result[T any] struct {
	Value T
	Err   error
}

// Parallel runs all steps concurrently and waits for all to finish.
// The returned slice is index-aligned: results[i] corresponds to steps[i].
//
// No step is cancelled if another fails — cancellation policy is the caller's
// responsibility. Callers who want fail-fast behaviour should pass a derived
// context with a cancel func and call it after checking results for errors.
func Parallel[T any](ctx context.Context, steps ...StepFunc[T]) []Result[T] {
	type indexed struct {
		i   int
		val T
		err error
	}
	ch := make(chan indexed, len(steps))
	for i, step := range steps {
		i, step := i, step
		go func() {
			val, err := step(ctx)
			ch <- indexed{i: i, val: val, err: err}
		}()
	}
	results := make([]Result[T], len(steps))
	for range steps {
		r := <-ch
		results[r.i] = Result[T]{Value: r.val, Err: r.err}
	}
	return results
}
