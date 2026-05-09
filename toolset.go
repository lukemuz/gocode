package luft

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

// Bind is the one-line constructor for a ToolBinding with no metadata.
// Equivalent to ToolBinding{Tool: t, Func: fn}.
func Bind(t Tool, fn ToolFunc) ToolBinding {
	return ToolBinding{Tool: t, Func: fn}
}

// ToolMetadata carries advisory annotations about a bound tool.
//
// Most fields are informational only; the library does not enforce policy
// based on them. Applications may inspect them to build confirmation wrappers,
// audit logs, or permission checks.
//
// Terminal is the one field with semantic effect: when a tool with
// Meta.Terminal == true is invoked successfully (no IsError result), Loop
// and LoopStream return after appending the tool result message, without
// asking the model for a final text turn. This is the primitive behind
// Extract — it lets a "submit_X" style tool double as the loop's exit signal.
type ToolMetadata struct {
	Source               string
	ReadOnly             bool
	Destructive          bool
	Network              bool
	Filesystem           bool
	Shell                bool
	RequiresConfirmation bool
	SafetyNotes          []string
	Terminal             bool
}

// Toolset is an ordered collection of ToolBindings. It is the input shape
// for Client.Loop and Client.LoopStream. Tools() and Dispatch() expose the
// raw slice and map for callers that want to inspect or use them directly.
//
// ProviderTools holds server-executed (category-1) tools advertised to the
// provider — e.g. Anthropic's web_search, code_execution. They have no Go
// implementation; the provider runs them and returns inline result blocks.
// The agent loop never inspects them: Tools() and Dispatch() ignore the slot
// and only describe local Bindings. Use the typed provider constructors
// (AnthropicWebSearch, etc.) and attach them via WithProviderTools or by
// setting the field directly.
type Toolset struct {
	Bindings      []ToolBinding
	ProviderTools []ProviderTool
}

// Tools is a variadic constructor for a Toolset. It is the most ergonomic
// way to build a small toolset literally:
//
//	tools := luft.Tools(
//	    luft.Bind(searchTool, searchFn),
//	    luft.Bind(submitTool, submitFn),
//	)
//
// Equivalent to Toolset{Bindings: []ToolBinding{...}}.
func Tools(bindings ...ToolBinding) Toolset {
	return Toolset{Bindings: bindings}
}

// WithProviderTools returns a copy of t with the supplied ProviderTools
// appended to the existing slot. Useful for fluent composition:
//
//	tools := agent.Tools(localBinding).
//	    WithProviderTools(agent.AnthropicWebSearch(agent.WebSearchOpts{MaxUses: 5}))
func (t Toolset) WithProviderTools(pt ...ProviderTool) Toolset {
	out := Toolset{
		Bindings:      t.Bindings,
		ProviderTools: append(append([]ProviderTool(nil), t.ProviderTools...), pt...),
	}
	return out
}

// CacheLast returns a copy of t with the last tool binding marked as a
// cache breakpoint. Anthropic cache markers are cumulative — a marker on
// the last tool caches the system prompt and every preceding tool — so
// this is the standard "cache the stable prefix" pattern.
//
// No-op for empty toolsets. Honored only by providers that support cache
// markers (AnthropicProvider, OpenRouterProvider routing to Anthropic);
// other providers ignore it.
func (t Toolset) CacheLast(cache *CacheControl) Toolset {
	if len(t.Bindings) == 0 || cache == nil {
		return t
	}
	out := Toolset{
		Bindings:      append([]ToolBinding(nil), t.Bindings...),
		ProviderTools: t.ProviderTools,
	}
	last := out.Bindings[len(out.Bindings)-1]
	last.Tool.CacheControl = cache
	out.Bindings[len(out.Bindings)-1] = last
	return out
}

// Tools returns the Tool slice — the model-facing definitions — derived
// from the bindings. Useful for inspection or for callers building their
// own loop on top of the primitive provider interfaces.
func (t Toolset) Tools() []Tool {
	tools := make([]Tool, len(t.Bindings))
	for i, b := range t.Bindings {
		tools[i] = b.Tool
	}
	return tools
}

// Dispatch returns the name→func map derived from the bindings.
func (t Toolset) Dispatch() map[string]ToolFunc {
	m := make(map[string]ToolFunc, len(t.Bindings))
	for _, b := range t.Bindings {
		m[b.Tool.Name] = b.Func
	}
	return m
}

// Join merges multiple Toolset values into one, preserving order.
// Returns an error if any tool name appears more than once across the sets.
// ProviderTools are concatenated without deduplication (each entry is opaque
// JSON; collision is ill-defined).
func Join(sets ...Toolset) (Toolset, error) {
	seen := make(map[string]bool)
	var result Toolset
	for _, s := range sets {
		for _, b := range s.Bindings {
			if seen[b.Tool.Name] {
				return Toolset{}, fmt.Errorf("luft: duplicate tool name %q in toolset", b.Tool.Name)
			}
			seen[b.Tool.Name] = true
			result.Bindings = append(result.Bindings, b)
		}
		result.ProviderTools = append(result.ProviderTools, s.ProviderTools...)
	}
	return result, nil
}

// MustJoin is like Join but panics on duplicate tool names. It is intended
// for static composition of toolsets at program startup, where a duplicate
// is a programmer error rather than a runtime condition. Follows the
// regexp.MustCompile / template.Must convention.
func MustJoin(sets ...Toolset) Toolset {
	t, err := Join(sets...)
	if err != nil {
		panic(err)
	}
	return t
}

// Middleware is a function that wraps a ToolBinding's Func with additional
// behaviour. The full ToolBinding (including Tool, Meta, and the current Func)
// is passed so wrappers can use the tool name, metadata, and safety notes.
// The wrapper must return a new ToolFunc; it must not mutate the binding.
type Middleware func(binding ToolBinding) ToolFunc

// Wrap applies each Middleware to every binding in the Toolset, returning a
// new Toolset with the decorated functions. Middlewares are applied in order,
// so the first one listed is outermost (executes first and last).
// ProviderTools (category-1) carry through unchanged — middleware applies
// only to local Bindings.
func (t Toolset) Wrap(middlewares ...Middleware) Toolset {
	result := Toolset{
		Bindings:      make([]ToolBinding, len(t.Bindings)),
		ProviderTools: t.ProviderTools,
	}
	for i, b := range t.Bindings {
		wrapped := b
		// Apply in reverse so the first listed middleware ends up outermost.
		for j := len(middlewares) - 1; j >= 0; j-- {
			fn := middlewares[j](wrapped)
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

// WithConfirmation returns a Middleware that calls prompt before each tool
// invocation. If prompt returns false the tool is skipped and a descriptive
// message is returned to the model instead of executing the tool. If prompt
// returns an error that error becomes a hard tool error and is surfaced to
// the caller as usual.
func WithConfirmation(prompt func(ctx context.Context, binding ToolBinding, input json.RawMessage) (bool, error)) Middleware {
	return func(b ToolBinding) ToolFunc {
		return func(ctx context.Context, input json.RawMessage) (string, error) {
			ok, err := prompt(ctx, b, input)
			if err != nil {
				return "", err
			}
			if !ok {
				return fmt.Sprintf("tool %q was not approved and was not executed", b.Tool.Name), nil
			}
			return b.Func(ctx, input)
		}
	}
}
