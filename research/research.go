// Package research implements a deep-research agent on top of gocode.
//
// The pipeline is:
//
//	question -> Plan (LLM)
//	         -> Parallel: Investigate(subtask) for each subtask (LLM + search tools)
//	         -> Synthesize (LLM)
//	         -> Report
//
// There is no graph runtime. The pipeline is three function calls and
// agent.Parallel. Sub-questions, notes, and the final report are plain Go
// structs the caller owns.
package research

import (
	"github.com/lukemuz/gocode/agent"
)

// Subtask is one focused sub-question the planner emits.
type Subtask struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Rationale string `json:"rationale,omitempty"`
}

// Plan is the output of the planner.
type Plan struct {
	Question  string    `json:"question"`
	Subtasks  []Subtask `json:"subtasks"`
	Reasoning string    `json:"reasoning,omitempty"`
}

// Citation references a source used by a worker.
type Citation struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// Note is one worker's findings for one subtask.
type Note struct {
	SubtaskID string     `json:"subtask_id"`
	Question  string     `json:"question"`
	Summary   string     `json:"summary"`
	Citations []Citation `json:"citations"`
	Err       string     `json:"error,omitempty"` // populated when the worker failed
}

// Report is the synthesized final answer.
type Report struct {
	Question string `json:"question"`
	Body     string `json:"body"`
	Notes    []Note `json:"notes"`
	Usage    agent.Usage `json:"usage"`
}

// Config wires the clients and tools used by Run.
//
// Planner/Worker/Synthesizer are separate clients so callers can mix models
// (e.g. Sonnet for planning + synthesis, Haiku for workers). The same client
// can be passed for all three.
type Config struct {
	Planner      *agent.Client
	Worker       *agent.Client
	Synthesizer  *agent.Client

	// SearchTools is the toolset workers use to find information. Typically an
	// MCP toolset (e.g. Brave Search) but any agent.Toolset works.
	SearchTools agent.Toolset

	// MaxSubtasks caps how many subtasks the planner is allowed to produce
	// (defense-in-depth; the planner is also told). 0 -> 6.
	MaxSubtasks int

	// MaxConcurrency limits how many workers run in parallel. 0 -> no limit.
	MaxConcurrency int

	// WorkerMaxIter caps tool-use iterations per worker. 0 -> 12.
	WorkerMaxIter int

	// Recorder, if non-nil, receives progress events from Run.
	Recorder ProgressRecorder
}

// ProgressRecorder receives lifecycle events. All methods may be called
// concurrently and must be safe for concurrent use.
type ProgressRecorder interface {
	OnPlan(plan Plan)
	OnWorkerStart(subtask Subtask)
	OnWorkerDone(note Note, err error)
	OnSynthesize()
}

// NopRecorder satisfies ProgressRecorder with no-ops.
type NopRecorder struct{}

func (NopRecorder) OnPlan(Plan)                 {}
func (NopRecorder) OnWorkerStart(Subtask)       {}
func (NopRecorder) OnWorkerDone(Note, error)    {}
func (NopRecorder) OnSynthesize()               {}
