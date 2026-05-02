// gocode is a CLI coding agent built on the gocode toolkit.
//
// Topology (Phase 2):
//
//	main agent           (Sonnet by default — configurable via -model)
//	  ├── direct tools   workspace read-only + trained bash + trained editor
//	  │                  + todo + clock + batch
//	  ├── explore        subagent on Haiku (-explore-model) — workspace
//	  │                  read-only + restricted bash + batch + clock.
//	  │                  Used for cheap, parallelisable inspection; its
//	  │                  iteration history never enters the main context.
//	  └── plan           subagent on Opus (-plan-model) — read-only tools,
//	                     no shell or edits. Used when the main agent wants
//	                     a stronger reasoner for design or hard debugging.
//
// The batch tool is also offered to the main agent so it can run several
// reads/searches concurrently in a single turn without paying for a
// subagent loop.
//
// Usage:
//
//	export OPENROUTER_API_KEY=sk-or-...
//	cd ~/your-project && gocode
//
// The agent is sandboxed to the current working directory by default.
// Pass -dir to operate on a different directory.
//
// Models default to Anthropic Claude routes on OpenRouter
// (anthropic/claude-sonnet-4.6, anthropic/claude-haiku-4.5,
// anthropic/claude-opus-4.7). Override with -model / -explore-model /
// -plan-model to use any OpenRouter-supported model id.
//
// Flags:
//
//	-dir            working directory the agent is sandboxed to (default cwd)
//	-model          main-agent model id (default Sonnet)
//	-explore-model  model used for the explore subagent (default Haiku)
//	-plan-model     model used for the plan subagent (default Opus)
//	-no-subagents   disable the explore and plan subagent tools
//	-bash           bash safety mode: restricted | standard | unrestricted
//	-yes            auto-approve every confirmation prompt
//	-max-iter       max model calls per turn (default 30)
//
// REPL commands:
//
//	:exit / :quit          leave
//	:reset                 clear conversation history
//	:tokens                print accumulated token usage
//	:help                  show this list
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lukemuz/gocode"
	"github.com/lukemuz/gocode/providers/openrouter"
	"github.com/lukemuz/gocode/tools/bash"
	"github.com/lukemuz/gocode/tools/batch"
	"github.com/lukemuz/gocode/tools/clock"
	"github.com/lukemuz/gocode/tools/editor"
	"github.com/lukemuz/gocode/tools/subagent"
	"github.com/lukemuz/gocode/tools/todo"
	"github.com/lukemuz/gocode/tools/web"
	"github.com/lukemuz/gocode/tools/workspace"
)

const mainSystemPrompt = `You are gocode, a fast and economical CLI coding assistant built on the gocode toolkit.

You operate inside a workspace directory. Available tools:
- list_directory, Glob, Grep, read_file, file_info: read-only filesystem inspection
- str_replace_based_edit_tool: view/create/str_replace/insert against files
- bash: run shell commands (safety policy varies by configuration)
- todo_write, todo_read: maintain a short planning checklist for multi-step work
- batch: run 2+ independent read-only calls in one turn. Default for independent reads/greps/inspections; skip only when a later call depends on an earlier result.
- web_fetch (when available): download an http(s) URL and return its content as text. HTML is converted to a plain-text approximation; long pages paginate via max_length + start_index. Use this for documentation lookups and inspecting URLs from error messages.
- explore (when available): bounded research specialist on a fast, cheap model — repo inspection or well-scoped Q&A (provided context, fetched docs, or its own knowledge); returns a concise summary with file dumps and fetches kept out of your context
- plan (when available): design and architecture specialist on a stronger model — get a structured plan before non-trivial multi-file edits, interface changes, or when stuck on a hypothesis
- now: current time

Operating principles:
1. Default to the explore subagent for bounded research — repo inspection (understand a module, find all usages, audit a pattern) and well-scoped Q&A (look up a stdlib function, summarise an RFC, answer a factual question with provided context). It's cheap, its file dumps and fetches stay out of your context, and you receive only its summary.
2. For tight, surgical lookups (one file, one symbol), call read_file or Grep directly.
3. Default to batch for independent read-only work. Each tool call is a full LLM round trip, so if you'd otherwise issue 2+ reads/greps/inspections that don't depend on each other, batch them. Issue solo calls only when a later call's input depends on an earlier call's output (e.g. grep first, then read only the files it returned).
4. Call plan as a routine first step for substantive design work — non-trivial multi-file changes, interface or data-shape changes, decisions with multiple plausible tradeoffs, or stuck debugging. Pass the question with the context you've gathered. Skip plan for routine single-file changes or when your approach is already clear.
5. For multi-step tasks, call todo_write at the start and update it as you go. Keep at most one item in_progress.
6. Be concise in chat. State what you're doing in one short sentence before tool calls; don't narrate every step.
7. After making edits, verify your work with appropriate checks (build, type-check, run affected tests via bash) before declaring success. Don't trust an edit you haven't checked.
8. When you change files, summarize the diff in one or two lines after.`

