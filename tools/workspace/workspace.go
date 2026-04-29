// Package workspace provides a safe, sandboxed set of filesystem tools rooted
// at a configurable directory. All path arguments are resolved relative to the
// root and validated to prevent directory traversal.
//
// Use NewReadOnly for the five read-only tools (list_directory, find_files,
// search_text, read_file, file_info). Use New to also include edit_file; pair
// it with the gocode.WithConfirmation middleware to gate writes.
package workspace

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lukemuz/gocode"
)

const (
	defaultMaxFileBytes = 1 << 20 // 1 MiB
	defaultMaxResults   = 100
)

// Config controls Workspace tool behaviour.
type Config struct {
	// Root is the directory that all workspace tools are sandboxed to.
	// All relative paths supplied to tools are resolved against Root.
	// Required.
	Root string

	// MaxFileBytes caps how many bytes read_file will return. 0 uses the
	// default (1 MiB).
	MaxFileBytes int64

	// MaxResults caps the number of entries returned by find_files and
	// search_text. 0 uses the default (100).
	MaxResults int
}

// Workspace exposes a set of safe, read-only filesystem tools sandboxed to
// a root directory.
//
// Usage:
//
//	ws, err := workspace.NewReadOnly(workspace.Config{Root: "."})
//	if err != nil { ... }
//
//	// Use the toolset directly:
//	result, err := client.Loop(ctx, system, history, ws.Toolset(), 10)
//
//	// Or compose with other toolsets:
//	toolset := gocode.MustJoin(clockset, ws.Toolset())
type Workspace struct {
	root         string
	maxFileBytes int64
	maxResults   int
	bindings     []gocode.ToolBinding
}

// New creates a Workspace with all six tools: the five read-only tools from
// NewReadOnly plus edit_file for in-place text replacement. The edit_file tool
// carries RequiresConfirmation: true metadata; pair the returned Toolset with
// gocode.WithConfirmation to gate writes.
// Returns an error if cfg.Root is empty or cannot be resolved to an absolute path.
func New(cfg Config) (*Workspace, error) {
	w, err := NewReadOnly(cfg)
	if err != nil {
		return nil, err
	}
	w.bindings = append(w.bindings, w.buildEditBinding())
	return w, nil
}

// NewReadOnly creates a Workspace with the five core read-only tools:
// list_directory, find_files, search_text, read_file, and file_info.
// Returns an error if cfg.Root is empty or cannot be resolved to an
// absolute path.
func NewReadOnly(cfg Config) (*Workspace, error) {
	if cfg.Root == "" {
		return nil, fmt.Errorf("workspace: Config.Root is required")
	}
	abs, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("workspace: resolve root %q: %w", cfg.Root, err)
	}
	maxBytes := cfg.MaxFileBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxFileBytes
	}
	maxRes := cfg.MaxResults
	if maxRes == 0 {
		maxRes = defaultMaxResults
	}
	w := &Workspace{
		root:         abs,
		maxFileBytes: maxBytes,
		maxResults:   maxRes,
	}
	w.bindings = w.buildBindings()
	return w, nil
}

// Toolset returns all workspace bindings as an gocode.Toolset.
func (w *Workspace) Toolset() gocode.Toolset {
	return gocode.Toolset{Bindings: w.bindings}
}

// Tools returns the model-facing Tool slice. Most callers should use
// Toolset() and pass it to Client.Loop directly; Tools() exists for
// inspection and for callers building a custom loop on top of the
// provider primitives.
func (w *Workspace) Tools() []gocode.Tool {
	return w.Toolset().Tools()
}

// Dispatch returns the name→func map. See Tools() for when to use this.
func (w *Workspace) Dispatch() map[string]gocode.ToolFunc {
	return w.Toolset().Dispatch()
}

// safePath resolves a caller-supplied relative path against the workspace
// root, rejects absolute paths and dot-dot traversal, and returns the
// cleaned absolute path. An empty rel resolves to the root itself.
func (w *Workspace) safePath(rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths are not allowed; use a path relative to the workspace root")
	}
	abs := filepath.Join(w.root, filepath.FromSlash(rel))
	// filepath.Join calls Clean, which resolves all ".." components. Check
	// the result is still inside the root.
	rootWithSep := w.root
	if !strings.HasSuffix(rootWithSep, string(filepath.Separator)) {
		rootWithSep += string(filepath.Separator)
	}
	if abs != w.root && !strings.HasPrefix(abs, rootWithSep) {
		return "", fmt.Errorf("path %q escapes the workspace root", rel)
	}
	return abs, nil
}

// relPath converts an absolute path back to a root-relative slash path for
// display in tool results.
func (w *Workspace) relPath(abs string) string {
	rel, err := filepath.Rel(w.root, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(rel)
}

// ---- tool input types -------------------------------------------------------

type listDirInput struct {
	Path string `json:"path"`
}

type findFilesInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

type searchTextInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Include string `json:"include"`
}

