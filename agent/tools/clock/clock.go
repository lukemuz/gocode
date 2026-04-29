// Package clock provides a safe, read-only tool that returns the current UTC time.
// It is one of the initial core built-ins described in the roadmap: broadly
// useful, trivially safe, and a good demo/quickstart primitive.
package clock

import (
	"context"
	"time"

	"github.com/lukemuz/gocode/agent"
)

// Clock is a safe read-only tool that returns the current UTC time in RFC3339.
//
// Usage:
//
//	c := clock.New()
//
//	// Direct field access (single-tool case):
//	tools := []agent.Tool{c.Tool}
//	dispatch := map[string]agent.ToolFunc{c.Tool.Name: c.Func}
//
//	// Or via Toolset for composition with Join:
//	toolset := c.Toolset()
type Clock struct {
	Tool agent.Tool
	Func agent.ToolFunc
	Meta agent.ToolMetadata
}

// New creates a Clock tool ready for use.
func New() *Clock {
	tool := agent.NewTool(
		"current_time",
		"Returns the current UTC time in RFC3339 format.",
		agent.Object(),
	)
	c := &Clock{
		Tool: tool,
		Func: agent.TypedToolFunc(func(_ context.Context, _ struct{}) (string, error) {
			return time.Now().UTC().Format(time.RFC3339), nil
		}),
		Meta: agent.ToolMetadata{
			Source:   "tools/clock",
			ReadOnly: true,
		},
	}
	return c
}

// Toolset returns a single-binding Toolset for use with agent.Join.
func (c *Clock) Toolset() agent.Toolset {
	return agent.Toolset{
		Bindings: []agent.ToolBinding{
			{Tool: c.Tool, Func: c.Func, Meta: c.Meta},
		},
	}
}
