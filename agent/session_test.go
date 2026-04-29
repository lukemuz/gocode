package agent

import (
	"context"
	"errors"
	"testing"
)

// testStore runs the full Store contract against any Store implementation.
// newStore is called once per sub-test so each sub-test starts with a clean store.
func testStore(t *testing.T, newStore func(t *testing.T) Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("Create and Get round-trip", func(t *testing.T) {
		store := newStore(t)
		s := &Session{ID: "sess-1", History: []Message{NewUserMessage("hello")}}
		if err := SetState(s, "key", "value"); err != nil {
			t.Fatal(err)
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
			t.Errorf("History = %+v, want 1 message 'hello'", got.History)
		}
		v, err := GetState[string](got, "key")
		if err != nil || v != "value" {
			t.Errorf("GetState key = %q, %v; want value, nil", v, err)
		}
	})

	t.Run("Create returns ErrSessionExists on duplicate ID", func(t *testing.T) {
		store := newStore(t)
		s := &Session{ID: "dup"}
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("first Create: %v", err)
		}
		if err := store.Create(ctx, s); !errors.Is(err, ErrSessionExists) {
			t.Errorf("second Create: got %v, want ErrSessionExists", err)
		}
	})

	t.Run("Create with empty ID returns validation error not ErrSessionExists", func(t *testing.T) {
		store := newStore(t)
		err := store.Create(ctx, &Session{ID: ""})
		if err == nil {
			t.Fatal("expected error for empty ID, got nil")
		}
		if errors.Is(err, ErrSessionExists) {
			t.Error("empty-ID error must not be ErrSessionExists")
		}
	})

	t.Run("Get returns ErrSessionNotFound for missing ID", func(t *testing.T) {
		store := newStore(t)
		if _, err := store.Get(ctx, "no-such"); !errors.Is(err, ErrSessionNotFound) {
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
		if err := store.Update(ctx, &Session{ID: "ghost"}); !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("Update: got %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("Delete removes a session", func(t *testing.T) {
		store := newStore(t)
		if err := store.Create(ctx, &Session{ID: "del-me"}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := store.Delete(ctx, "del-me"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := store.Get(ctx, "del-me"); !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("Get after Delete: got %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("Delete returns ErrSessionNotFound for missing ID", func(t *testing.T) {
		store := newStore(t)
		if err := store.Delete(ctx, "missing"); !errors.Is(err, ErrSessionNotFound) {
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
		for i, want := range []string{"a-sess", "b-sess", "c-sess"} {
			if sessions[i].ID != want {
				t.Errorf("sessions[%d].ID = %q, want %q", i, sessions[i].ID, want)
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

	t.Run("List returns non-nil empty slice when nothing matches", func(t *testing.T) {
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
		s := &Session{ID: "iso", History: []Message{NewUserMessage("original")}}
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
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
		got.History = append(got.History, NewUserMessage("v2"))
		got2, err := store.Get(ctx, "iso2")
		if err != nil {
			t.Fatalf("second Get: %v", err)
		}
		if len(got2.History) != 1 {
			t.Errorf("store reflects mutation of returned session: got %d messages, want 1", len(got2.History))
		}
	})

	t.Run("State values survive round-trip with correct types", func(t *testing.T) {
		store := newStore(t)
		s := &Session{ID: "state-rt"}
		if err := SetState(s, "name", "alice"); err != nil {
			t.Fatal(err)
		}
		if err := SetState(s, "count", 42); err != nil {
			t.Fatal(err)
		}
		if err := SetState(s, "active", true); err != nil {
			t.Fatal(err)
		}
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
		got, err := store.Get(ctx, "state-rt")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		name, err := GetState[string](got, "name")
		if err != nil || name != "alice" {
			t.Errorf("name = %q, %v; want alice, nil", name, err)
		}
		count, err := GetState[int](got, "count")
		if err != nil || count != 42 {
			t.Errorf("count = %d, %v; want 42, nil", count, err)
		}
		active, err := GetState[bool](got, "active")
		if err != nil || !active {
			t.Errorf("active = %v, %v; want true, nil", active, err)
		}
	})
}

func TestMemoryStore(t *testing.T) {
	testStore(t, func(_ *testing.T) Store { return NewMemoryStore() })
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
	for _, tc := range []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"slash", "a/b"},
		{"backslash", `a\b`},
		{"null byte", "a\x00b"},
		{"space", "a b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := store.Create(ctx, &Session{ID: tc.id}); err == nil {
				t.Errorf("Create with id %q: expected error, got nil", tc.id)
			}
		})
	}
}

func TestSave(t *testing.T) {
	ctx := context.Background()

	t.Run("Save creates when session does not exist", func(t *testing.T) {
		store := NewMemoryStore()
		s := &Session{ID: "new-save"}
		if err := Save(ctx, store, s); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if _, err := store.Get(ctx, "new-save"); err != nil {
			t.Fatalf("Get after Save: %v", err)
		}
	})

	t.Run("Save updates when session already exists", func(t *testing.T) {
		store := NewMemoryStore()
		s := &Session{ID: "existing", History: []Message{NewUserMessage("v1")}}
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
		s.History = append(s.History, NewUserMessage("v2"))
		if err := Save(ctx, store, s); err != nil {
			t.Fatalf("Save: %v", err)
		}
		got, err := store.Get(ctx, "existing")
		if err != nil {
			t.Fatal(err)
		}
		if len(got.History) != 2 {
			t.Errorf("expected 2 messages after Save update, got %d", len(got.History))
		}
	})
}

func TestGetStateKeyNotFound(t *testing.T) {
	s := &Session{ID: "x"}
	_, err := GetState[string](s, "missing")
	if err == nil {
		t.Error("expected error for missing key, got nil")
	}
}

func TestMemoryStoreListIDs(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	for _, id := range []string{"user-1", "user-2", "admin-1"} {
		if err := store.Create(ctx, &Session{ID: id}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	ids, err := store.ListIDs(ctx, "user-", 0)
	if err != nil {
		t.Fatalf("ListIDs: %v", err)
	}
	if len(ids) != 2 || ids[0] != "user-1" || ids[1] != "user-2" {
		t.Errorf("ListIDs = %v, want [user-1 user-2]", ids)
	}
}

func TestFileStoreListIDs(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"user-1", "user-2", "admin-1"} {
		if err := store.Create(ctx, &Session{ID: id}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	ids, err := store.ListIDs(ctx, "user-", 0)
	if err != nil {
		t.Fatalf("ListIDs: %v", err)
	}
	if len(ids) != 2 || ids[0] != "user-1" || ids[1] != "user-2" {
		t.Errorf("ListIDs = %v, want [user-1 user-2]", ids)
	}
}
