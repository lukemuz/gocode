package clock_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lukemuz/gocode/agent/tools/clock"
)

func TestClockToolName(t *testing.T) {
	c := clock.New()
	if c.Tool.Name != "current_time" {
		t.Errorf("want name %q, got %q", "current_time", c.Tool.Name)
	}
}

func TestClockFuncReturnsRFC3339(t *testing.T) {
	c := clock.New()
	before := time.Now().UTC().Truncate(time.Second)
	got, err := c.Func(context.Background(), json.RawMessage(`{}`))
	after := time.Now().UTC().Add(time.Second)
	if err != nil {
		t.Fatalf("clock func error: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("result is not RFC3339: %q: %v", got, err)
	}
	if parsed.Before(before) || parsed.After(after) {
		t.Errorf("time %v out of range [%v, %v]", parsed, before, after)
	}
}

func TestClockToolset(t *testing.T) {
	c := clock.New()
	ts := c.Toolset()
	if len(ts.Bindings) != 1 {
		t.Fatalf("want 1 binding, got %d", len(ts.Bindings))
	}
	tools := ts.Tools()
	if len(tools) != 1 || tools[0].Name != "current_time" {
		t.Errorf("unexpected tools: %v", tools)
	}
	dispatch := ts.Dispatch()
	if _, ok := dispatch["current_time"]; !ok {
		t.Error("dispatch missing current_time")
	}
}

func TestClockMetaReadOnly(t *testing.T) {
	c := clock.New()
	if !c.Meta.ReadOnly {
		t.Error("want ReadOnly=true")
	}
}
