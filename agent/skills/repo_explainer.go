package skills

import (
	"github.com/lukemuz/gocode/agent"
	"github.com/lukemuz/gocode/agent/tools/workspace"
)

// RepoExplainerConfig configures the RepoExplainer skill.
type RepoExplainerConfig struct {
	// Root is the repository directory to explore. Required.
	Root string

	// MaxFileBytes caps how many bytes read_file will return per call.
	// 0 uses the workspace default (1 MiB).
	MaxFileBytes int64

	// MaxResults caps the number of entries returned by find_files and
	// search_text. 0 uses the workspace default (100).
	MaxResults int
}

// RepoExplainer is a skill for exploring and explaining code repositories.
// It provides workspace read tools paired with instructions that direct the
// model to ground its answers in actual file contents.
type RepoExplainer struct {
	toolset agent.Toolset
}

// NewRepoExplainer creates a RepoExplainer for the directory at cfg.Root.
// Returns an error if cfg.Root is empty or unresolvable.
func NewRepoExplainer(cfg RepoExplainerConfig) (*RepoExplainer, error) {
	ws, err := workspace.NewReadOnly(workspace.Config{
		Root:         cfg.Root,
		MaxFileBytes: cfg.MaxFileBytes,
		MaxResults:   cfg.MaxResults,
	})
	if err != nil {
		return nil, err
	}
	return &RepoExplainer{toolset: ws.Toolset()}, nil
}

func (s *RepoExplainer) Meta() SkillMeta {
	return SkillMeta{
		Name:        "repo_explainer",
		Description: "Explores and explains a code repository by reading its files.",
		Version:     "1.0.0",
	}
}

func (s *RepoExplainer) Instructions() string {
	return `You are a code repository expert. Use the provided workspace tools to explore and explain this codebase accurately.

When asked about the repository:
1. Read key entry points first — README, go.mod, package.json, Cargo.toml, or similar manifest files — to understand the project's purpose and structure.
2. Explore the directory tree and source files to give well-grounded answers.
3. Always cite specific file paths when referring to code. Use line numbers when referencing a particular function, type, or statement.
4. If you are uncertain about something, use the tools to verify before answering.
5. Prefer reading a small relevant section over speculating about what a file might contain.`
}

func (s *RepoExplainer) Toolset() agent.Toolset { return s.toolset }

func (s *RepoExplainer) Examples() []agent.Message { return nil }
