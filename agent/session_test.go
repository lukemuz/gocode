package agent

import (
	"context"
	"errors"
	"testing"
)

// testStore runs the full Store contract against any Store implementation.
// newStore is called once per sub-test so each sub-test gets a clean store.
func testStore(t *testing.T, newStore func(t *testing.T) Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("Create and Get round-trip", func(t *testing.T) {
		store := newStore(t)
		s := &Session{
			ID:      "sess-1",
			History: []Message{NewUserMessage("hello")},
			State:   map[string]any{"key": "value"},
		}
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
		got, err := store.Get(ctx, "sess-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.ID != "sess-1" {
			t.Errorf("ID = %q, want sess-1", got.ID)
		}
		if len(got.History) != 1 || TextContent(got.History[0]) != "hello" {
			t.Errorf("History = %+v, want 1 message with text 'hello'", got.History)
		}
		if got.State["key"] != "value" {
			t.Errorf("State[key] = %v, want value", got.State["key"])
		}
	})

	t.Run("Create returns ErrSessionExists on duplicate ID", func(t *testing.T) {
		store := newStore(t)
		s := &Session{ID: "dup"}
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("first Create: %v", err)
		}
		err := store.Create(ctx, s)
		if !errors.Is(err, ErrSessionExists) {
			t.Errorf("second Create: got %v, want ErrSessionExists", err)
		}
	})

	t.Run("Get returns ErrSessionNotFound for missing ID", func(t *testing.T) {
		store := newStore(t)
		_, err := store.Get(ctx, "no-such-session")
		if !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("Get: got %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("Update replaces history", func(t *testing.T) {
		store := newStore(t)
		s := &Session{ID: "upd", History: []Message{NewUserMessage("v1")}}
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
		s.History = append(s.History, NewUserMessage("v2"))
		if err := store.Update(ctx, s); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, err := store.Get(ctx, "upd")
		if err != nil {
			t.Fatalf("Get after Update: %v", err)
		}
		if len(got.History) != 2 {
			t.Errorf("expected 2 messages after update, got %d", len(got.History))
		}
	})

	t.Run("Update returns ErrSessionNotFound for missing ID", func(t *testing.T) {
		store := newStore(t)
		err := store.Update(ctx, &Session{ID: "ghost"})
		if !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("Update: got %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("Delete removes a session", func(t *testing.T) {
		store := newStore(t)
		s := &Session{ID: "del-me"}
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := store.Delete(ctx, "del-me"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		_, err := store.Get(ctx, "del-me")
		if !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("Get after Delete: got %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("Delete returns ErrSessionNotFound for missing ID", func(t *testing.T) {
		store := newStore(t)
		err := store.Delete(ctx, "missing")
		if !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("Delete: got %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("List returns all sessions sorted by ID", func(t *testing.T) {
		store := newStore(t)
		for _, id := range []string{"c-sess", "a-sess", "b-sess"} {
			if err := store.Create(ctx, &Session{ID: id}); err != nil {
				t.Fatalf("Create %s: %v", id, err)
			}
		}
		sessions, err := store.List(ctx, "", 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(sessions) != 3 {
			t.Fatalf("expected 3 sessions, got %d", len(sessions))
		}
		wantOrder := []string{"a-sess", "b-sess", "c-sess"}
		for i, s := range sessions {
			if s.ID != wantOrder[i] {
				t.Errorf("sessions[%d].ID = %q, want %q", i, s.ID, wantOrder[i])
			}
		}
	})

	t.Run("List filters by prefix", func(t *testing.T) {
		store := newStore(t)
		for _, id := range []string{"user-1", "user-2", "admin-1"} {
			if err := store.Create(ctx, &Session{ID: id}); err != nil {
				t.Fatalf("Create %s: %v", id, err)
			}
		}
		sessions, err := store.List(ctx, "user-", 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(sessions) != 2 {
			t.Errorf("expected 2 user sessions, got %d", len(sessions))
		}
		for _, s := range sessions {
			if s.ID[:5] != "user-" {
				t.Errorf("unexpected session in prefix result: %s", s.ID)
			}
		}
	})

	t.Run("List respects limit", func(t *testing.T) {
		store := newStore(t)
		for _, id := range []string{"x-1", "x-2", "x-3", "x-4"} {
			if err := store.Create(ctx, &Session{ID: id}); err != nil {
				t.Fatalf("Create %s: %v", id, err)
			}
		}
		sessions, err := store.List(ctx, "", 2)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(sessions) != 2 {
			t.Errorf("expected 2 sessions with limit=2, got %d", len(sessions))
		}
	})

	t.Run("List returns empty slice when no sessions match", func(t *testing.T) {
		store := newStore(t)
		sessions, err := store.List(ctx, "no-match-", 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if sessions == nil {
			t.Error("List should return non-nil empty slice, got nil")
		}
		if len(sessions) != 0 {
			t.Errorf("expected 0 sessions, got %d", len(sessions))
		}
	})

	t.Run("stored sessions are isolated from caller mutations", func(t *testing.T) {
		store := newStore(t)
		s := &Session{
			ID:      "iso",
			History: []Message{NewUserMessage("original")},
		}
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}

		// Mutate the original after storing.
		s.History = append(s.History, NewUserMessage("mutated"))

		got, err := store.Get(ctx, "iso")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if len(got.History) != 1 {
			t.Errorf("store reflects caller mutation: got %d messages, want 1", len(got.History))
		}
	})

	t.Run("returned sessions are isolated from subsequent mutations", func(t *testing.T) {
		store := newStore(t)
		if err := store.Create(ctx, &Session{ID: "iso2", History: []Message{NewUserMessage("v1")}}); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got, err := store.Get(ctx, "iso2")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		// Mutate the returned session.
		got.History = append(got.History, NewUserMessage("v2"))

		got2, err := store.Get(ctx, "iso2")
		if err != nil {
			t.Fatalf("second Get: %v", err)
		}
		if len(got2.History) != 1 {
			t.Errorf("store reflects mutation of returned session: got %d messages, want 1", len(got2.History))
		}
	})
}

func TestMemoryStore(t *testing.T) {
	testStore(t, func(_ *testing.T) Store {
		return NewMemoryStore()
	})
}

func TestFileStore(t *testing.T) {
	testStore(t, func(t *testing.T) Store {
		store, err := NewFileStore(t.TempDir())
		if err != nil {
			t.Fatalf("NewFileStore: %v", err)
		}
		return store
	})
}

func TestFileStoreInvalidID(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"slash", "a/b"},
		{"backslash", `a\b`},
		{"null byte", "a\x00b"},
		{"space", "a b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := store.Create(ctx, &Session{ID: tc.id})
			if err == nil {
				t.Errorf("Create with id %q: expected error, got nil", tc.id)
			}
		})
	}
}

func TestMemoryStoreEmptyID(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	if err := store.Create(ctx, &Session{ID: ""}); err == nil {
		t.Error("Create with empty ID: expected error, got nil")
	}
	if err := store.Update(ctx, &Session{ID: ""}); err == nil {
		t.Error("Update with empty ID: expected error, got nil")
	}
}
