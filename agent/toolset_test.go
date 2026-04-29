package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestToolsetToolsAndDispatch(t *testing.T) {
	makeBinding := func(name string) ToolBinding {
		schema, _ := json.Marshal(InputSchema{Type: "object", Properties: map[string]SchemaProperty{}})
		return ToolBinding{
			Tool: Tool{Name: name, Description: name, InputSchema: schema},
			Func: func(_ context.Context, _ json.RawMessage) (string, error) { return name, nil },
		}
	}

	ts := Toolset{Bindings: []ToolBinding{makeBinding("alpha"), makeBinding("beta")}}

	tools := ts.Tools()
	if len(tools) != 2 {
		t.Fatalf("want 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "alpha" || tools[1].Name != "beta" {
		t.Errorf("unexpected tool names: %v %v", tools[0].Name, tools[1].Name)
	}

	dispatch := ts.Dispatch()
	if len(dispatch) != 2 {
		t.Fatalf("want 2 dispatch entries, got %d", len(dispatch))
	}
	for _, name := range []string{"alpha", "beta"} {
		fn, ok := dispatch[name]
		if !ok {
			t.Fatalf("dispatch missing %q", name)
		}
		got, err := fn(context.Background(), nil)
		if err != nil || got != name {
			t.Errorf("dispatch[%q]() = %q, %v; want %q, nil", name, got, err, name)
		}
	}
}

func TestJoin(t *testing.T) {
	makeBinding := func(name string) ToolBinding {
		schema, _ := json.Marshal(InputSchema{Type: "object", Properties: map[string]SchemaProperty{}})
		return ToolBinding{Tool: Tool{Name: name, InputSchema: schema}}
	}
	a := Toolset{Bindings: []ToolBinding{makeBinding("a1"), makeBinding("a2")}}
	b := Toolset{Bindings: []ToolBinding{makeBinding("b1")}}

	got, err := Join(a, b)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if len(got.Bindings) != 3 {
		t.Fatalf("want 3 bindings, got %d", len(got.Bindings))
	}
}

func TestJoinDuplicateError(t *testing.T) {
	makeBinding := func(name string) ToolBinding {
		schema, _ := json.Marshal(InputSchema{Type: "object", Properties: map[string]SchemaProperty{}})
		return ToolBinding{Tool: Tool{Name: name, InputSchema: schema}}
	}
	a := Toolset{Bindings: []ToolBinding{makeBinding("dup")}}
	b := Toolset{Bindings: []ToolBinding{makeBinding("dup")}}

	_, err := Join(a, b)
	if err == nil {
		t.Fatal("want error for duplicate tool name, got nil")
	}
}

func TestEmptyToolset(t *testing.T) {
	ts := Toolset{}
	if tools := ts.Tools(); len(tools) != 0 {
		t.Errorf("want 0 tools, got %d", len(tools))
	}
	if d := ts.Dispatch(); len(d) != 0 {
		t.Errorf("want 0 dispatch entries, got %d", len(d))
	}
}

// ---- Middleware tests -------------------------------------------------------

func makeTestBinding(name string, fn ToolFunc) ToolBinding {
	schema, _ := json.Marshal(InputSchema{Type: "object", Properties: map[string]SchemaProperty{}})
	return ToolBinding{
		Tool: Tool{Name: name, Description: name, InputSchema: schema},
		Func: fn,
	}
}

func TestWrapPreservesOrder(t *testing.T) {
	var calls []string
	ts := Toolset{Bindings: []ToolBinding{
		makeTestBinding("a", func(_ context.Context, _ json.RawMessage) (string, error) {
			calls = append(calls, "a")
			return "a", nil
		}),
		makeTestBinding("b", func(_ context.Context, _ json.RawMessage) (string, error) {
			calls = append(calls, "b")
			return "b", nil
		}),
	}}

	wrapped := ts.Wrap() // no-op wrap
	dispatch := wrapped.Dispatch()
	dispatch["a"](context.Background(), nil)
	dispatch["b"](context.Background(), nil)

	if len(calls) != 2 || calls[0] != "a" || calls[1] != "b" {
		t.Errorf("unexpected call order: %v", calls)
	}
}

func TestWithTimeout(t *testing.T) {
	ts := Toolset{Bindings: []ToolBinding{
		makeTestBinding("slow", func(ctx context.Context, _ json.RawMessage) (string, error) {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(10 * time.Second):
				return "done", nil
			}
		}),
	}}

	wrapped := ts.Wrap(WithTimeout(10 * time.Millisecond))
	_, err := wrapped.Dispatch()["slow"](context.Background(), nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestWithResultLimit(t *testing.T) {
	ts := Toolset{Bindings: []ToolBinding{
		makeTestBinding("big", func(_ context.Context, _ json.RawMessage) (string, error) {
			return strings.Repeat("x", 1000), nil
		}),
	}}

	wrapped := ts.Wrap(WithResultLimit(100))
	out, err := wrapped.Dispatch()["big"](context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 100 {
		t.Errorf("expected 100 bytes, got %d", len(out))
	}
}

func TestWithResultLimitPassesError(t *testing.T) {
	want := errors.New("tool failed")
	ts := Toolset{Bindings: []ToolBinding{
		makeTestBinding("fail", func(_ context.Context, _ json.RawMessage) (string, error) {
			return "", want
		}),
	}}

	wrapped := ts.Wrap(WithResultLimit(100))
	_, err := wrapped.Dispatch()["fail"](context.Background(), nil)
	if !errors.Is(err, want) {
		t.Errorf("expected %v, got %v", want, err)
	}
}

type testLogger struct{ entries []string }

func (l *testLogger) Info(msg string, args ...any)  { l.entries = append(l.entries, "INFO:"+msg) }
func (l *testLogger) Error(msg string, args ...any) { l.entries = append(l.entries, "ERROR:"+msg) }

func TestWithLoggingSuccess(t *testing.T) {
	logger := &testLogger{}
	ts := Toolset{Bindings: []ToolBinding{
		makeTestBinding("mytool", func(_ context.Context, _ json.RawMessage) (string, error) {
			return "ok", nil
		}),
	}}

	wrapped := ts.Wrap(WithLogging(logger))
	wrapped.Dispatch()["mytool"](context.Background(), nil)

	if len(logger.entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d: %v", len(logger.entries), logger.entries)
	}
}

func TestWithLoggingError(t *testing.T) {
	logger := &testLogger{}
	ts := Toolset{Bindings: []ToolBinding{
		makeTestBinding("bad", func(_ context.Context, _ json.RawMessage) (string, error) {
			return "", errors.New("boom")
		}),
	}}

	wrapped := ts.Wrap(WithLogging(logger))
	wrapped.Dispatch()["bad"](context.Background(), nil)

	hasError := false
	for _, e := range logger.entries {
		if strings.HasPrefix(e, "ERROR:") {
			hasError = true
		}
	}
	if !hasError {
		t.Errorf("expected an ERROR log entry, got: %v", logger.entries)
	}
}

func TestWithPanicRecovery(t *testing.T) {
	ts := Toolset{Bindings: []ToolBinding{
		makeTestBinding("panicky", func(_ context.Context, _ json.RawMessage) (string, error) {
			panic("something went wrong")
		}),
	}}

	wrapped := ts.Wrap(WithPanicRecovery())
	out, err := wrapped.Dispatch()["panicky"](context.Background(), nil)
	if err == nil {
		t.Fatal("expected error from panic recovery")
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Errorf("expected 'panicked' in error message, got: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output, got %q", out)
	}
}

func TestWithLoggingSlogCompatible(t *testing.T) {
	// Ensure *slog.Logger satisfies the Logger interface.
	var _ Logger = slog.Default()
}