type readFileInput struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type fileInfoInput struct {
	Path string `json:"path"`
}

type editFileInput struct {
	Path          string `json:"path"`
	OldString     string `json:"old_string"`
	NewString     string `json:"new_string"`
	ExpectedCount int    `json:"expected_count"`
}

// ---- binding construction ---------------------------------------------------

func (w *Workspace) buildBindings() []gocode.ToolBinding {
	meta := gocode.ToolMetadata{
		Source:     "tools/workspace",
		ReadOnly:   true,
		Filesystem: true,
	}

	listDirTool := gocode.NewTool(
		"list_directory",
		"Lists files and directories at a path relative to the workspace root. Non-recursive.",
		gocode.Object(
			gocode.String("path", `Directory to list, relative to the workspace root. Defaults to "." (the root).`),
		),
	)

	findFilesTool := gocode.NewTool(
		"find_files",
		"Finds files whose names match a glob pattern under a path relative to the workspace root. "+
			"Results are root-relative slash paths. Capped at the configured MaxResults limit.",
		gocode.Object(
			gocode.String("pattern", "Glob pattern matched against file names (e.g. \"*.go\", \"*_test.go\").", gocode.Required()),
			gocode.String("path", `Directory to search under, relative to the workspace root. Defaults to ".".`),
		),
	)

	searchTextTool := gocode.NewTool(
		"search_text",
		"Searches file contents for lines matching a regular expression. "+
			"Returns matching lines as \"file:line: content\" entries. "+
			"Capped at MaxResults matches.",
		gocode.Object(
			gocode.String("pattern", "Regular expression to search for.", gocode.Required()),
			gocode.String("path", `Directory to search under, relative to the workspace root. Defaults to ".".`),
			gocode.String("include", `Optional glob to filter which file names are searched (e.g. "*.go").`),
		),
	)

	readFileTool := gocode.NewTool(
		"read_file",
		"Reads the contents of a file. Respects MaxFileBytes. "+
			"Optionally restricts output to a line range (1-indexed, inclusive).",
		gocode.Object(
			gocode.String("path", "File path relative to the workspace root.", gocode.Required()),
			gocode.Integer("start_line", "First line to include (1-indexed). 0 or omitted means start of file."),
			gocode.Integer("end_line", "Last line to include (1-indexed, inclusive). 0 or omitted means end of file."),
		),
	)

	fileInfoTool := gocode.NewTool(
		"file_info",
		"Returns metadata (name, size, modification time, is_dir, mode) for a path relative to the workspace root.",
		gocode.Object(
			gocode.String("path", "Path relative to the workspace root.", gocode.Required()),
		),
	)

	return []gocode.ToolBinding{
		{Tool: listDirTool, Func: gocode.TypedToolFunc(w.listDirectory), Meta: meta},
		{Tool: findFilesTool, Func: gocode.TypedToolFunc(w.findFiles), Meta: meta},
		{Tool: searchTextTool, Func: gocode.TypedToolFunc(w.searchText), Meta: meta},
		{Tool: readFileTool, Func: gocode.TypedToolFunc(w.readFile), Meta: meta},
		{Tool: fileInfoTool, Func: gocode.TypedToolFunc(w.fileInfo), Meta: meta},
	}
}

func (w *Workspace) buildEditBinding() gocode.ToolBinding {
	editMeta := gocode.ToolMetadata{
		Source:               "tools/workspace",
		ReadOnly:             false,
		Destructive:          true,
		Filesystem:           true,
		RequiresConfirmation: true,
	}
	editFileTool := gocode.NewTool(
		"edit_file",
		"Replaces all occurrences of old_string with new_string in a file. "+
			"Returns an error if old_string is not found. "+
			"Set expected_count to a positive integer to assert the exact number of replacements; "+
			"the edit is rejected when the actual count differs.",
		gocode.Object(
			gocode.String("path", "File path relative to the workspace root.", gocode.Required()),
			gocode.String("old_string", "Exact string to find and replace.", gocode.Required()),
			gocode.String("new_string", "Replacement string.", gocode.Required()),
			gocode.Integer("expected_count", "Expected number of occurrences. When non-zero, the edit is rejected if the actual count differs."),
		),
	)
	return gocode.ToolBinding{Tool: editFileTool, Func: gocode.TypedToolFunc(w.editFile), Meta: editMeta}
}

// ---- tool implementations ---------------------------------------------------

func (w *Workspace) listDirectory(_ context.Context, in listDirInput) (string, error) {
	rel := in.Path
	if rel == "" {
		rel = "."
	}
	abs, err := w.safePath(rel)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", fmt.Errorf("list_directory: %w", err)
	}
	type entry struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
	}
	out := make([]entry, len(entries))
	for i, e := range entries {
		out[i] = entry{Name: e.Name(), IsDir: e.IsDir()}
	}
	return gocode.JSONResult(out)
}

