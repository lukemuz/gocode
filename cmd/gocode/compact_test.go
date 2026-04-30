package main

import (
	"testing"

	"github.com/lukemuz/gocode"
)

func TestFindCutPointKeepsRecentTurns(t *testing.T) {
	h := []gocode.Message{
		gocode.NewUserMessage("hello"),                 // user-typed #1 (idx 0)
		{Role: gocode.RoleAssistant, Content: []gocode.ContentBlock{{Type: gocode.TypeText, Text: "hi"}}},
		gocode.NewUserMessage("now what"),              // user-typed #2 (idx 2)
		{Role: gocode.RoleAssistant, Content: []gocode.ContentBlock{{Type: gocode.TypeText, Text: "ok"}}},
		gocode.NewUserMessage("third question"),        // user-typed #3 (idx 4)
		{Role: gocode.RoleAssistant, Content: []gocode.ContentBlock{{Type: gocode.TypeText, Text: "ok"}}},
	}
	// keepRecent=2 should cut at the start of user-typed #2.
	if got := findCutPoint(h, 2); got != 2 {
		t.Fatalf("keep=2: got %d want 2", got)
	}
	if got := findCutPoint(h, 3); got != 0 {
		t.Fatalf("keep=3 (== num user turns): got %d want 0 (no compaction)", got)
	}
	if got := findCutPoint(h, 1); got != 4 {
		t.Fatalf("keep=1: got %d want 4", got)
	}
}

func TestFindCutPointSkipsToolResults(t *testing.T) {
	toolResultMsg := gocode.NewToolResultMessage([]gocode.ToolResult{{ToolUseID: "x", Content: "ok"}})
	h := []gocode.Message{
		gocode.NewUserMessage("real one"), // idx 0 — user-typed
		{Role: gocode.RoleAssistant, Content: []gocode.ContentBlock{{Type: gocode.TypeText, Text: "..."}}},
		toolResultMsg, // idx 2 — synthetic tool-result, not a user turn
		{Role: gocode.RoleAssistant, Content: []gocode.ContentBlock{{Type: gocode.TypeText, Text: "..."}}},
		gocode.NewUserMessage("second real"), // idx 4 — user-typed
	}
	// Only 2 user-typed turns; keep=1 cuts at idx 4 (the "second real").
	if got := findCutPoint(h, 1); got != 4 {
		t.Fatalf("got %d want 4 (must skip tool_result message)", got)
	}
}

func TestFindCutPointEmptyHistory(t *testing.T) {
	if got := findCutPoint(nil, 4); got != 0 {
		t.Fatalf("got %d want 0", got)
	}
}

func TestRenderTranscriptIncludesAllRoles(t *testing.T) {
	h := []gocode.Message{
		gocode.NewUserMessage("hello"),
		{Role: gocode.RoleAssistant, Content: []gocode.ContentBlock{
			{Type: gocode.TypeText, Text: "thinking"},
			{Type: gocode.TypeToolUse, ID: "abc", Name: "Grep", Input: []byte(`{"q":"foo"}`)},
		}},
	}
	out := renderTranscript(h)
	for _, want := range []string{"## user", "## assistant", "hello", "thinking", "tool_use Grep"} {
		if !contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
