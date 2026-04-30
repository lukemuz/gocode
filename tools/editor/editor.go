// Package editor implements the handlers for Anthropic's str_replace_based_edit_tool
// (text_editor_20250728) sandboxed to a workspace root.
//
// Pair the handler with anthropic.TextEditor20250728 to register a binding
// the model has been post-trained on:
//
//	ed, _ := editor.New(editor.Config{Root: "."})
//	binding := anthropic.TextEditor20250728(ed.Handler())
//	tools := gocode.Tools(binding)
//
// Four commands are supported:
//
//   - view:        read a file (with optional [start,end] line range and
//                  max_characters cap) or list a directory's entries
//   - str_replace: in-place exact-string replacement; rejects when old_str
//                  is missing or non-unique
//   - create:      write a new file (refuses to overwrite existing files)
//   - insert:      insert new_str after a given line (0 = beginning)
//
// All paths are validated against the workspace root and rejected if they
// escape it or are absolute.
package editor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lukemuz/gocode"
)

const (
	defaultMaxCharacters = 200_000
	defaultMaxFileBytes  = 5 << 20 // 5 MiB hard cap on files we'll touch
)

// Config controls Editor behaviour.
type Config struct {
	// Root is the directory all paths are resolved against. Required.
	Root string

	// MaxCharacters is the default cap on bytes returned by view when the
	// model does not supply max_characters. 0 uses 200_000.
	MaxCharacters int

	// MaxFileBytes caps how large a file the editor will read or modify.
	// 0 uses 5 MiB.
	MaxFileBytes int64
}

// Editor implements the str_replace_based_edit_tool commands.
type Editor struct {
	root          string
	maxChars      int
	maxFileBytes  int64
}

// New constructs an Editor.
func New(cfg Config) (*Editor, error) {
	if cfg.Root == "" {
		return nil, fmt.Errorf("editor: Config.Root is required")
	}
	abs, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("editor: resolve root: %w", err)
	}
	maxChars := cfg.MaxCharacters
	if maxChars == 0 {
		maxChars = defaultMaxCharacters
	}
	maxBytes := cfg.MaxFileBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxFileBytes
	}
	return &Editor{root: abs, maxChars: maxChars, maxFileBytes: maxBytes}, nil
}

// Handler returns a ToolFunc suitable for anthropic.TextEditor20250728.
func (e *Editor) Handler() gocode.ToolFunc {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var in editorInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", fmt.Errorf("editor: parse input: %w", err)
		}
		switch in.Command {
		case "view":
			return e.view(in)
		case "str_replace":
			return e.strReplace(in)
		case "create":
			return e.create(in)
		case "insert":
			return e.insert(in)
		case "":
			return "", fmt.Errorf("editor: command is required")
		default:
			return "", fmt.Errorf("editor: unsupported command %q (want view|str_replace|create|insert)", in.Command)
		}
	}
}

// editorInput is the union of fields the model may emit. The active set
// depends on Command.
type editorInput struct {
	Command       string `json:"command"`
	Path          string `json:"path"`
	OldStr        string `json:"old_str,omitempty"`
	NewStr        string `json:"new_str,omitempty"`
	FileText      string `json:"file_text,omitempty"`
	InsertLine    *int   `json:"insert_line,omitempty"`
	ViewRange     []int  `json:"view_range,omitempty"`
	MaxCharacters int    `json:"max_characters,omitempty"`
}

func (e *Editor) safePath(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("path is required")
	}
	// The trained tool emits absolute-looking paths sometimes; treat any
	// path as relative to root by stripping a leading slash.
	rel = strings.TrimPrefix(rel, "/")
	abs := filepath.Clean(filepath.Join(e.root, filepath.FromSlash(rel)))
	rootSep := e.root
	if !strings.HasSuffix(rootSep, string(filepath.Separator)) {
		rootSep += string(filepath.Separator)
	}
	if abs != e.root && !strings.HasPrefix(abs, rootSep) {
		return "", fmt.Errorf("path %q escapes the workspace root", rel)
	}
	return abs, nil
}

