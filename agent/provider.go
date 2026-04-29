package agent

import "context"

// Provider is the abstraction over an LLM API backend.
// Implementations translate between the canonical ProviderRequest /
// ProviderResponse types and their own wire formats.
type Provider interface {
	// Call sends a single request and returns a normalised response.
	Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error)

	// Stream sends the request and invokes onDelta for every incremental
	// ContentBlock (typically text deltas; also partial tool_use blocks
	// when supported by the backend). It returns the final aggregated
	// ProviderResponse (complete Content, StopReason, and Usage) once
	// the stream ends. The callback is invoked synchronously as data
	// arrives. Retries (if configured) may cause multiple callback
	// invocations across attempts.
	Stream(ctx context.Context, req ProviderRequest, onDelta func(ContentBlock)) (ProviderResponse, error)
}

// ProviderRequest is the canonical request passed to every Provider.
//
// Tools are local (category-2 included) tool declarations the model may call;
// the provider translates them to its native wire format and the agent loop
// dispatches via ToolFunc when the model emits a tool_use block.
//
// ProviderTools are server-executed (category-1) tools — the provider runs
// them and returns inline result blocks. The loop never inspects them; they
// are passed straight through and spliced verbatim into the provider's tools
// array. Each entry is tagged with a Provider string so providers can reject
// mismatched entries at request build time.
type ProviderRequest struct {
	Model         string
	MaxTokens     int
	System        string
	Messages      []Message
	Tools         []Tool
	ProviderTools []ProviderTool
}

// ProviderResponse is the normalised response every Provider must return.
type ProviderResponse struct {
	Content    []ContentBlock
	StopReason string
	Usage      Usage
}
