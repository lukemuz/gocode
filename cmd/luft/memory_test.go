package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectMemoryReadsAgentsAndClaude(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agents content"), 0o644)
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("claude content"), 0o644)
	got := loadProjectMemory(dir)
	if !strings.Contains(got, "agents content") || !strings.Contains(got, "claude content") {
		t.Fatalf("missing content: %q", got)
	}
}

func TestLoadProjectMemoryEmptyWhenNothingPresent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // ensure we don't pick up a real ~/.claude/CLAUDE.md
	got := loadProjectMemory(dir)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestLoadProjectMemorySkipsBlankFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("   \n\n"), 0o644)
	if got := loadProjectMemory(dir); got != "" {
		t.Fatalf("expected blank file to be skipped, got %q", got)
	}
}
