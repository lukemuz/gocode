package agent

import "strings"

// JoinInstructions joins system-prompt fragments with a blank line between each
// non-empty part. Empty or whitespace-only fragments are silently dropped.
// This is the idiomatic way to combine a base system prompt with the
// instructions returned by a skill.
func JoinInstructions(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			keep = append(keep, t)
		}
	}
	return strings.Join(keep, "\n\n")
}