func (w *Workspace) findFiles(_ context.Context, in findFilesInput) (string, error) {
	base := in.Path
	if base == "" {
		base = "."
	}
	absBase, err := w.safePath(base)
	if err != nil {
		return "", err
	}
	pattern := in.Pattern
	if pattern == "" {
		return "", fmt.Errorf("find_files: pattern is required")
	}

	var matches []string
	err = filepath.WalkDir(absBase, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable dirs
		}
		if d.IsDir() {
			return nil
		}
		matched, _ := filepath.Match(pattern, d.Name())
		if matched {
			matches = append(matches, w.relPath(path))
			if len(matches) >= w.maxResults {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("find_files: %w", err)
	}
	return gocode.JSONResult(matches)
}

func (w *Workspace) searchText(_ context.Context, in searchTextInput) (string, error) {
	base := in.Path
	if base == "" {
		base = "."
	}
	absBase, err := w.safePath(base)
	if err != nil {
		return "", err
	}
	if in.Pattern == "" {
		return "", fmt.Errorf("search_text: pattern is required")
	}
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return "", fmt.Errorf("search_text: invalid pattern: %w", err)
	}

	type match struct {
		File string `json:"file"`
		Line int    `json:"line"`
		Text string `json:"text"`
	}
	var matches []match

	err = filepath.WalkDir(absBase, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if in.Include != "" {
			ok, _ := filepath.Match(in.Include, d.Name())
			if !ok {
				return nil
			}
		}
		f, err := os.Open(path)
		if err != nil {
			return nil // skip unreadable files
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				matches = append(matches, match{
					File: w.relPath(path),
					Line: lineNum,
					Text: line,
				})
				if len(matches) >= w.maxResults {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("search_text: %w", err)
	}
	return gocode.JSONResult(matches)
}

func (w *Workspace) readFile(_ context.Context, in readFileInput) (string, error) {
	if in.Path == "" {
		return "", fmt.Errorf("read_file: path is required")
	}
	abs, err := w.safePath(in.Path)
	if err != nil {
		return "", err
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	defer f.Close()

	// No line range: read up to maxFileBytes directly.
	if in.StartLine == 0 && in.EndLine == 0 {
		limited := io.LimitReader(f, w.maxFileBytes+1)
		data, err := io.ReadAll(limited)
		if err != nil {
			return "", fmt.Errorf("read_file: %w", err)
		}
		truncated := ""
		if int64(len(data)) > w.maxFileBytes {
			data = data[:w.maxFileBytes]
			truncated = fmt.Sprintf("\n[truncated: file exceeds %d bytes]", w.maxFileBytes)
		}
		return string(data) + truncated, nil
	}

	// Line range requested: scan line by line.
	start := in.StartLine
	end := in.EndLine
	if start < 1 {
		start = 1
	}

	var sb strings.Builder
	var byteCount int64
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum < start {
			continue
		}
		if end > 0 && lineNum > end {
			break
		}
		line := scanner.Text() + "\n"
		byteCount += int64(len(line))
		if byteCount > w.maxFileBytes {
			sb.WriteString(fmt.Sprintf("[truncated: output exceeds %d bytes]", w.maxFileBytes))
			break
		}
		sb.WriteString(line)
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	return sb.String(), nil
}

type fileInfoResult struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	IsDir   bool      `json:"is_dir"`
	Mode    string    `json:"mode"`
}

func (w *Workspace) fileInfo(_ context.Context, in fileInfoInput) (string, error) {
	if in.Path == "" {
		return "", fmt.Errorf("file_info: path is required")
	}
	abs, err := w.safePath(in.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("file_info: %w", err)
	}
	out := fileInfoResult{
		Name:    info.Name(),
		Size:    info.Size(),
		ModTime: info.ModTime().UTC(),
		IsDir:   info.IsDir(),
		Mode:    info.Mode().String(),
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("file_info: marshal result: %w", err)
	}
	return string(data), nil
}

func (w *Workspace) editFile(_ context.Context, in editFileInput) (string, error) {
	if in.Path == "" {
		return "", fmt.Errorf("edit_file: path is required")
	}
	if in.OldString == "" {
		return "", fmt.Errorf("edit_file: old_string is required")
	}
	abs, err := w.safePath(in.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}
	content := string(data)
	count := strings.Count(content, in.OldString)
	if count == 0 {
		return "", fmt.Errorf("edit_file: old_string not found in %q", in.Path)
	}
	if in.ExpectedCount > 0 && count != in.ExpectedCount {
		return "", fmt.Errorf("edit_file: expected %d occurrence(s) of old_string, found %d", in.ExpectedCount, count)
	}
	updated := strings.ReplaceAll(content, in.OldString, in.NewString)
	if err := os.WriteFile(abs, []byte(updated), info.Mode()); err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}
	return fmt.Sprintf("replaced %d occurrence(s) in %s", count, in.Path), nil
}
