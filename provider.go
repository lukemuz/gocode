package gocode

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
type ProviderRequest struct {
	Model     string
	MaxTokens int
	System    string
	Messages  []Message
	Tools     []Tool
}

// ProviderResponse is the normalised response every Provider must return.
type ProviderResponse struct {
	Content    []ContentBlock
	StopReason string
	Usage      Usage
}
