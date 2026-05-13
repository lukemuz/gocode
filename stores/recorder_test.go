package stores_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lukemuz/luft"
	"github.com/lukemuz/luft/stores"
)

func TestSession_EventsRoundTripThroughFileStore(t *testing.T) {
	dir := t.TempDir()
	store, err := stores.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sess := &luft.Session{ID: "s1"}
	rec := luft.RecorderToSession(sess)
	rec.Record(context.Background(), luft.Event{
		Type: luft.EventToolCallStart, TurnID: "t1", Iter: 0,
		ToolUseID: "u1", ToolName: "echo",
		ToolInput: json.RawMessage(`{"a":1}`),
	})
	rec.Record(context.Background(), luft.Event{
		Type: luft.EventToolCallEnd, TurnID: "t1", Iter: 0,
		ToolUseID: "u1", ToolName: "echo", ToolOutput: "ok",
	})
	if err := luft.Save(context.Background(), store, sess); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 2 {
		t.Fatalf("want 2 events after roundtrip, got %d", len(got.Events))
	}
	if got.Events[0].ToolName != "echo" || got.Events[0].ToolUseID != "u1" {
		t.Errorf("event 0 lost fields: %+v", got.Events[0])
	}
	if string(got.Events[0].ToolInput) != `{"a":1}` {
		t.Errorf("event 0 ToolInput: got %s", got.Events[0].ToolInput)
	}
	if got.Events[1].ToolOutput != "ok" {
		t.Errorf("event 1 ToolOutput: got %q", got.Events[1].ToolOutput)
	}
}
