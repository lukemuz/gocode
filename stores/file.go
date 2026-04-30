package stores

import (
	"github.com/lukemuz/gocode"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileStore is a Store implementation that persists each Session as a JSON
// file named <id>.json in a single directory. It is suitable for simple local
// applications that need durable conversation history without a database.
//
// Writes are atomic: each Create and Update operation writes to a temporary
// file in the same directory and renames it into place, so a crash during a
// write leaves the previous version intact.
//
// FileStore is safe for concurrent use within one process. It does not
// coordinate with other processes that may read or write the same directory.
//
// gocode.Session IDs must be non-empty and may only contain letters, digits, hyphens,
// underscores, and dots. This covers UUIDs, timestamp-based IDs, and most
// common ID schemes while preventing path traversal.
type FileStore struct {
	dir string
	mu  sync.RWMutex
}

// NewFileStore returns a FileStore rooted at dir, creating the directory if it
// does not exist.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("gocode: FileStore: create directory: %w", err)
	}
	return &FileStore{dir: dir}, nil
}

func (f *FileStore) Create(_ context.Context, s *gocode.Session) error {
	if err := validateFileSessionID(s.ID); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("gocode: FileStore: marshal session: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := f.path(s.ID)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return gocode.SessionExists(s.ID)
		}
		return fmt.Errorf("gocode: FileStore: create file: %w", err)
	}
	_, werr := file.Write(data)
	cerr := file.Close()
	if werr != nil {
		os.Remove(path)
		return fmt.Errorf("gocode: FileStore: write session: %w", werr)
	}
	return cerr
}

func (f *FileStore) Get(_ context.Context, id string) (*gocode.Session, error) {
	if err := validateFileSessionID(id); err != nil {
		return nil, err
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.readSession(id)
}

func (f *FileStore) Update(_ context.Context, s *gocode.Session) error {
	if err := validateFileSessionID(s.ID); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("gocode: FileStore: marshal session: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// Existence check and write are both under the write lock, eliminating
	// the TOCTOU race that would exist if they were separate operations.
	if _, err := os.Stat(f.path(s.ID)); err != nil {
		if os.IsNotExist(err) {
			return gocode.SessionNotFound(s.ID)
		}
		return fmt.Errorf("gocode: FileStore: stat session: %w", err)
	}
	return atomicWrite(f.path(s.ID), data)
}

func (f *FileStore) Delete(_ context.Context, id string) error {
	if err := validateFileSessionID(id); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := os.Remove(f.path(id)); err != nil {
		if os.IsNotExist(err) {
			return gocode.SessionNotFound(id)
		}
		return fmt.Errorf("gocode: FileStore: delete session: %w", err)
	}
	return nil
}

func (f *FileStore) List(_ context.Context, prefix string, limit int) ([]*gocode.Session, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	ids, err := f.matchingIDs(prefix, limit)
	if err != nil {
		return nil, err
	}

	out := make([]*gocode.Session, 0, len(ids))
	for _, id := range ids {
		s, err := f.readSession(id)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// ListIDs returns the IDs of sessions whose IDs have the given prefix, up to
// limit entries sorted alphabetically. An empty prefix matches all IDs; a
// limit of 0 means no limit. It is more efficient than List when only IDs are
// needed, since it reads only directory entries without loading file contents.
func (f *FileStore) ListIDs(_ context.Context, prefix string, limit int) ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.matchingIDs(prefix, limit)
}

// matchingIDs scans the directory and returns IDs with the given prefix,
// sorted and capped at limit. Must be called with at least a read lock held.
func (f *FileStore) matchingIDs(prefix string, limit int) ([]string, error) {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return nil, fmt.Errorf("gocode: FileStore: read directory: %w", err)
	}
	// ReadDir returns entries sorted by filename, so no explicit sort needed.
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if strings.HasPrefix(id, prefix) {
			ids = append(ids, id)
		}
		if limit > 0 && len(ids) == limit {
			break
		}
	}
	return ids, nil
}

// readSession loads and decodes the session file for id. Must be called with
// at least a read lock held.
func (f *FileStore) readSession(id string) (*gocode.Session, error) {
	data, err := os.ReadFile(f.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, gocode.SessionNotFound(id)
		}
		return nil, fmt.Errorf("gocode: FileStore: read session: %w", err)
	}
	var s gocode.Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("gocode: FileStore: unmarshal session %s: %w", id, err)
	}
	return &s, nil
}

func (f *FileStore) path(id string) string {
	return filepath.Join(f.dir, id+".json")
}

// validateFileSessionID returns an error if id is empty or contains characters
// that are unsafe to use as a filename component. Allowed: letters, digits,
// hyphens, underscores, and dots.
func validateFileSessionID(id string) error {
	if id == "" {
		return fmt.Errorf("gocode: FileStore: session ID must not be empty")
	}
	for _, r := range id {
		if !('a' <= r && r <= 'z') && !('A' <= r && r <= 'Z') &&
			!('0' <= r && r <= '9') && r != '-' && r != '_' && r != '.' {
			return fmt.Errorf("gocode: FileStore: session ID %q contains invalid character %q", id, r)
		}
	}
	return nil
}

// atomicWrite writes data to path by writing to a sibling temp file and
// renaming it into place, preserving the previous file if the write fails.
func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session-*.tmp")
	if err != nil {
		return fmt.Errorf("gocode: FileStore: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("gocode: FileStore: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("gocode: FileStore: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("gocode: FileStore: rename temp file: %w", err)
	}
	return nil
}
