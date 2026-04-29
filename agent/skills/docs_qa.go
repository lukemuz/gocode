package skills

import (
	"github.com/lukemuz/gocode/agent"
	"github.com/lukemuz/gocode/agent/tools/workspace"
)

// DocsQAConfig configures the DocsQA skill.
type DocsQAConfig struct {
	// Root is the directory containing documentation files. Required.
	Root string

	// MaxFileBytes caps how many bytes read_file will return per call.
	// 0 uses the workspace default (1 MiB).
	MaxFileBytes int64

	// MaxResults caps search and find results per call.
	// 0 uses the workspace default (100).
	MaxResults int
}

// DocsQA is a skill for answering questions from local documentation.
// It provides workspace read tools and instructions that keep the model
// grounded in the actual documentation rather than general knowledge.
type DocsQA struct {
	toolset agent.Toolset
}

// NewDocsQA creates a DocsQA skill rooted at cfg.Root.
// Returns an error if cfg.Root is empty or unresolvable.
func NewDocsQA(cfg DocsQAConfig) (*DocsQA, error) {
	ws, err := workspace.NewReadOnly(workspace.Config{
		Root:         cfg.Root,
		MaxFileBytes: cfg.MaxFileBytes,
		MaxResults:   cfg.MaxResults,
	})
	if err != nil {
		return nil, err
	}
	return &DocsQA{toolset: ws.Toolset()}, nil
}

func (s *DocsQA) Meta() SkillMeta {
	return SkillMeta{
		Name:        "docs_qa",
		Description: "Answers questions grounded in local documentation files.",
		Version:     "1.0.0",
	}
}

func (s *DocsQA) Instructions() string {
	return `You are a documentation assistant. Use the provided workspace tools to read and search the documentation before answering questions.

Guidelines:
1. Always search or read the documentation before answering. Do not rely on general knowledge when the answer should come from the docs.
2. When you cite information, reference the specific document name and, where possible, the section or line number.
3. If the documentation does not cover a question, say so clearly. Do not guess or supplement with information that is not in the docs.
4. If multiple documents are relevant, consult all of them before answering.
5. Prefer quoting or paraphrasing the exact documentation text over interpreting it loosely.
6. If you find conflicting information across documents, surface the conflict and note which document says what.`
}

func (s *DocsQA) Toolset() agent.Toolset { return s.toolset }

func (s *DocsQA) Examples() []agent.Message { return nil }
