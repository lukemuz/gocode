// Command research runs a deep-research agent over a single question.
//
// Pipeline: planner -> parallel workers (Brave MCP search) -> synthesizer.
//
// Setup:
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	export BRAVE_API_KEY=BSA...
//
// Run:
//
//	go run ./cmd/research "What are the practical differences between QUIC and HTTP/2?"
//
// Flags:
//
//	-max-subtasks N      cap subtasks the planner emits (default 5)
//	-concurrency N       cap workers in flight (default 3)
//	-worker-iter N       cap tool-use iterations per worker (default 12)
//	-planner-model M     model for planner (default sonnet)
//	-worker-model M      model for workers (default haiku)
//	-synth-model M       model for synthesizer (default sonnet)
//	-out FILE            write the report body to FILE (default stdout)
//	-json                emit the full Report as JSON instead of just the body
//	-quiet               suppress progress logs on stderr
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/lukemuz/gocode/agent"
	"github.com/lukemuz/gocode/agent/mcp"
	"github.com/lukemuz/gocode/research"
)

func main() {
	maxSubtasks := flag.Int("max-subtasks", 5, "maximum sub-questions the planner can emit")
	concurrency := flag.Int("concurrency", 3, "maximum workers in flight")
	workerIter := flag.Int("worker-iter", 12, "max tool-use iterations per worker")
	plannerModel := flag.String("planner-model", agent.ModelSonnet, "model for planner")
	workerModel := flag.String("worker-model", agent.ModelHaiku, "model for workers")
	synthModel := flag.String("synth-model", agent.ModelSonnet, "model for synthesizer")
	outFile := flag.String("out", "", "write report to FILE (default stdout)")
	asJSON := flag.Bool("json", false, "emit full Report JSON instead of just the body")
	quiet := flag.Bool("quiet", false, "suppress progress logs")
	flag.Parse()

	question := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if question == "" {
		fmt.Fprintln(os.Stderr, "usage: research [flags] \"your question\"")
		flag.PrintDefaults()
		os.Exit(2)
	}

	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		fatal("ANTHROPIC_API_KEY is not set")
	}
	if os.Getenv("BRAVE_API_KEY") == "" {
		fatal("BRAVE_API_KEY is not set (required for Brave Search MCP)")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Connect Brave Search MCP. The npm package exposes brave_web_search and
	// brave_local_search tools; we surface them all to the worker.
	logf(*quiet, "connecting to Brave Search MCP server (npx -y @modelcontextprotocol/server-brave-search)\n")
	srv, err := mcp.Connect(ctx, mcp.Config{
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-brave-search"},
		Env:     []string{"BRAVE_API_KEY=" + os.Getenv("BRAVE_API_KEY")},
	})
	if err != nil {
		fatal("connect Brave MCP: " + err.Error())
	}
	defer srv.Close()

	searchTools, err := srv.Toolset(ctx)
	if err != nil {
		fatal("list MCP tools: " + err.Error())
	}
	// Defensive: cap each tool result so a verbose search reply can't blow
	// the worker's context window.
	searchTools = searchTools.Wrap(agent.WithResultLimit(8000))

	logf(*quiet, "Brave search tools: %s\n", strings.Join(toolNames(searchTools), ", "))

	provider, err := agent.NewAnthropicProviderFromEnv()
	if err != nil {
		fatal(err.Error())
	}
	mkClient := func(model string) *agent.Client {
		c, err := agent.New(agent.Config{Provider: provider, Model: model, MaxTokens: 4096})
		if err != nil {
			fatal(err.Error())
		}
		return c
	}

	cfg := research.Config{
		Planner:        mkClient(*plannerModel),
		Worker:         mkClient(*workerModel),
		Synthesizer:    mkClient(*synthModel),
		SearchTools:    searchTools,
		MaxSubtasks:    *maxSubtasks,
		MaxConcurrency: *concurrency,
		WorkerMaxIter:  *workerIter,
		Recorder:       newCLIRecorder(*quiet),
	}

	logf(*quiet, "question: %s\n", question)
	report, err := research.Run(ctx, cfg, question)
	if err != nil {
		fatal(err.Error())
	}

	logf(*quiet, "\ntokens used: %d in / %d out\n",
		report.Usage.InputTokens, report.Usage.OutputTokens)

	out := os.Stdout
	if *outFile != "" {
		f, err := os.Create(*outFile)
		if err != nil {
			fatal("create output: " + err.Error())
		}
		defer f.Close()
		out = f
	}

	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		fmt.Fprintln(out, report.Body)
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "research: "+msg)
	os.Exit(1)
}

func logf(quiet bool, format string, args ...any) {
	if quiet {
		return
	}
	fmt.Fprintf(os.Stderr, format, args...)
}

func toolNames(t agent.Toolset) []string {
	out := make([]string, 0, len(t.Bindings))
	for _, b := range t.Bindings {
		out = append(out, b.Tool.Name)
	}
	return out
}

// cliRecorder prints progress to stderr. It is concurrency-safe.
type cliRecorder struct {
	quiet bool
	mu    sync.Mutex
}

func newCLIRecorder(quiet bool) *cliRecorder { return &cliRecorder{quiet: quiet} }

func (r *cliRecorder) OnPlan(p research.Plan) {
	if r.quiet {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(os.Stderr, "plan: %d sub-questions\n", len(p.Subtasks))
	for _, s := range p.Subtasks {
		fmt.Fprintf(os.Stderr, "  [%s] %s\n", s.ID, s.Question)
	}
}

func (r *cliRecorder) OnWorkerStart(s research.Subtask) {
	if r.quiet {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(os.Stderr, "  -> [%s] start\n", s.ID)
}

func (r *cliRecorder) OnWorkerDone(n research.Note, err error) {
	if r.quiet {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  <- [%s] error: %v\n", n.SubtaskID, err)
		return
	}
	fmt.Fprintf(os.Stderr, "  <- [%s] done (%d citations)\n", n.SubtaskID, len(n.Citations))
}

func (r *cliRecorder) OnSynthesize() {
	if r.quiet {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintln(os.Stderr, "synthesizing...")
}
