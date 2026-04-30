package gocode

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel errors for use with errors.Is.
var (
	// ErrMaxIter is wrapped in a LoopError when Loop exhausts its iteration budget.
	ErrMaxIter = errors.New("gocode: loop exceeded maxIter")

	// ErrMissingTool is wrapped in a ToolError when the model calls a tool
	// that is not present in the dispatch map.
	ErrMissingTool = errors.New("gocode: model called unknown tool")

	// ErrRetryExhausted is wrapped in a RetryExhaustedError when all retry
	// attempts have been consumed without a successful response.
	ErrRetryExhausted = errors.New("gocode: retry exhausted")
)

// APIError is returned when the LLM API responds with a non-2xx status.
type APIError struct {
	StatusCode int
	Type       string
	Message    string
	// RetryAfter, when non-zero, carries the duration requested by the API via
	// a Retry-After header (e.g. on a 429 Too Many Requests response).
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	s := fmt.Sprintf("gocode: API %d (%s): %s", e.StatusCode, e.Type, e.Message)
	if e.RetryAfter != 0 {
		s += fmt.Sprintf(" (retry after %s)", e.RetryAfter)
	}
	return s
}

// ToolError is returned when a dispatch lookup fails (tool missing from map).
// Individual tool execution errors are soft-faulted into is_error results
// and are never wrapped in ToolError.
type ToolError struct {
	ToolName  string
	ToolUseID string
	Cause     error
}

func (e *ToolError) Error() string {
	return fmt.Sprintf("gocode: tool %q (%s): %s", e.ToolName, e.ToolUseID, e.Cause)
}

func (e *ToolError) Unwrap() error { return e.Cause }

// LoopError is returned when Loop exits without reaching end_turn.
// Iter is the loop iteration count at the point of failure.
type LoopError struct {
	Iter  int
	Cause error
}

func (e *LoopError) Error() string {
	return fmt.Sprintf("gocode: loop aborted at iteration %d: %s", e.Iter, e.Cause)
}

func (e *LoopError) Unwrap() error { return e.Cause }

// RetryExhaustedError is returned by callWithRetry when every attempt has
// failed and no more retries remain. Attempts is the total number of calls
// that were made (including the initial attempt). Cause is the last error
// returned by the underlying function.
type RetryExhaustedError struct {
	Attempts int
	Cause    error
}

func (e *RetryExhaustedError) Error() string {
	return fmt.Sprintf("gocode: retry exhausted after %d attempt(s): %s", e.Attempts, e.Cause)
}

// Unwrap returns the last error that caused retries to be exhausted, enabling
// errors.Is / errors.As to inspect the underlying failure. It also chains
// ErrRetryExhausted so callers can match on errors.Is(err, ErrRetryExhausted).
func (e *RetryExhaustedError) Unwrap() []error {
	return []error{e.Cause, ErrRetryExhausted}
}
