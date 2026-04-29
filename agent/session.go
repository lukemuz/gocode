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
//	result, err := assistant.Step(ctx, sess.History)
//	if err != nil {
//	    return err
//	}
//	sess.History = result.Messages
//
//	return store.Update(ctx, sess)  // or Create for a new session
type Session struct {
	ID      string         `json:"id"`
	History []Message      `json:"history,omitempty"`
	State   map[string]any `json:"state,omitempty"`
}

// Store persists and retrieves Sessions. All implementations must be safe for
// concurrent use.
//
// Create and Update are intentionally separate: Create fails if the ID already
// exists, Update fails if it does not. This makes the caller's intent explicit
// and avoids silent overwrites.
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

func (e *sessionNotFoundError) Is(target error) bool {
	return target == ErrSessionNotFound
}

// sessionExistsError wraps ErrSessionExists with the offending ID.
type sessionExistsError struct{ id string }

func (e *sessionExistsError) Error() string {
	return fmt.Sprintf("agent: session already exists: %s", e.id)
}

func (e *sessionExistsError) Is(target error) bool {
	return target == ErrSessionExists
}

// cloneSession returns a deep copy of s, copying all slice and map fields so
// neither the caller's original nor the stored copy can alias each other.
func cloneSession(s *Session) *Session {
	c := &Session{ID: s.ID}
	if len(s.History) > 0 {
		c.History = make([]Message, len(s.History))
		for i, m := range s.History {
			nm := Message{Role: m.Role}
			if len(m.Content) > 0 {
				nm.Content = make([]ContentBlock, len(m.Content))
				for j, b := range m.Content {
					nb := b // copy scalar fields
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
		c.State = make(map[string]any, len(s.State))
		for k, v := range s.State {
			c.State[k] = v
		}
	}
	return c
}
