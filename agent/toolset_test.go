package agent

import (
	"context"
	"encoding/json"
	"testing"
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
