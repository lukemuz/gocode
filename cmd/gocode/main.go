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
// Status: beta / experimental. Surface area is unstable.
//
// Usage:
//
//	# Run Claude Code's /login once on Linux/Windows; the CLI reuses
//	# ~/.claude/.credentials.json automatically.
//	# Or set a subscription OAuth token explicitly (e.g. on macOS):
//	export ANTHROPIC_AUTH_TOKEN=sk-ant-oat...
//	# Or fall back to pay-as-you-go API billing:
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run ./cmd/gocode -dir . -bash standard
//
// Flags:
//
//	-dir            working directory the agent is sandboxed to
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
	"errors"
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
	"github.com/lukemuz/gocode/providers/anthropic"
	"github.com/lukemuz/gocode/tools/bash"
	"github.com/lukemuz/gocode/tools/batch"
	"github.com/lukemuz/gocode/tools/clock"
	"github.com/lukemuz/gocode/tools/editor"
	"github.com/lukemuz/gocode/tools/subagent"
	"github.com/lukemuz/gocode/tools/todo"
	"github.com/lukemuz/gocode/tools/workspace"
)

const mainSystemPrompt = `You are gocode, a fast and economical CLI coding assistant built on the gocode toolkit.

You operate inside a workspace directory. Available tools:
- list_directory, Glob, Grep, read_file, file_info: read-only filesystem inspection
- str_replace_based_edit_tool: view/create/str_replace/insert against files (Anthropic's trained editor)
- bash: run shell commands (Anthropic's trained bash; safety policy varies by configuration)
- todo_write, todo_read: maintain a short planning checklist for multi-step work
- batch: run several read-only tool calls concurrently in one turn (great for fanning out greps and reads)
- web_search, web_fetch (when available): Anthropic-hosted tools for searching the web and fetching specific URLs (e.g. library docs)
- explore (when available): delegate inspection to a faster, cheaper specialist that returns a summary
- plan (when available): delegate hard reasoning or design questions to a stronger model
- now: current time

Operating principles:
1. For broad inspection (understand a module, find all usages, audit a pattern), prefer the explore subagent — it's cheaper and its file dumps stay out of your context. You receive only its summary.
2. For tight, surgical lookups (one file, one symbol), call read_file or Grep directly.
3. To fan out several independent reads or searches in one turn, use batch.
4. For genuinely hard reasoning (architecture, subtle bugs, debugging strategy), call plan and feed it the relevant context.
5. For multi-step tasks, call todo_write at the start and update it as you go. Keep at most one item in_progress.
6. Be concise in chat. State what you're doing in one short sentence before tool calls; don't narrate every step.
7. After making edits, verify your work with appropriate checks (build, type-check, run affected tests via bash) before declaring success. Don't trust an edit you haven't checked.
8. When you change files, summarize the diff in one or two lines after.`

const exploreSystemPrompt = `You are gocode's explore specialist — a fast, focused researcher.

You receive one self-contained task and return a concise, factual summary. You have read-only filesystem tools, restricted bash for read-only commands, and a batch tool to fan out several reads or searches at once.

Operating principles:
1. Plan briefly, then execute the inspection. Use batch to run independent reads/greps concurrently.
2. Cite specific files and line numbers in your findings.
3. Do NOT speculate about anything you have not directly verified.
4. Keep your final summary tight — it's the only thing the orchestrator sees. Aim for the smallest answer that fully resolves the task.
5. Do not edit files. You have no write access. Refuse if asked.`

const planSystemPrompt = `You are gocode's plan specialist — a careful reasoner backed by a strong model.

You receive a design or debugging question along with relevant context the orchestrator has gathered. You have read-only filesystem tools to verify specifics, but no shell and no edits.

Operating principles:
1. Think carefully. Cover trade-offs, edge cases, and likely failure modes.
2. Verify with read_file or Grep rather than guessing when a fact is in doubt.
3. Return a structured plan: numbered steps, files to touch, risks. Keep it implementable, not aspirational.
4. Be honest about what you don't know.`