const exploreSystemPrompt = `You are gocode's explore specialist — a fast, focused researcher.

You answer self-contained, well-bounded questions and return concise, factual summaries. Two common shapes:
- Repo research: inspect the codebase to find callers, audit a pattern, summarise a module, locate references, etc.
- Bounded Q&A: answer a specific question using context the orchestrator provides, web docs you fetch, or your own knowledge — whichever fits.

You have read-only filesystem tools, restricted bash for read-only commands, web_fetch (when available) for documentation and external references, and a batch tool to fan out independent calls.

Operating principles:
1. Plan briefly, then execute. Default to batch for independent reads/greps/fetches — each tool call is a full LLM round trip, so issue solo calls only when later input depends on earlier output.
2. Cite specific files and line numbers (or URLs) in your findings.
3. Do NOT speculate about anything you have not directly verified. If your own knowledge is the source, say so explicitly so the orchestrator can judge confidence.
4. Keep your final summary tight — it's the only thing the orchestrator sees. Aim for the smallest answer that fully resolves the task.
5. Do not edit files. You have no write access. Refuse if asked.`

const planSystemPrompt = `You are gocode's plan specialist — a careful reasoner backed by a strong model.

You receive a design, implementation-planning, or debugging question along with relevant context the orchestrator has gathered. You have read-only filesystem tools to verify specifics, but no shell and no edits.

Operating principles:
1. Think carefully. Cover trade-offs, edge cases, and likely failure modes.
2. Verify with read_file or Grep rather than guessing when a fact is in doubt.
3. Return a structured plan: numbered steps, files to touch, risks. Keep it implementable, not aspirational.
4. Be honest about what you don't know.`

