package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Session holds a conversation and optional caller-owned metadata.
//
// A Session is plain data. It does not call models, run tools, trim context,
// schedule work, or manage application lifecycle. The caller decides when to
// load a session, pass History to a model call, and persist the result.
//
// State holds arbitrary caller-owned metadata encoded as JSON values. Use the
// SetState and GetState helpers to read and write typed values:
//
//	agent.SetState(sess, "user_id", "u-123")
//	agent.SetState(sess, "turn", 7)
//
//	id, _ := agent.GetState[string](sess, "user_id")
//	turn, _ := agent.GetState[int](sess, "turn")
//
// Using json.RawMessage as the value type means State survives JSON
// round-trips without type loss — MemoryStore and FileStore behave
// identically for all value types.
//
// Typical usage:
//
//	sess, err := store.Get(ctx, id)
//	if errors.Is(err, agent.ErrSessionNotFound) {
//	    sess = &agent.Session{ID: id}
//	} else if err != nil {
//	    return err
//	}
//
//	sess.History = append(sess.History, agent.NewUserMessage(input))
//	result, err := a.Step(ctx, sess.History)
//	if err != nil {
//	    return err
//	}
//	sess.History = result.Messages
//
//	return agent.Save(ctx, store, sess) // creates or updates as needed
type Session struct {
	ID      string                     `json:"id"`
	History []Message                  `json:"history,omitempty"`
	State   map[string]json.RawMessage `json:"state,omitempty"`

	// Events is an append-only activity log populated when a Recorder is
	// attached to the session via RecorderToSession. It records intermediate
	// turn activity — model calls, tool calls, retries — that does not
	// appear in History. Persisted by Store implementations as plain JSON.
	Events []Event `json:"events,omitempty"`
}

// SetState marshals val as JSON and stores it under key in s.State.
func SetState[T any](s *Session, key string, val T) error {
	data, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("agent: SetState %q: %w", key, err)
	}
	if s.State == nil {
		s.State = make(map[string]json.RawMessage)
	}
	s.State[key] = data
	return nil
}

// GetState unmarshals the JSON value stored under key in s.State into a value
// of type T. Returns an error if the key is absent or the value cannot be
// decoded into T.
func GetState[T any](s *Session, key string) (T, error) {
	var zero T
	data, ok := s.State[key]
	if !ok {
		return zero, fmt.Errorf("agent: GetState: key %q not found", key)
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return zero, fmt.Errorf("agent: GetState %q: %w", key, err)
	}
	return v, nil
}

// Store persists and retrieves Sessions. All implementations must be safe for
// concurrent use.
//
// Create and Update are intentionally separate: Create fails if the ID already
// exists, Update fails if it does not. This makes the caller's intent explicit
// and avoids silent overwrites. Use Save when you do not need that distinction.
type Store interface {
	// Create persists a new Session. Returns ErrSessionExists if the ID is
	// already present.
	Create(ctx context.Context, session *Session) error

	// Get loads a Session by ID. Returns ErrSessionNotFound if the ID is
	// absent.
	Get(ctx context.Context, id string) (*Session, error)

	// Update overwrites an existing Session. Returns ErrSessionNotFound if
	// the ID is absent.
	Update(ctx context.Context, session *Session) error

	// Delete removes a Session. Returns ErrSessionNotFound if the ID is
	// absent.
	Delete(ctx context.Context, id string) error

	// List returns Sessions whose IDs have the given prefix, up to limit
	// entries sorted by ID. An empty prefix matches all IDs. A limit of 0
	// means no limit.
	List(ctx context.Context, prefix string, limit int) ([]*Session, error)
}

// Save updates s if it already exists, or creates it if not. It is a
// convenience wrapper for callers that do not need to distinguish between
// first use and subsequent calls.
func Save(ctx context.Context, store Store, s *Session) error {
	if err := store.Update(ctx, s); err == nil {
		return nil
	} else if !errors.Is(err, ErrSessionNotFound) {
		return err
	}
	return store.Create(ctx, s)
}

// Load returns the session with the given ID, or a fresh &Session{ID: id}
// if no session with that ID exists. It is the read-side symmetry of Save:
// callers that don't need to distinguish first use from subsequent use can
// rely on it for the open-or-create pattern. Other errors are returned as-is.
func Load(ctx context.Context, store Store, id string) (*Session, error) {
	sess, err := store.Get(ctx, id)
	if err == nil {
		return sess, nil
	}
	if errors.Is(err, ErrSessionNotFound) {
		return &Session{ID: id}, nil
	}
	return nil, err
}

// Sentinel errors for Store operations.
var (
	// ErrSessionNotFound is returned by Get, Update, and Delete when no
	// session with the requested ID exists.
	ErrSessionNotFound = errors.New("agent: session not found")

	// ErrSessionExists is returned by Create when a session with the given
	// ID already exists.
	ErrSessionExists = errors.New("agent: session already exists")
)

// sessionNotFoundError wraps ErrSessionNotFound with the offending ID.
type sessionNotFoundError struct{ id string }

func (e *sessionNotFoundError) Error() string {
	return fmt.Sprintf("agent: session not found: %s", e.id)
}

func (e *sessionNotFoundError) Is(target error) bool { return target == ErrSessionNotFound }

// sessionExistsError wraps ErrSessionExists with the offending ID.
type sessionExistsError struct{ id string }

func (e *sessionExistsError) Error() string {
	return fmt.Sprintf("agent: session already exists: %s", e.id)
}

func (e *sessionExistsError) Is(target error) bool { return target == ErrSessionExists }

// cloneSession returns a deep copy of s so neither the caller's original nor
// the stored copy can alias each other.
func cloneSession(s *Session) *Session {
	c := &Session{ID: s.ID}
	if len(s.History) > 0 {
		c.History = make([]Message, len(s.History))
		for i, m := range s.History {
			nm := Message{Role: m.Role}
			if len(m.Content) > 0 {
				nm.Content = make([]ContentBlock, len(m.Content))
				for j, b := range m.Content {
					nb := b
					if len(b.Input) > 0 {
						nb.Input = make(json.RawMessage, len(b.Input))
						copy(nb.Input, b.Input)
					}
					nm.Content[j] = nb
				}
			}
			c.History[i] = nm
		}
	}
	if len(s.State) > 0 {
		c.State = make(map[string]json.RawMessage, len(s.State))
		for k, v := range s.State {
			cv := make(json.RawMessage, len(v))
			copy(cv, v)
			c.State[k] = cv
		}
	}
	if len(s.Events) > 0 {
		c.Events = make([]Event, len(s.Events))
		for i, ev := range s.Events {
			ne := ev
			if len(ev.History) > 0 {
				ne.History = make([]Message, len(ev.History))
				copy(ne.History, ev.History)
			}
			if len(ev.ToolInput) > 0 {
				ne.ToolInput = make(json.RawMessage, len(ev.ToolInput))
				copy(ne.ToolInput, ev.ToolInput)
			}
			c.Events[i] = ne
		}
	}
	return c
}
