package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loadProjectMemory assembles the project-memory block that is appended to
// the agent's system prompt. It looks for, in order:
//
//   1. <workspace>/AGENTS.md           (vendor-neutral convention; used by
//                                       Codex, Cursor, Aider, etc.)
//   2. <workspace>/CLAUDE.md           (Claude Code's flavour — read for
//                                       compatibility with existing repos)
//   3. ~/.config/gocode/AGENTS.md      (user-level, gocode-specific)
//   4. ~/.claude/CLAUDE.md             (user-level, picked up so Claude
//                                       Code users can share their memory)
//
// Files that don't exist are skipped silently. The combined result is
// rendered with clearly delimited section headers so the model can tell
// the sources apart, and is intended to be marked as a cache breakpoint
// — it's stable per session and forms part of the cacheable prefix.
func loadProjectMemory(workspaceDir string) string {
	type src struct {
		label string
		path  string
	}
	home, _ := os.UserHomeDir()
	candidates := []src{
		{"project AGENTS.md", filepath.Join(workspaceDir, "AGENTS.md")},
		{"project CLAUDE.md", filepath.Join(workspaceDir, "CLAUDE.md")},
	}
	if home != "" {
		candidates = append(candidates,
			src{"user ~/.config/gocode/AGENTS.md", filepath.Join(home, ".config", "gocode", "AGENTS.md")},
			src{"user ~/.claude/CLAUDE.md", filepath.Join(home, ".claude", "CLAUDE.md")},
		)
	}

	var b strings.Builder
	for _, c := range candidates {
		data, err := os.ReadFile(c.path)
		if err != nil {
			continue
		}
		body := strings.TrimSpace(string(data))
		if body == "" {
			continue
		}
		fmt.Fprintf(&b, "\n\n--- %s ---\n%s", c.label, body)
	}
	return strings.TrimSpace(b.String())
}
