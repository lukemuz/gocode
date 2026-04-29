// Package math provides a safe calculator tool for basic arithmetic.
// It is one of the initial core built-ins: useful in quickstarts and tests,
// trivially safe, and a clear example of the TypedToolFunc pattern.
package math

import (
	"context"
	"fmt"
	"math"

	"github.com/lukemuz/gocode/agent"
)

// Calculator is a safe read-only tool for basic arithmetic.
//
// Usage:
//
//	calc := math.New()
//
//	// Direct field access:
//	tools := []agent.Tool{calc.Tool}
//	dispatch := map[string]agent.ToolFunc{calc.Tool.Name: calc.Func}
//
//	// Or via Toolset:
//	toolset := calc.Toolset()
type Calculator struct {
	Tool agent.Tool
	Func agent.ToolFunc
	Meta agent.ToolMetadata
}

type input struct {
	Operation string  `json:"operation"`
	A         float64 `json:"a"`
	B         float64 `json:"b"`
}

// New creates a Calculator tool ready for use.
func New() *Calculator {
	tool := agent.NewTool(
		"calculator",
		"Performs basic arithmetic. Supported operations: add, subtract, multiply, divide.",
		agent.Object(
			agent.String("operation", "Arithmetic operation to perform.",
				agent.Required(),
				agent.Enum("add", "subtract", "multiply", "divide"),
			),
			agent.Number("a", "First operand.", agent.Required()),
			agent.Number("b", "Second operand.", agent.Required()),
		),
	)
	c := &Calculator{
		Tool: tool,
		Func: agent.TypedToolFunc(func(_ context.Context, in input) (string, error) {
			var result float64
			switch in.Operation {
			case "add":
				result = in.A + in.B
			case "subtract":
				result = in.A - in.B
			case "multiply":
				result = in.A * in.B
			case "divide":
				if in.B == 0 {
					return "", fmt.Errorf("division by zero")
				}
				result = in.A / in.B
			default:
				return "", fmt.Errorf("unknown operation %q; use add, subtract, multiply, or divide", in.Operation)
			}
			if result == math.Trunc(result) && !math.IsInf(result, 0) && math.Abs(result) < 1e15 {
				return fmt.Sprintf("%.0f", result), nil
			}
			return fmt.Sprintf("%g", result), nil
		}),
		Meta: agent.ToolMetadata{
			Source:   "tools/math",
			ReadOnly: true,
		},
	}
	return c
}

// Toolset returns a single-binding Toolset for use with agent.Join.
func (c *Calculator) Toolset() agent.Toolset {
	return agent.Toolset{
		Bindings: []agent.ToolBinding{
			{Tool: c.Tool, Func: c.Func, Meta: c.Meta},
		},
	}
}