func main() {
	dir := flag.String("dir", ".", "working directory the agent is sandboxed to (defaults to the current directory)")
	model := flag.String("model", envOr("GOCODE_MODEL", "anthropic/claude-sonnet-4.6"), "main-agent model id (any OpenRouter slug; env: GOCODE_MODEL)")
	exploreModel := flag.String("explore-model", envOr("GOCODE_EXPLORE_MODEL", "anthropic/claude-haiku-4.5"), "model id for the explore subagent (env: GOCODE_EXPLORE_MODEL)")
	planModel := flag.String("plan-model", envOr("GOCODE_PLAN_MODEL", "anthropic/claude-opus-4.7"), "model id for the plan subagent (env: GOCODE_PLAN_MODEL)")
	noSubagents := flag.Bool("no-subagents", false, "disable explore and plan subagent tools")
	noFetch := flag.Bool("no-fetch", false, "disable the native web_fetch tool")
	bashMode := flag.String("bash", "restricted", "bash safety mode: restricted | standard | unrestricted")
	autoYes := flag.Bool("yes", false, "auto-approve every confirmation prompt")
	maxIter := flag.Int("max-iter", 30, "max model calls per turn")
	logPath := flag.String("log", "", "JSONL session log path. Pass `auto` to write under ~/.config/gocode/sessions/")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	mode, err := parseBashMode(*bashMode)
	if err != nil {
		log.Fatal(err)
	}

	provider, err := openrouter.NewProviderFromEnv()
	if err != nil {
		log.Fatalf("openrouter provider: %v", err)
	}

	mainClient := mustClient(provider, *model)

	// Optional JSONL session log. The recorder is attached to mainClient
	// before any WithModel-derived clients (summarizer, explore, plan) are
	// created — WithModel preserves the recorder, so all loops in the
	// session log to the same file.
	var logFile *os.File
	resolvedLog := ""
	if *logPath != "" {
		path, err := resolveLogPath(*logPath)
		if err != nil {
			log.Fatalf("log path: %v", err)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Fatalf("open log: %v", err)
		}
		logFile = f
		resolvedLog = path
		mainClient = mainClient.WithRecorder(gocode.NewJSONLRecorder(f))
	}
	defer func() {
		if logFile != nil {
			_ = logFile.Close()
		}
	}()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	confirm := makeConfirmer(*autoYes)

	// --- shared building blocks --------------------------------------------

	ws, err := workspace.NewReadOnly(workspace.Config{Root: *dir})
	if err != nil {
		log.Fatal(err)
	}
	clk := clock.New()

	// Read-only middleware stack: timeout, output cap, logging. No
	// confirmation needed; these tools cannot mutate state.
	roMiddleware := []gocode.Middleware{
		gocode.WithTimeout(60 * time.Second),
		gocode.WithResultLimit(64 * 1024),
		gocode.WithLogging(logger),
	}
	roTools := gocode.MustJoin(ws.Toolset(), clk.Toolset()).Wrap(roMiddleware...)

	// Restricted bash for subagents: read-only commands only, no
	// confirmation needed.
	subBashTool, err := bash.New(bash.Config{Root: *dir, Mode: bash.ModeRestricted})
	if err != nil {
		log.Fatal(err)
	}
	subBashToolset := subBashTool.Toolset().Wrap(roMiddleware...)

	// Web tools (web_fetch) are constructed up here so subagents can
	// include them and so batch can fan out concurrent fetches. Empty
	// when --no-fetch is set; an empty toolset contributes no bindings.
	var webTools gocode.Toolset
	if !*noFetch {
		webTools = web.New(web.Config{}).Toolset().Wrap(
			gocode.WithTimeout(30*time.Second),
			gocode.WithResultLimit(64*1024),
			gocode.WithLogging(logger),
		)
	}

	// Batch tool for read-only fan-out. Built from already-wrapped read-only
	// bindings so each sub-call inherits the timeout/limit/logging stack.
	roBatchBinding := batch.New(batch.Config{
		Bindings:    append(append(append([]gocode.ToolBinding{}, roTools.Bindings...), subBashToolset.Bindings...), webTools.Bindings...),
		MaxParallel: 8,
	})

	// --- subagent tools ----------------------------------------------------

	var subagentBindings []gocode.ToolBinding
	if !*noSubagents {
		exploreClient := mainClient.WithModel(*exploreModel)
		exploreTools := gocode.MustJoin(roTools, subBashToolset, webTools, gocode.Tools(roBatchBinding)).
			CacheLast(gocode.Ephemeral())
		exploreBinding, err := subagent.New(subagent.Config{
			Name:        "explore",
			Description: "Delegate a bounded research task to a fast, cheap specialist. Two main shapes: (a) repo research — find callers, audit a pattern, summarise a module, locate references; (b) bounded Q&A — answer a well-scoped question from provided context, fetched docs, or general knowledge (e.g. 'what does this stdlib function do', 'summarise this RFC's caching rules'). The specialist has read-only filesystem tools, restricted bash, web_fetch, and batch fan-out; it returns a concise summary and its iteration history stays out of your context. Pass a self-contained task description with any context the specialist needs. Default for any task that would otherwise have you read 3+ files or research a well-scoped question yourself.",
			Client:      exploreClient,
			System:      exploreSystemPrompt,
			Tools:       exploreTools,
			MaxIter:     50,
		})
		if err != nil {
			log.Fatal(err)
		}

		planClient := mainClient.WithModel(*planModel)
		planBinding, err := subagent.New(subagent.Config{
			Name:        "plan",
			Description: "Delegate design and architecture work to a stronger reasoning model (better at multi-step architectural reasoning and invariant tracking across components). Get a structured plan before editing when work touches 3+ files, changes a public interface or shared data shape, has multiple plausible tradeoffs, or you've spent 2+ debugging turns without a clear hypothesis. Pass the question PLUS context you've gathered (file excerpts, error messages, prior attempts). Returns a numbered plan with files to touch and risks. Skip for routine single-file changes or when your approach is already clear.",
			Client:      planClient,
			System:      planSystemPrompt,
			Tools:       roTools.CacheLast(gocode.Ephemeral()),
			MaxIter:     25,
		})
		if err != nil {
			log.Fatal(err)
		}
		subagentBindings = []gocode.ToolBinding{exploreBinding, planBinding}
	}

	// --- main-agent edit tools ---------------------------------------------

	mainBashTool, err := bash.New(bash.Config{Root: *dir, Mode: mode})
	if err != nil {
		log.Fatal(err)
	}
	mainBashBindings := mainBashTool.Toolset().Bindings

	ed, err := editor.New(editor.Config{Root: *dir})
	if err != nil {
		log.Fatal(err)
	}
	editorBindings := ed.Toolset().Bindings

	editTools := gocode.Tools(append(mainBashBindings, editorBindings...)...).Wrap(
		gocode.WithConfirmation(confirm),
		gocode.WithTimeout(60*time.Second),
		gocode.WithResultLimit(64*1024),
		gocode.WithLogging(logger),
	)

	// --- main agent assembly ----------------------------------------------

	mainTools := gocode.MustJoin(
		roTools,
		gocode.Tools(roBatchBinding),
		editTools,
		todo.New().Toolset(),
		webTools,
		gocode.Tools(subagentBindings...),
	).CacheLast(gocode.Ephemeral()) // cache the entire tool block — stable per session

	memory := loadProjectMemory(*dir)
	system := mainSystemPrompt
	if memory != "" {
		system += "\n\n## Project memory\n\n" + memory
	}

	agent := gocode.Agent{
		Client: mainClient,
		System: system,
		Tools:  mainTools,
		Context: gocode.ContextManager{
			MaxTokens:  120_000,
			KeepFirst:  1,
			KeepRecent: 30,
		},
		MaxIter: *maxIter,
	}

	// Summarizer for /compact runs on Haiku — cheap and plenty capable
	// for transcript summarization. Independent of the user's main model.
	summarizer := mainClient.WithModel(envOr("GOCODE_SUMMARIZE_MODEL", "anthropic/claude-haiku-4.5"))

	// --- run ---------------------------------------------------------------

	abs, _ := absDir(*dir)
	subStatus := green("on")
	if *noSubagents {
		subStatus = dim("off")
	}
	row := func(label, value string) {
		fmt.Fprintf(os.Stderr, "  %s %s\n", grey(padRight(label, 10)), value)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  %s %s\n", boldCyan("▍gocode"), grey("— a fast, economical CLI coding agent"))
	fmt.Fprintln(os.Stderr)
	row("model", bold(*model))
	row("bash", *bashMode)
	row("subagents", subStatus)
	if !*noSubagents {
		row("explore", *exploreModel)
		row("plan", *planModel)
	}
	row("dir", abs)
	if resolvedLog != "" {
		row("log", resolvedLog)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, dim("  type a request, or /help for commands. ctrl-c to interrupt, ctrl-d to exit."))

	s := &session{
		agent:      agent,
		summarizer: summarizer,
		provider:   provider,
		memory:     memory,
		logPath:    resolvedLog,
	}
	s.repl(ctx)
}

// resolveLogPath turns a user-supplied -log value into an absolute path.
// "auto" expands to ~/.config/gocode/sessions/<timestamp>.jsonl, creating
// the directory if needed. Any other value is treated as a literal path.
func resolveLogPath(spec string) (string, error) {
	if spec != "auto" {
		return filepath.Abs(spec)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "gocode", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := time.Now().Format("2006-01-02T15-04-05")
	return filepath.Join(dir, stamp+".jsonl"), nil
}

func mustClient(provider gocode.Provider, model string) *gocode.Client {
	c, err := gocode.New(gocode.Config{
		Provider:  provider,
		Model:     model,
		MaxTokens: 8192,
		Retry: gocode.RetryConfig{
			MaxRetries:  3,
			InitialWait: time.Second,
			MaxWait:     10 * time.Second,
		},
		SystemCache: &gocode.CacheControl{Type: "ephemeral"},
	})
	if err != nil {
		log.Fatal(err)
	}
	return c
}

func makeConfirmer(autoYes bool) func(ctx context.Context, b gocode.ToolBinding, input json.RawMessage) (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	return func(ctx context.Context, b gocode.ToolBinding, input json.RawMessage) (bool, error) {
		if !b.Meta.RequiresConfirmation || autoYes {
			return true, nil
		}
		fmt.Fprintf(os.Stderr, "\n[approve %s]\n", b.Tool.Name)
		if compact := compactJSON(input); compact != "" {
			fmt.Fprintf(os.Stderr, "  input: %s\n", compact)
		}
		fmt.Fprint(os.Stderr, "  approve? [y/N] ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return false, nil
		}
		ans := strings.TrimSpace(strings.ToLower(line))
		return ans == "y" || ans == "yes", nil
	}
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	s := string(out)
	if len(s) > 400 {
		s = s[:400] + "..."
	}
	return s
}

// envOr returns the value of env var name, or fallback if the variable
// is unset or empty. Used to give CLI flags env-var defaults.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func parseBashMode(s string) (bash.Mode, error) {
	switch strings.ToLower(s) {
	case "restricted", "":
		return bash.ModeRestricted, nil
	case "standard":
		return bash.ModeStandard, nil
	case "unrestricted":
		return bash.ModeUnrestricted, nil
	}
	return 0, fmt.Errorf("unknown bash mode %q (want: restricted | standard | unrestricted)", s)
}

func absDir(dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	return filepath.Abs(dir)
}
