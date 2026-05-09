package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lukemuz/luft"
)

func mkBinding(name string, meta luft.ToolMetadata, fn luft.ToolFunc) luft.ToolBinding {
	t := luft.NewTool(name, "test", luft.InputSchema{Type: "object", Properties: map[string]luft.SchemaProperty{}})
	return luft.ToolBinding{Tool: t, Func: fn, Meta: meta}
}

func TestBatchRunsConcurrently(t *testing.T) {
	var counter int64
	slow := func(ctx context.Context, _ json.RawMessage) (string, error) {
		atomic.AddInt64(&counter, 1)
		time.Sleep(50 * time.Millisecond)
		return "done", nil
	}
	bindings := []luft.ToolBinding{
		mkBinding("a", luft.ToolMetadata{}, slow),
		mkBinding("b", luft.ToolMetadata{}, slow),
		mkBinding("c", luft.ToolMetadata{}, slow),
	}
	b := New(Config{Bindings: bindings})

	in := json.RawMessage(`{"calls":[
		{"name":"a","input":{}},
		{"name":"b","input":{}},
		{"name":"c","input":{}}
	]}`)
	start := time.Now()
	out, err := b.Func(context.Background(), in)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 120*time.Millisecond {
		t.Fatalf("expected concurrent execution, took %s", elapsed)
	}
	if strings.Count(out, "done") != 3 {
		t.Fatalf("expected 3 done markers, got %q", out)
	}
	if got := atomic.LoadInt64(&counter); got != 3 {
		t.Fatalf("expected 3 invocations, got %d", got)
	}
}

func TestBatchOmitsConfirmationGated(t *testing.T) {
	bindings := []luft.ToolBinding{
		mkBinding("safe", luft.ToolMetadata{}, func(context.Context, json.RawMessage) (string, error) { return "ok", nil }),
		mkBinding("dangerous", luft.ToolMetadata{RequiresConfirmation: true}, func(context.Context, json.RawMessage) (string, error) { return "ok", nil }),
	}
	b := New(Config{Bindings: bindings})

	in := json.RawMessage(`{"calls":[{"name":"dangerous","input":{}}]}`)
	out, err := b.Func(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not available") {
		t.Fatalf("expected dangerous to be rejected, got %q", out)
	}
}

func TestBatchOmitsItself(t *testing.T) {
	bindings := []luft.ToolBinding{
		mkBinding("safe", luft.ToolMetadata{}, func(context.Context, json.RawMessage) (string, error) { return "ok", nil }),
		mkBinding(Name, luft.ToolMetadata{}, func(context.Context, json.RawMessage) (string, error) { return "loop", nil }),
	}
	b := New(Config{Bindings: bindings})

	in := json.RawMessage(fmt.Sprintf(`{"calls":[{"name":%q,"input":{}}]}`, Name))
	out, err := b.Func(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not available") {
		t.Fatalf("expected batch self-call to be rejected, got %q", out)
	}
}

func TestBatchPropagatesErrors(t *testing.T) {
	bindings := []luft.ToolBinding{
		mkBinding("ok", luft.ToolMetadata{}, func(context.Context, json.RawMessage) (string, error) { return "fine", nil }),
		mkBinding("bad", luft.ToolMetadata{}, func(context.Context, json.RawMessage) (string, error) { return "", fmt.Errorf("boom") }),
	}
	b := New(Config{Bindings: bindings})

	in := json.RawMessage(`{"calls":[{"name":"ok","input":{}},{"name":"bad","input":{}}]}`)
	out, err := b.Func(context.Background(), in)
	if err != nil {
		t.Fatalf("batch itself should not fail: %v", err)
	}
	if !strings.Contains(out, "fine") || !strings.Contains(out, "boom") {
		t.Fatalf("expected both results, got %q", out)
	}
}