func main() {
	dir := flag.String("dir", ".", "working directory the agent is sandboxed to")
	model := flag.String("model", gocode.ModelSonnet, "main-agent model id")
	exploreModel := flag.String("explore-model", gocode.ModelHaiku, "model id for the explore subagent")
	planModel := flag.String("plan-model", gocode.ModelOpus, "model id for the plan subagent")
	noSubagents := flag.Bool("no-subagents", false, "disable explore and plan subagent tools")
	noWeb := flag.Bool("no-web", false, "disable Anthropic-hosted web_search and web_fetch tools")
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

	provider, authLabel, err := resolveAnthropicProvider()
	if err != nil {
		log.Fatalf("anthropic provider: %v", err)
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
	subBashBinding := anthropic.BashTool(subBashTool.TrainedHandler())
	subBashToolset := gocode.Tools(subBashBinding).Wrap(roMiddleware...)

	// Batch tool for read-only fan-out. Built from already-wrapped read-only
	// bindings so each sub-call inherits the timeout/limit/logging stack.
	roBatchBinding := batch.New(batch.Config{
		Bindings:    append(append([]gocode.ToolBinding{}, roTools.Bindings...), subBashToolset.Bindings...),
		MaxParallel: 8,
	})

	// --- subagent tools ----------------------------------------------------

	var subagentBindings []gocode.ToolBinding
	if !*noSubagents {
		exploreClient := mainClient.WithModel(*exploreModel)
		exploreTools := gocode.MustJoin(roTools, subBashToolset, gocode.Tools(roBatchBinding)).
			CacheLast(gocode.Ephemeral())
		exploreBinding, err := subagent.New(subagent.Config{
			Name:        "explore",
			Description: "Delegate a focused inspection task to a fast, cheap specialist. Provide a self-contained task description (e.g. 'find every caller of FooBar in /internal and summarise their patterns'). The specialist has read-only filesystem tools, restricted bash, and batch fan-out. It returns a concise textual summary; its iteration history is discarded so it does not pollute your context. Use this whenever a task involves reading more than two or three files.",
			Client:      exploreClient,
			System:      exploreSystemPrompt,
			Tools:       exploreTools,
			MaxIter:     12,
		})
		if err != nil {
			log.Fatal(err)
		}

		planClient := mainClient.WithModel(*planModel)
		planBinding, err := subagent.New(subagent.Config{
			Name:        "plan",
			Description: "Delegate a hard reasoning task — architecture decision, subtle bug analysis, debugging strategy — to a stronger model. Pass the question PLUS the relevant context you have already gathered (file excerpts, error messages, prior attempts). The specialist returns a structured plan with numbered steps and risks. Use sparingly; it is expensive.",
			Client:      planClient,
			System:      planSystemPrompt,
			Tools:       roTools.CacheLast(gocode.Ephemeral()),
			MaxIter:     6,
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
	mainBashBinding := anthropic.BashTool(mainBashTool.TrainedHandler())
	mainBashBinding.Meta.RequiresConfirmation = mainBashTool.Toolset().Bindings[0].Meta.RequiresConfirmation

	ed, err := editor.New(editor.Config{Root: *dir})
	if err != nil {
		log.Fatal(err)
	}
	editorBinding := anthropic.TextEditor20250728(ed.Handler())

	editTools := gocode.Tools(mainBashBinding, editorBinding).Wrap(
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
		gocode.Tools(subagentBindings...),
	).CacheLast(gocode.Ephemeral()) // cache the entire tool block — stable per session

	// Anthropic's hosted web tools — server-executed by the API, no Go
	// handler needed. Useful for documentation lookups and reading
	// arbitrary URLs the model encounters in error messages or notes.
	if !*noWeb {
		mainTools = mainTools.WithProviderTools(
			anthropic.WebSearch(anthropic.WebSearchOpts{MaxUses: 5}),
			anthropic.WebFetch(anthropic.WebFetchOpts{MaxUses: 5, MaxContentTokens: 8000}),
		)
	}

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
	summarizer := mainClient.WithModel(gocode.ModelHaiku)

	// --- run ---------------------------------------------------------------

	abs, _ := absDir(*dir)
	subStatus := "on"
	if *noSubagents {
		subStatus = "off"
	}
	fmt.Fprintln(os.Stderr, "gocode (beta) — experimental CLI built on the gocode toolkit")
	fmt.Fprintf(os.Stderr, "  auth=%s  model=%s  bash=%s  subagents=%s  dir=%s\n", authLabel, *model, *bashMode, subStatus, abs)
	if !*noSubagents {
		fmt.Fprintf(os.Stderr, "  explore=%s  plan=%s\n", *exploreModel, *planModel)
	}
	if resolvedLog != "" {
		fmt.Fprintf(os.Stderr, "  log=%s\n", resolvedLog)
	}
	fmt.Fprintln(os.Stderr, "type a request, or /help for commands. ctrl-c to interrupt, ctrl-d to exit.")

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

// resolveAnthropicProvider builds an anthropic.Provider, picking
// credentials from (in order): ANTHROPIC_AUTH_TOKEN (OAuth bearer
// token, e.g. for macOS users who can't read the credentials file),
// ~/.claude/.credentials.json (the OAuth credentials Claude Code's
// /login writes on Linux/Windows), and finally ANTHROPIC_API_KEY
// (pay-as-you-go API). Subscription auth wins over API auth so users
// who have both set don't accidentally bill the API.
//
// Returns the provider plus a short human-readable label describing
// which source was used, for the startup banner.
func resolveAnthropicProvider() (*anthropic.Provider, string, error) {
	if tok := os.Getenv("ANTHROPIC_AUTH_TOKEN"); tok != "" {
		p, err := anthropic.NewProvider(anthropic.Config{OAuthToken: tok})
		return p, "ANTHROPIC_AUTH_TOKEN (subscription)", err
	}
	creds, credsErr := anthropic.LoadClaudeCredentials()
	if credsErr == nil {
		if creds.Expired() {
			return nil, "", fmt.Errorf("Claude Code OAuth token at ~/.claude/.credentials.json expired at %s — re-run `claude` (the Claude Code CLI) to refresh, or unset and use ANTHROPIC_API_KEY",
				creds.ExpiresAt.Format(time.RFC3339))
		}
		p, err := anthropic.NewProvider(anthropic.Config{OAuthToken: creds.AccessToken})
		label := "Claude Code subscription"
		if creds.SubscriptionType != "" {
			label = fmt.Sprintf("Claude Code subscription (%s)", creds.SubscriptionType)
		}
		return p, label, err
	}
	if !errors.Is(credsErr, os.ErrNotExist) {
		return nil, "", fmt.Errorf("read ~/.claude/.credentials.json: %w", credsErr)
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		p, err := anthropic.NewProvider(anthropic.Config{APIKey: key})
		return p, "ANTHROPIC_API_KEY (api)", err
	}
	return nil, "", errors.New("no Anthropic credentials found. Set ANTHROPIC_AUTH_TOKEN (subscription OAuth token), run Claude Code's /login to populate ~/.claude/.credentials.json, or set ANTHROPIC_API_KEY (pay-as-you-go)")
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
	abs, err := os.Getwd()
	if err != nil {
		return dir, err
	}
	if dir == "." || dir == "" {
		return abs, nil
	}
	return dir, nil
}
