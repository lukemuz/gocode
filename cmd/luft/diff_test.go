package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func init() {
	// Force plain output for stable assertions; tests never run on a TTY
	// anyway, but be explicit.
	useColor = false
}

func TestRenderEditPreview_StrReplace(t *testing.T) {
	in := []byte(`{"command":"str_replace","path":"foo.go","old_str":"a := 1\nb := 2","new_str":"a := 10\nb := 20"}`)
	out, ok := renderEditPreview("str_replace_based_edit_tool", in)
	if !ok {
		t.Fatal("expected ok=true for str_replace")
	}
	wants := []string{"edit", "foo.go", "- a := 1", "- b := 2", "+ a := 10", "+ b := 20"}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
}

func TestRenderEditPreview_Create(t *testing.T) {
	in := []byte(`{"command":"create","path":"new.go","file_text":"package x\n\nfunc Y() {}\n"}`)
	out, ok := renderEditPreview("str_replace_based_edit_tool", in)
	if !ok {
		t.Fatal("expected ok=true for create")
	}
	for _, w := range []string{"create", "new.go", "new file", "+ package x", "+ func Y() {}"} {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
}

func TestRenderEditPreview_Insert(t *testing.T) {
	in := []byte(`{"command":"insert","path":"f.go","insert_line":3,"new_str":"// hello\nx := 1"}`)
	out, ok := renderEditPreview("str_replace_based_edit_tool", in)
	if !ok {
		t.Fatal("expected ok=true for insert")
	}
	for _, w := range []string{"insert", "f.go", "after line 3", "+ // hello", "+ x := 1"} {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
}

func TestRenderEditPreview_View_FallsBack(t *testing.T) {
	in := []byte(`{"command":"view","path":"f.go"}`)
	if _, ok := renderEditPreview("str_replace_based_edit_tool", in); ok {
		t.Fatal("view should fall through to JSON path (ok=false)")
	}
}

func TestRenderEditPreview_NonEditorTool(t *testing.T) {
	in := []byte(`{"command":"ls"}`)
	if _, ok := renderEditPreview("bash", in); ok {
		t.Fatal("bash should fall through (ok=false)")
	}
}

func TestRenderEditPreview_TruncatesLongBodies(t *testing.T) {
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "x")
	}
	body := strings.Join(lines, "\n")
	in, err := json.Marshal(map[string]string{
		"command":   "create",
		"path":      "big.txt",
		"file_text": body,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, ok := renderEditPreview("str_replace_based_edit_tool", in)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !strings.Contains(out, "more line") {
		t.Errorf("expected truncation marker; got:\n%s", out)
	}
}
