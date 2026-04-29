package research

import (
	"context"
	"fmt"

	"github.com/lukemuz/gocode/agent"
)

// Run executes the full pipeline: plan -> parallel workers -> synthesize.
//
// Worker errors are non-fatal: a failed worker becomes a Note with Err set,
// and the synthesizer is told. The whole call only fails if the planner
// itself fails or every worker fails.
func Run(ctx context.Context, cfg Config, question string) (Report, error) {
	if cfg.Planner == nil || cfg.Worker == nil || cfg.Synthesizer == nil {
		return Report{}, fmt.Errorf("research: Planner, Worker, and Synthesizer clients are required")
	}
	rec := cfg.Recorder
	if rec == nil {
		rec = NopRecorder{}
	}
	var totalUsage agent.Usage

	// 1. Plan.
	plan, plannerUsage, err := Decompose(ctx, cfg.Planner, question, cfg.MaxSubtasks)
	if err != nil {
		return Report{Question: question}, fmt.Errorf("research: plan: %w", err)
	}
	totalUsage = addUsage(totalUsage, plannerUsage)
	rec.OnPlan(plan)

	// 2. Parallel workers.
	notes, workerUsage := runWorkers(ctx, cfg, plan.Subtasks, rec)
	totalUsage = addUsage(totalUsage, workerUsage)

	// Bail only if every single worker failed with no usable summary.
	usable := 0
	for _, n := range notes {
		if n.Summary != "" {
			usable++
		}
	}
	if usable == 0 {
		return Report{Question: question, Notes: notes, Usage: totalUsage},
			fmt.Errorf("research: every worker failed; no notes to synthesize")
	}

	// 3. Synthesize.
	rec.OnSynthesize()
	body, synthUsage, err := Synthesize(ctx, cfg.Synthesizer, question, notes)
	if err != nil {
		return Report{Question: question, Notes: notes, Usage: totalUsage},
			fmt.Errorf("research: synthesize: %w", err)
	}
	totalUsage = addUsage(totalUsage, synthUsage)

	return Report{
		Question: question,
		Body:     body,
		Notes:    notes,
		Usage:    totalUsage,
	}, nil
}

// runWorkers fans out the subtasks. If MaxConcurrency > 0, a buffered
// semaphore caps in-flight workers; otherwise agent.Parallel runs all at once.
func runWorkers(ctx context.Context, cfg Config, subtasks []Subtask, rec ProgressRecorder) ([]Note, agent.Usage) {
	type result struct {
		note  Note
		usage agent.Usage
	}

	var sem chan struct{}
	if cfg.MaxConcurrency > 0 {
		sem = make(chan struct{}, cfg.MaxConcurrency)
	}

	steps := make([]agent.StepFunc[result], len(subtasks))
	for i, st := range subtasks {
		st := st
		steps[i] = func(ctx context.Context) (result, error) {
			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					return result{note: Note{SubtaskID: st.ID, Question: st.Question, Err: ctx.Err().Error()}}, ctx.Err()
				}
			}
			rec.OnWorkerStart(st)
			note, usage, err := Investigate(ctx, cfg.Worker, st, cfg.SearchTools, cfg.WorkerMaxIter)
			rec.OnWorkerDone(note, err)
			return result{note: note, usage: usage}, nil // err already in note.Err
		}
	}

	results := agent.Parallel(ctx, steps...)
	notes := make([]Note, len(results))
	var total agent.Usage
	for i, r := range results {
		notes[i] = r.Value.note
		total = addUsage(total, r.Value.usage)
	}
	return notes, total
}

func addUsage(a, b agent.Usage) agent.Usage {
	return agent.Usage{
		InputTokens:  a.InputTokens + b.InputTokens,
		OutputTokens: a.OutputTokens + b.OutputTokens,
	}
}
