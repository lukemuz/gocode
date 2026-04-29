package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// MemoryStore is an in-memory Store implementation. It is safe for concurrent
// use and suitable for tests and single-process development.
//
// Sessions are deep-copied on Create, Update, and Get so that callers cannot
// alias the stored data.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: make(map[string]*Session)}
}

func (m *MemoryStore) Create(_ context.Context, s *Session) error {
	if s.ID == "" {
		return fmt.Errorf("agent: MemoryStore: session ID must not be empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[s.ID]; ok {
		return &sessionExistsError{id: s.ID}
	}
	m.sessions[s.ID] = cloneSession(s)
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, &sessionNotFoundError{id: id}
	}
	return cloneSession(s), nil
}

func (m *MemoryStore) Update(_ context.Context, s *Session) error {
	if s.ID == "" {
		return fmt.Errorf("agent: MemoryStore: session ID must not be empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[s.ID]; !ok {
		return &sessionNotFoundError{id: s.ID}
	}
	m.sessions[s.ID] = cloneSession(s)
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; !ok {
		return &sessionNotFoundError{id: id}
	}
	delete(m.sessions, id)
	return nil
}

func (m *MemoryStore) List(_ context.Context, prefix string, limit int) ([]*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := m.matchingIDs(prefix, limit)
	out := make([]*Session, len(ids))
	for i, id := range ids {
		out[i] = cloneSession(m.sessions[id])
	}
	return out, nil
}

// ListIDs returns the IDs of sessions whose IDs have the given prefix, up to
// limit entries sorted alphabetically. An empty prefix matches all IDs; a
// limit of 0 means no limit. It is more efficient than List when only IDs are
// needed, since it does not clone session data.
func (m *MemoryStore) ListIDs(_ context.Context, prefix string, limit int) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.matchingIDs(prefix, limit), nil
}

// matchingIDs collects, sorts, and trims IDs matching prefix. Must be called
// with at least a read lock held.
func (m *MemoryStore) matchingIDs(prefix string, limit int) []string {
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		if strings.HasPrefix(id, prefix) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if limit > 0 && len(ids) > limit {
		ids = ids[:limit]
	}
	return ids
}
