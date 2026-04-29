package skills

import (
	"github.com/lukemuz/gocode/agent"
	"github.com/lukemuz/gocode/agent/tools/workspace"
)

// LogSummarizerConfig configures the LogSummarizer skill.
type LogSummarizerConfig struct {
	// Root is the directory containing the log files to analyze. Required.
	Root string

	// MaxFileBytes caps how many bytes read_file will return per call.
	// 0 uses the workspace default (1 MiB).
	MaxFileBytes int64

	// MaxResults caps the number of search matches returned by search_text
	// and find_files. 0 uses the workspace default (100).
	MaxResults int
}

// LogSummarizer is a skill for reading and summarizing log files.
// It provides workspace read tools focused on log discovery and content
// extraction, paired with instructions that direct the model toward
// actionable, pattern-oriented summaries.
type LogSummarizer struct {
	toolset agent.Toolset
}

// NewLogSummarizer creates a LogSummarizer rooted at cfg.Root.
// Returns an error if cfg.Root is empty or unresolvable.
func NewLogSummarizer(cfg LogSummarizerConfig) (*LogSummarizer, error) {
	ws, err := workspace.NewReadOnly(workspace.Config{
		Root:         cfg.Root,
		MaxFileBytes: cfg.MaxFileBytes,
		MaxResults:   cfg.MaxResults,
	})
	if err != nil {
		return nil, err
	}
	return &LogSummarizer{toolset: ws.Toolset()}, nil
}

func (s *LogSummarizer) Meta() SkillMeta {
	return SkillMeta{
		Name:        "log_summarizer",
		Description: "Reads and summarizes log files, highlighting errors, patterns, and anomalies.",
		Version:     "1.0.0",
	}
}

func (s *LogSummarizer) Instructions() string {
	return `You are a log analysis expert. Use the provided workspace tools to read and analyze log files.

When summarizing logs:
1. Start by listing available log files to understand what is present.
2. Search for ERROR, WARN, FATAL, CRITICAL, or similar severity keywords to triage quickly.
3. Identify recurring patterns — group similar messages rather than repeating them individually.
4. Note the time range covered if timestamps are present.
5. Highlight anomalies: unexpected spikes, novel errors, or sequences that indicate a cascade failure.
6. Focus on what is actionable or interesting. Omit routine INFO noise unless it is relevant to a problem.
7. When quoting a log line, cite the file name and approximate line number.`
}

func (s *LogSummarizer) Toolset() agent.Toolset { return s.toolset }

func (s *LogSummarizer) Examples() []agent.Message { return nil }
