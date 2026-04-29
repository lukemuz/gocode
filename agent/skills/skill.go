// Package skills provides inspectable, composable bundles of instructions,
// tools, examples, and metadata for common LLM assistant tasks.
//
// A skill is not an autonomous agent and does not own a loop. Every skill
// exposes plain data and plain functions that the caller wires together:
//
//	skill, err := skills.NewRepoExplainer(skills.RepoExplainerConfig{Root: "."})
//	if err != nil { ... }
//
//	system := agent.JoinInstructions(baseSystem, skill.Instructions())
//	toolset, err := agent.Join(myTools, skill.Toolset())
//	if err != nil { ... }
//
//	result, err := client.Loop(ctx, system, history, toolset.Tools(), toolset.Dispatch(), 10)
//
// Callers may inspect, adapt, or ignore any part. Skills compose with local
// tools and MCP tools via agent.Join.
package skills

import "github.com/lukemuz/gocode/agent"

// Skill is an inspectable bundle of instructions, tools, examples, and
// metadata. Implementations are concrete structs; this interface lets callers
// compose skills generically.
type Skill interface {
	// Meta returns the skill's name, description, and version.
	Meta() SkillMeta
	// Instructions returns the system-prompt fragment for this skill.
	// Compose it with a base system prompt using agent.JoinInstructions.
	Instructions() string
	// Toolset returns the tools provided by this skill.
	// Compose it with other toolsets using agent.Join.
	Toolset() agent.Toolset
	// Examples returns optional few-shot conversation examples.
	// Prepend these to your history slice before the first user message to
	// give the model concrete demonstrations of the expected behaviour.
	// Returns nil when no examples are defined.
	Examples() []agent.Message
}

// SkillMeta describes a skill's identity.
type SkillMeta struct {
	Name        string
	Description string
	Version     string
}
