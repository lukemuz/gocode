package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
// Session IDs must be non-empty and may only contain letters, digits, hyphens,
// underscores, and dots. This covers UUIDs, timestamp-based IDs, and most
// common ID schemes while preventing path traversal.
type FileStore struct {
	dir string
}

// NewFileStore returns a FileStore rooted at dir, creating the directory if it
// does not exist.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("agent: FileStore: create directory: %w", err)
	}
	return &FileStore{dir: dir}, nil
}

func (f *FileStore) Create(_ context.Context, s *Session) error {
	if err := validateFileSessionID(s.ID); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("agent: FileStore: marshal session: %w", err)
	}
	path := f.path(s.ID)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return &sessionExistsError{id: s.ID}
		}
		return fmt.Errorf("agent: FileStore: create file: %w", err)
	}
	_, werr := file.Write(data)
	cerr := file.Close()
	if werr != nil {
		os.Remove(path)
		return fmt.Errorf("agent: FileStore: write session: %w", werr)
	}
	return cerr
}

func (f *FileStore) Get(_ context.Context, id string) (*Session, error) {
	if err := validateFileSessionID(id); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(f.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &sessionNotFoundError{id: id}
		}
		return nil, fmt.Errorf("agent: FileStore: read session: %w", err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("agent: FileStore: unmarshal session %s: %w", id, err)
	}
	return &s, nil
}

func (f *FileStore) Update(_ context.Context, s *Session) error {
	if err := validateFileSessionID(s.ID); err != nil {
		return err
	}
	path := f.path(s.ID)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return &sessionNotFoundError{id: s.ID}
		}
		return fmt.Errorf("agent: FileStore: stat session: %w", err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("agent: FileStore: marshal session: %w", err)
	}
	return atomicWrite(path, data)
}

func (f *FileStore) Delete(_ context.Context, id string) error {
	if err := validateFileSessionID(id); err != nil {
		return err
	}
	err := os.Remove(f.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return &sessionNotFoundError{id: id}
		}
		return fmt.Errorf("agent: FileStore: delete session: %w", err)
	}
	return nil
}

func (f *FileStore) List(_ context.Context, prefix string, limit int) ([]*Session, error) {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return nil, fmt.Errorf("agent: FileStore: read directory: %w", err)
	}

	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if strings.HasPrefix(id, prefix) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	if limit > 0 && len(ids) > limit {
		ids = ids[:limit]
	}

	out := make([]*Session, 0, len(ids))
	for _, id := range ids {
		data, err := os.ReadFile(f.path(id))
		if err != nil {
			return nil, fmt.Errorf("agent: FileStore: read session %s: %w", id, err)
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("agent: FileStore: unmarshal session %s: %w", id, err)
		}
		out = append(out, &s)
	}
	return out, nil
}

func (f *FileStore) path(id string) string {
	return filepath.Join(f.dir, id+".json")
}

// validateFileSessionID returns an error if id is empty or contains characters
// that are unsafe to use as a filename component. Allowed: letters, digits,
// hyphens, underscores, and dots.
func validateFileSessionID(id string) error {
	if id == "" {
		return fmt.Errorf("agent: FileStore: session ID must not be empty")
	}
	for _, r := range id {
		if !('a' <= r && r <= 'z') && !('A' <= r && r <= 'Z') &&
			!('0' <= r && r <= '9') && r != '-' && r != '_' && r != '.' {
			return fmt.Errorf("agent: FileStore: session ID %q contains invalid character %q", id, r)
		}
	}
	return nil
}

// atomicWrite writes data to path by writing to a sibling temp file and
// renaming it into place, preserving the previous file if the write fails.
func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session-*.tmp")
	if err != nil {
		return fmt.Errorf("agent: FileStore: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("agent: FileStore: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("agent: FileStore: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("agent: FileStore: rename temp file: %w", err)
	}
	return nil
}
