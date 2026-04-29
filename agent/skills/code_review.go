package skills

import (
	"github.com/lukemuz/gocode/agent"
	"github.com/lukemuz/gocode/agent/tools/workspace"
)

// CodeReviewConfig configures the CodeReview skill.
type CodeReviewConfig struct {
	// Root is the repository or directory to review. Required.
	Root string

	// MaxFileBytes caps how many bytes read_file will return per call.
	// 0 uses the workspace default (1 MiB).
	MaxFileBytes int64

	// MaxResults caps search and find results per call.
	// 0 uses the workspace default (100).
	MaxResults int
}

// CodeReview is a skill for reviewing source code for correctness, security,
// style, and idiomatic quality. It provides workspace read tools and
// instructions that guide the model through a structured review process.
type CodeReview struct {
	toolset agent.Toolset
}

// NewCodeReview creates a CodeReview skill rooted at cfg.Root.
// Returns an error if cfg.Root is empty or unresolvable.
func NewCodeReview(cfg CodeReviewConfig) (*CodeReview, error) {
	ws, err := workspace.NewReadOnly(workspace.Config{
		Root:         cfg.Root,
		MaxFileBytes: cfg.MaxFileBytes,
		MaxResults:   cfg.MaxResults,
	})
	if err != nil {
		return nil, err
	}
	return &CodeReview{toolset: ws.Toolset()}, nil
}

func (s *CodeReview) Meta() SkillMeta {
	return SkillMeta{
		Name:        "code_review",
		Description: "Reviews source code for correctness, security, style, and idiomatic quality.",
		Version:     "1.0.0",
	}
}

func (s *CodeReview) Instructions() string {
	return `You are a thorough code reviewer. Use the provided workspace tools to read and analyze the code before commenting on it.

For each file or change under review, check for:
- Correctness: logic errors, off-by-one mistakes, incorrect assumptions about input, unhandled edge cases.
- Security: injection vulnerabilities, improper input validation, use of insecure APIs, exposed secrets, directory traversal, or other OWASP Top 10 concerns.
- Error handling: errors silently swallowed, missing propagation, inconsistent sentinel values.
- Performance: unnecessary allocations, unbounded loops, N+1 patterns, or other obvious hotspots.
- Clarity: unclear naming, overly complex logic that could be simplified, missing context for non-obvious behaviour.
- Idioms: code that goes against the conventions of the language or project.

Structure your feedback clearly:
- Distinguish blocking issues (must fix) from suggestions (nice to have).
- Cite specific file paths and line numbers for every finding.
- Be concise. One clear sentence per finding is better than a paragraph.
- If you need to read additional context before commenting, use the tools to do so.`
}

func (s *CodeReview) Toolset() agent.Toolset { return s.toolset }

func (s *CodeReview) Examples() []agent.Message { return nil }
