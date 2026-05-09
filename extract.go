package luft

import (
	"context"
	"fmt"
)

const (
	defaultExtractName    = "submit"
	defaultExtractMaxIter = 8
)

// ExtractParams configures Extract. Description and Schema are required;
// the rest have sensible defaults.
//
// The struct is generic over T so Validate can take a typed argument without
// the caller writing assertions. Most call sites pass a single literal:
//
//	luft.Extract(ctx, client, system, history, luft.ExtractParams[MyPlan]{
//	    Description: "Submit the final plan",
//	    Schema:      planSchema,
//	})
type ExtractParams[T any] struct {
	// Description is the submit tool's description shown to the model. It is
	// the model-facing instruction for what value to produce. Required.
	Description string

	// Schema describes the shape of T. Required. Construct with the schema
	// helpers (Object, Array, ObjectOf, ...) or hand-roll JSON via
	// InputSchema literals.
	Schema InputSchema

	// Tools are additional tools the model may call before submitting (e.g.
	// search, retrieval). Pass a zero-value Toolset for pure structured output.
	Tools Toolset

	// Name is the submit tool's name. Defaults to "submit".
	Name string

	// MaxIter caps the number of model turns. Defaults to 8.
	MaxIter int

	// Validate, if non-nil, is called with the model's submitted value before
	// Extract accepts it. Returning a non-nil error marks the submit as failed:
	// the model sees an is_error=true tool result with the error message and
	// may retry within the iteration budget. Use this to enforce constraints
	// the schema cannot express (length caps, cross-field invariants, etc.).
	Validate func(T) error
}

// Extract runs a tool-use loop in which the model is required to call a single
// terminal "submit" tool whose typed argument becomes the return value.
//
// This collapses the "submit_X" structured-output pattern into one call. Both
// pure structured output (no other tools) and structured output via tool use
// (e.g. search-then-submit) are expressed by setting Params.Tools.
//
// On success, the returned T is the accepted value and LoopResult contains the
// full conversation (including the terminal tool call) and aggregate usage.
//
// Errors:
//   - The underlying loop fails (network, max_tokens, max_iter, etc.).
//   - The model ends its turn without ever calling the submit tool — Extract
//     returns the zero value of T and an error mentioning the tool name.
//
// Validation failures are NOT errors — they are surfaced to the model as
// retriable tool errors. Only when the loop ultimately ends without an
// accepted submission does Extract return an error.
func Extract[T any](
	ctx context.Context,
	client *Client,
	system string,
	history []Message,
	params ExtractParams[T],
) (T, LoopResult, error) {
	var captured T
	if params.Description == "" {
		return captured, LoopResult{}, fmt.Errorf("luft: Extract: ExtractParams.Description is required")
	}

	name := params.Name
	if name == "" {
		name = defaultExtractName
	}
	maxIter := params.MaxIter
	if maxIter == 0 {
		maxIter = defaultExtractMaxIter
	}

	submitted := false
	submitFn := TypedToolFunc(func(ctx context.Context, in T) (string, error) {
		if params.Validate != nil {
			if err := params.Validate(in); err != nil {
				// Surface as a retriable tool error so the model can correct.
				return "", err
			}
		}
		captured = in
		submitted = true
		return "submitted", nil
	})

	submitTool := NewTool(name, params.Description, params.Schema)
	submitBinding := ToolBinding{
		Tool: submitTool,
		Func: submitFn,
		Meta: ToolMetadata{Terminal: true, Source: "agent/extract"},
	}

	allTools, err := Join(params.Tools, Tools(submitBinding))
	if err != nil {
		return captured, LoopResult{}, fmt.Errorf("luft: Extract: combine tools: %w", err)
	}

	result, err := client.Loop(ctx, system, history, allTools, maxIter)
	if err != nil {
		return captured, result, err
	}
	if !submitted {
		return captured, result, fmt.Errorf("luft: Extract: model ended without calling %q", name)
	}
	return captured, result, nil
}
