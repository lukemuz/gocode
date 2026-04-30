package todo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteAndRead(t *testing.T) {
	l := New()
	ts := l.Toolset()
	d := ts.Dispatch()

	in := writeInput{Items: []Item{
		{Content: "design API", Status: StatusInProgress},
		{Content: "write tests", Status: StatusPending},
	}}
	raw, _ := json.Marshal(in)
	out, err := d["todo_write"](context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[~] design API") || !strings.Contains(out, "[ ] write tests") {
		t.Fatalf("unexpected render: %q", out)
	}

	got, err := d["todo_read"](context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != out {
		t.Fatalf("read != write render:\nwrite=%q\nread=%q", out, got)
	}

	if items := l.Items(); len(items) != 2 || items[0].Content != "design API" {
		t.Fatalf("Items() returned %+v", items)
	}
}

func TestRejectsInvalidStatus(t *testing.T) {
	l := New()
	d := l.Toolset().Dispatch()
	raw := json.RawMessage(`{"items":[{"content":"x","status":"bogus"}]}`)
	if _, err := d["todo_write"](context.Background(), raw); err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestEmptyRender(t *testing.T) {
	l := New()
	d := l.Toolset().Dispatch()
	out, err := d["todo_read"](context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "empty") {
		t.Fatalf("expected empty marker, got %q", out)
	}
}

func TestReplaceSemantics(t *testing.T) {
	l := New()
	d := l.Toolset().Dispatch()
	first, _ := json.Marshal(writeInput{Items: []Item{{Content: "a", Status: StatusPending}}})
	second, _ := json.Marshal(writeInput{Items: []Item{{Content: "b", Status: StatusCompleted}}})
	if _, err := d["todo_write"](context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := d["todo_write"](context.Background(), second); err != nil {
		t.Fatal(err)
	}
	items := l.Items()
	if len(items) != 1 || items[0].Content != "b" {
		t.Fatalf("expected replace semantics, got %+v", items)
	}
}
