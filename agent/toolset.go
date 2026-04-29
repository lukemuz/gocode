package agent

import "fmt"

// ToolBinding pairs a Tool definition with its ToolFunc implementation
// and optional advisory metadata. It is the unit of composition for Toolset.
type ToolBinding struct {
	Tool Tool
	Func ToolFunc
	Meta ToolMetadata
}

// ToolMetadata carries advisory safety annotations about a bound tool.
// Fields are informational only; the library does not enforce any policy
// based on them. Applications may inspect them to build confirmation
// wrappers, audit logs, or permission checks.
type ToolMetadata struct {
	Source               string
	ReadOnly             bool
	Destructive          bool
	Network              bool
	Filesystem           bool
	Shell                bool
	RequiresConfirmation bool
	SafetyNotes          []string
}

// Toolset is an ordered collection of ToolBindings. It produces the
// []Tool and map[string]ToolFunc slices that Client.Loop and
// Client.LoopStream expect.
type Toolset struct {
	Bindings []ToolBinding
}

// Tools returns the Tool slice for passing to Client.Loop or Client.LoopStream.
func (t Toolset) Tools() []Tool {
	tools := make([]Tool, len(t.Bindings))
	for i, b := range t.Bindings {
		tools[i] = b.Tool
	}
	return tools
}

// Dispatch returns the dispatch map for passing to Client.Loop or Client.LoopStream.
func (t Toolset) Dispatch() map[string]ToolFunc {
	m := make(map[string]ToolFunc, len(t.Bindings))
	for _, b := range t.Bindings {
		m[b.Tool.Name] = b.Func
	}
	return m
}

// Join merges multiple Toolset values into one, preserving order.
// Returns an error if any tool name appears more than once across the sets.
func Join(sets ...Toolset) (Toolset, error) {
	seen := make(map[string]bool)
	var result Toolset
	for _, s := range sets {
		for _, b := range s.Bindings {
			if seen[b.Tool.Name] {
				return Toolset{}, fmt.Errorf("agent: duplicate tool name %q in toolset", b.Tool.Name)
			}
			seen[b.Tool.Name] = true
			result.Bindings = append(result.Bindings, b)
		}
	}
	return result, nil
}
