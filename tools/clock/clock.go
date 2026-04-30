// Package clock provides a safe, read-only tool that returns the current UTC time.
// It is one of the initial core built-ins described in the roadmap: broadly
// useful, trivially safe, and a good demo/quickstart primitive.
package clock

import (
	"context"
	"time"

	"github.com/lukemuz/gocode"
)

// Clock is a safe read-only tool that returns the current UTC time in RFC3339.
//
// Usage:
//
//	c := clock.New()
//
//	// Direct field access (single-tool case):
//	tools := []gocode.Tool{c.Tool}
//	dispatch := map[string]gocode.ToolFunc{c.Tool.Name: c.Func}
//
//	// Or via Toolset for composition with Join:
//	toolset := c.Toolset()
type Clock struct {
	Tool gocode.Tool
	Func gocode.ToolFunc
	Meta gocode.ToolMetadata
}

// New creates a Clock tool ready for use.
func New() *Clock {
	tool := gocode.NewTool(
		"current_time",
		"Returns the current UTC time in RFC3339 format.",
		gocode.Object(),
	)
	c := &Clock{
		Tool: tool,
		Func: gocode.TypedToolFunc(func(_ context.Context, _ struct{}) (string, error) {
			return time.Now().UTC().Format(time.RFC3339), nil
		}),
		Meta: gocode.ToolMetadata{
			Source:   "tools/clock",
			ReadOnly: true,
		},
	}
	return c
}

// Toolset returns a single-binding Toolset for use with gocode.Join.
func (c *Clock) Toolset() gocode.Toolset {
	return gocode.Toolset{
		Bindings: []gocode.ToolBinding{
			{Tool: c.Tool, Func: c.Func, Meta: c.Meta},
		},
	}
}