func (e *Editor) relPath(abs string) string {
	rel, err := filepath.Rel(e.root, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(rel)
}

func (e *Editor) view(in editorInput) (string, error) {
	abs, err := e.safePath(in.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("view: %w", err)
	}
	if info.IsDir() {
		entries, err := os.ReadDir(abs)
		if err != nil {
			return "", fmt.Errorf("view: %w", err)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s/\n", e.relPath(abs))
		for _, ent := range entries {
			suffix := ""
			if ent.IsDir() {
				suffix = "/"
			}
			fmt.Fprintf(&b, "  %s%s\n", ent.Name(), suffix)
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
	if info.Size() > e.maxFileBytes {
		return "", fmt.Errorf("view: file is %d bytes, exceeds limit %d", info.Size(), e.maxFileBytes)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("view: %w", err)
	}
	text := string(data)

	if len(in.ViewRange) == 2 {
		lines := strings.Split(text, "\n")
		start, end := in.ViewRange[0], in.ViewRange[1]
		if start < 1 {
			start = 1
		}
		if end == -1 || end > len(lines) {
			end = len(lines)
		}
		if start > end {
			return "", fmt.Errorf("view: invalid range [%d,%d]", in.ViewRange[0], in.ViewRange[1])
		}
		var b strings.Builder
		for i := start - 1; i < end; i++ {
			fmt.Fprintf(&b, "%6d\t%s\n", i+1, lines[i])
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}

	cap := in.MaxCharacters
	if cap <= 0 {
		cap = e.maxChars
	}
	if len(text) > cap {
		text = text[:cap] + fmt.Sprintf("\n... [truncated %d more bytes; pass max_characters or view_range to see more]", len(text)-cap)
	}
	// Number lines for the model's benefit.
	lines := strings.Split(text, "\n")
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, line)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (e *Editor) strReplace(in editorInput) (string, error) {
	if in.OldStr == "" {
		return "", fmt.Errorf("str_replace: old_str is required")
	}
	abs, err := e.safePath(in.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("str_replace: %w", err)
	}
	if info.Size() > e.maxFileBytes {
		return "", fmt.Errorf("str_replace: file is %d bytes, exceeds limit %d", info.Size(), e.maxFileBytes)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("str_replace: %w", err)
	}
	text := string(data)
	count := strings.Count(text, in.OldStr)
	if count == 0 {
		return "", fmt.Errorf("str_replace: old_str not found in %q", in.Path)
	}
	if count > 1 {
		return "", fmt.Errorf("str_replace: old_str matches %d times in %q; make it unique by adding context", count, in.Path)
	}
	updated := strings.Replace(text, in.OldStr, in.NewStr, 1)
	if err := os.WriteFile(abs, []byte(updated), info.Mode().Perm()); err != nil {
		return "", fmt.Errorf("str_replace: %w", err)
	}
	return fmt.Sprintf("edited %s (1 replacement)", e.relPath(abs)), nil
}

func (e *Editor) create(in editorInput) (string, error) {
	abs, err := e.safePath(in.Path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("create: %q already exists; use str_replace to modify it", in.Path)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	if err := os.WriteFile(abs, []byte(in.FileText), 0o644); err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	return fmt.Sprintf("created %s (%d bytes)", e.relPath(abs), len(in.FileText)), nil
}

func (e *Editor) insert(in editorInput) (string, error) {
	if in.InsertLine == nil {
		return "", fmt.Errorf("insert: insert_line is required")
	}
	abs, err := e.safePath(in.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("insert: %w", err)
	}
	if info.Size() > e.maxFileBytes {
		return "", fmt.Errorf("insert: file is %d bytes, exceeds limit %d", info.Size(), e.maxFileBytes)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("insert: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	at := *in.InsertLine
	if at < 0 || at > len(lines) {
		return "", fmt.Errorf("insert: insert_line %d is out of range [0,%d]", at, len(lines))
	}
	newLines := strings.Split(in.NewStr, "\n")
	out := make([]string, 0, len(lines)+len(newLines))
	out = append(out, lines[:at]...)
	out = append(out, newLines...)
	out = append(out, lines[at:]...)
	if err := os.WriteFile(abs, []byte(strings.Join(out, "\n")), info.Mode().Perm()); err != nil {
		return "", fmt.Errorf("insert: %w", err)
	}
	return fmt.Sprintf("inserted %d line(s) into %s after line %d", len(newLines), e.relPath(abs), at), nil
}
