package agent

import (
	"errors"
	"fmt"
)

// Sentinel errors for use with errors.Is.
var (
	// ErrMaxIter is wrapped in a LoopError when Loop exhausts its iteration budget.
	ErrMaxIter = errors.New("agent: loop exceeded maxIter")

	// ErrMissingTool is wrapped in a ToolError when the model calls a tool
	// that is not present in the dispatch map.
	ErrMissingTool = errors.New("agent: model called unknown tool")
)

// APIError is returned when the Anthropic API responds with a non-2xx status.
type APIError struct {
	StatusCode int
	Type       string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("agent: API %d (%s): %s", e.StatusCode, e.Type, e.Message)
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
	return fmt.Sprintf("agent: tool %q (%s): %s", e.ToolName, e.ToolUseID, e.Cause)
}

func (e *ToolError) Unwrap() error { return e.Cause }

// LoopError is returned when Loop exits without reaching end_turn.
// Iter is the loop iteration count at the point of failure.
type LoopError struct {
	Iter  int
	Cause error
}

func (e *LoopError) Error() string {
	return fmt.Sprintf("agent: loop aborted at iteration %d: %s", e.Iter, e.Cause)
}

func (e *LoopError) Unwrap() error { return e.Cause }
