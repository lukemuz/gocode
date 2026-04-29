package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// ToolBinding pairs a Tool definition with its ToolFunc implementation
// and optional advisory metadata. It is the unit of composition for Toolset.
type ToolBinding struct {
	Tool Tool
	Func ToolFunc
	Meta ToolMetadata
}

// ToolMetadata carries advisory safety annotations about a bound tool.
// Fields are informational only; the library does not enforce any policy
// based on them. Applications may inspect them to build confirmation
// wrappers, audit logs, or permission checks.
type ToolMetadata struct {
	Source               string
	ReadOnly             bool
	Destructive          bool
	Network              bool
	Filesystem           bool
	Shell                bool
	RequiresConfirmation bool
	SafetyNotes          []string
}

// Toolset is an ordered collection of ToolBindings. It produces the
// []Tool and map[string]ToolFunc slices that Client.Loop and
// Client.LoopStream expect.
type Toolset struct {
	Bindings []ToolBinding
}

// Tools returns the Tool slice for passing to Client.Loop or Client.LoopStream.
func (t Toolset) Tools() []Tool {
	tools := make([]Tool, len(t.Bindings))
	for i, b := range t.Bindings {
		tools[i] = b.Tool
	}
	return tools
}

// Dispatch returns the dispatch map for passing to Client.Loop or Client.LoopStream.
func (t Toolset) Dispatch() map[string]ToolFunc {
	m := make(map[string]ToolFunc, len(t.Bindings))
	for _, b := range t.Bindings {
		m[b.Tool.Name] = b.Func
	}
	return m
}

// Join merges multiple Toolset values into one, preserving order.
// Returns an error if any tool name appears more than once across the sets.
func Join(sets ...Toolset) (Toolset, error) {
	seen := make(map[string]bool)
	var result Toolset
	for _, s := range sets {
		for _, b := range s.Bindings {
			if seen[b.Tool.Name] {
				return Toolset{}, fmt.Errorf("agent: duplicate tool name %q in toolset", b.Tool.Name)
			}
			seen[b.Tool.Name] = true
			result.Bindings = append(result.Bindings, b)
		}
	}
	return result, nil
}

// Middleware is a function that wraps a ToolBinding's Func with additional
// behaviour. The full ToolBinding (including Tool, Meta, and the current Func)
// is passed so wrappers can use the tool name, metadata, and safety notes.
// The wrapper must return a new ToolFunc; it must not mutate the binding.
type Middleware func(binding ToolBinding) ToolFunc

// Wrap applies each Middleware to every binding in the Toolset, returning a
// new Toolset with the decorated functions. Middlewares are applied in order,
// so the first one listed is outermost (executes first and last).
func (t Toolset) Wrap(middlewares ...Middleware) Toolset {
	result := Toolset{Bindings: make([]ToolBinding, len(t.Bindings))}
	for i, b := range t.Bindings {
		wrapped := b
		for _, mw := range middlewares {
			fn := mw(wrapped)
			wrapped = ToolBinding{Tool: wrapped.Tool, Func: fn, Meta: wrapped.Meta}
		}
		result.Bindings[i] = wrapped
	}
	return result
}

// WithTimeout returns a Middleware that cancels a tool call if it exceeds d.
func WithTimeout(d time.Duration) Middleware {
	return func(b ToolBinding) ToolFunc {
		return func(ctx context.Context, input json.RawMessage) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			return b.Func(ctx, input)
		}
	}
}

// WithResultLimit returns a Middleware that truncates tool output to at most
// maxBytes bytes. This prevents unexpectedly large tool results from filling
// the model's context window.
func WithResultLimit(maxBytes int) Middleware {
	return func(b ToolBinding) ToolFunc {
		return func(ctx context.Context, input json.RawMessage) (string, error) {
			out, err := b.Func(ctx, input)
			if err != nil {
				return out, err
			}
			if len(out) > maxBytes {
				out = out[:maxBytes]
			}
			return out, nil
		}
	}
}

// Logger is the logging interface used by WithLogging. *slog.Logger satisfies
// it. Applications may supply any compatible implementation.
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

// WithLogging returns a Middleware that logs each tool call and its result at
// Info level, or Error level on failure, using the supplied Logger.
// Pass a *slog.Logger (or any Logger-compatible value) as the argument.
func WithLogging(logger Logger) Middleware {
	return func(b ToolBinding) ToolFunc {
		return func(ctx context.Context, input json.RawMessage) (string, error) {
			logger.Info("tool call", slog.String("tool", b.Tool.Name))
			out, err := b.Func(ctx, input)
			if err != nil {
				logger.Error("tool error", slog.String("tool", b.Tool.Name), slog.String("error", err.Error()))
			} else {
				logger.Info("tool result", slog.String("tool", b.Tool.Name), slog.Int("bytes", len(out)))
			}
			return out, err
		}
	}
}

// WithPanicRecovery returns a Middleware that recovers from panics inside a
// ToolFunc, converting them into ordinary errors so the agent loop can
// continue. This is useful when wrapping untrusted or third-party tool
// implementations.
func WithPanicRecovery() Middleware {
	return func(b ToolBinding) ToolFunc {
		return func(ctx context.Context, input json.RawMessage) (out string, err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("tool %q panicked: %v", b.Tool.Name, r)
				}
			}()
			return b.Func(ctx, input)
		}
	}
}
