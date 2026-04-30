package gocode

import (
	"encoding/json"
	"testing"
)

func TestContentBlockOpaqueRoundTrip(t *testing.T) {
	// Provider responses return block types we don't model directly
	// (server_tool_use, web_search_tool_result, etc.). ContentBlock must
	// capture them verbatim and re-emit them on the next request so
	// multi-turn conversations preserve provider history.
	wire := `[
        {"type":"text","text":"hello"},
        {"type":"server_tool_use","id":"srvtu_1","name":"web_search","input":{"query":"go"}},
        {"type":"web_search_tool_result","tool_use_id":"srvtu_1","content":[{"type":"web_search_result","title":"Go"}]}
    ]`
	var blocks []ContentBlock
	if err := json.Unmarshal([]byte(wire), &blocks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks, got %d", len(blocks))
	}
	if blocks[0].Type != TypeText || blocks[0].Text != "hello" {
		t.Errorf("text block decoded wrong: %+v", blocks[0])
	}
	if blocks[1].Type != "server_tool_use" || len(blocks[1].Raw) == 0 {
		t.Errorf("server_tool_use should be opaque: %+v", blocks[1])
	}
	if blocks[2].Type != "web_search_tool_result" || len(blocks[2].Raw) == 0 {
		t.Errorf("web_search_tool_result should be opaque: %+v", blocks[2])
	}

	// Round-trip: re-encoding the slice must reproduce semantically equal
	// JSON for opaque blocks.
	out, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	var roundTripped []map[string]any
	if err := json.Unmarshal(out, &roundTripped); err != nil {
		t.Fatalf("decode round-trip: %v", err)
	}
	if roundTripped[1]["type"] != "server_tool_use" || roundTripped[1]["id"] != "srvtu_1" {
		t.Errorf("server_tool_use lost data: %v", roundTripped[1])
	}
	if roundTripped[2]["type"] != "web_search_tool_result" || roundTripped[2]["tool_use_id"] != "srvtu_1" {
		t.Errorf("web_search_tool_result lost data: %v", roundTripped[2])
	}
}

func TestExtractToolUsesIgnoresOpaqueBlocks(t *testing.T) {
	// The agent loop must not try to dispatch server_tool_use blocks
	// locally — they are executed by the provider.
	blocks := []ContentBlock{
		{Type: TypeText, Text: "thinking"},
		{Type: "server_tool_use", Raw: json.RawMessage(`{"type":"server_tool_use","id":"x"}`)},
		{Type: TypeToolUse, ID: "tu_local", Name: "local_tool", Input: json.RawMessage(`{}`)},
	}
	uses := extractToolUses(blocks)
	if len(uses) != 1 || uses[0].ID != "tu_local" {
		t.Errorf("extractToolUses returned %v, want only local tool", uses)
	}
}

func TestToolset_CacheLast(t *testing.T) {
	a := NewTool("a", "", Object())
	b := NewTool("b", "", Object())
	ts := Tools(Bind(a, nil), Bind(b, nil)).CacheLast(Ephemeral())
	if ts.Bindings[0].Tool.CacheControl != nil {
		t.Errorf("first tool should not be marked: %v", ts.Bindings[0].Tool.CacheControl)
	}
	if ts.Bindings[1].Tool.CacheControl == nil {
		t.Error("last tool should be marked as cache breakpoint")
	}
}

func TestToolset_CacheLastEmptyIsNoOp(t *testing.T) {
	ts := Tools().CacheLast(Ephemeral())
	if len(ts.Bindings) != 0 {
		t.Errorf("expected empty toolset, got %d", len(ts.Bindings))
	}
}
