// gocode is a CLI coding agent built on the gocode toolkit.
//
// Phase 1: a basic REPL that pairs an Anthropic-backed Agent with the
// workspace, bash, todo, and clock tools. Future phases add a model
// router, parallel batching, and aggressive context management.
//
// Usage:
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run ./cmd/gocode -dir . -bash standard
//
// Flags:
//
//	-dir       working directory the agent is sandboxed to (default ".")
//	-model     Anthropic model id (default claude-sonnet-4-6)
//	-bash      bash safety mode: restricted | standard | unrestricted
//	-yes       auto-approve every confirmation prompt
//	-max-iter  max model calls per turn (default 30)
//
// Inside the REPL:
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
	"strings"
	"syscall"
	"time"

	"github.com/lukemuz/gocode"
	"github.com/lukemuz/gocode/providers/anthropic"
	"github.com/lukemuz/gocode/tools/bash"
	"github.com/lukemuz/gocode/tools/clock"
	"github.com/lukemuz/gocode/tools/todo"
	"github.com/lukemuz/gocode/tools/workspace"
)

const systemPrompt = `You are gocode, a fast and economical CLI coding assistant built on the gocode toolkit.

You operate inside a workspace directory. Available tools:
- list_directory, find_files, search_text, read_file, file_info: read-only filesystem inspection
- edit_file: in-place text replacement (requires user confirmation)
- bash: run shell commands (safety policy varies by configuration)
- todo_write, todo_read: maintain a short planning checklist for multi-step work
- now: current time

Operating principles:
1. Prefer the structured workspace tools (search_text, find_files, read_file) over bash for inspection — they are sandboxed and predictable.
2. For multi-step tasks, call todo_write at the start and update it as you go. Keep at most one item in_progress.
3. Be concise in chat. State what you're doing in one short sentence before tool calls; don't narrate every step.
4. When you change files, summarize the diff in one or two lines after.
5. If you don't know something about the codebase, look it up before guessing.`

func main() {
	dir := flag.String("dir", ".", "working directory the agent is sandboxed to")
	model := flag.String("model", gocode.ModelSonnet, "Anthropic model id")
	bashMode := flag.String("bash", "restricted", "bash safety mode: restricted | standard | unrestricted")
	autoYes := flag.Bool("yes", false, "auto-approve every confirmation prompt")
	maxIter := flag.Int("max-iter", 30, "max model calls per turn")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	mode, err := parseBashMode(*bashMode)
	if err != nil {
		log.Fatal(err)
	}

	provider, err := anthropic.NewProviderFromEnv()
	if err != nil {
		log.Fatalf("anthropic provider: %v", err)
	}

	client, err := gocode.New(gocode.Config{
		Provider:  provider,
		Model:     *model,
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

	ws, err := workspace.New(workspace.Config{Root: *dir})
	if err != nil {
		log.Fatal(err)
	}
	bashTool, err := bash.New(bash.Config{Root: *dir, Mode: mode})
	if err != nil {
		log.Fatal(err)
	}
	todos := todo.New()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	confirm := makeConfirmer(*autoYes)

	tools := gocode.MustJoin(
		clock.New().Toolset(),
		ws.Toolset(),
		bashTool.Toolset(),
		todos.Toolset(),
	).Wrap(
		gocode.WithConfirmation(confirm),
		gocode.WithTimeout(60*time.Second),
		gocode.WithResultLimit(64*1024),
		gocode.WithLogging(logger),
	)

	agent := gocode.Agent{
		Client: client,
		System: systemPrompt,
		Tools:  tools,
		Context: gocode.ContextManager{
			MaxTokens:  120_000,
			KeepFirst:  1,
			KeepRecent: 30,
		},
		MaxIter: *maxIter,
	}

	abs, _ := absDir(*dir)
	fmt.Fprintf(os.Stderr, "gocode  model=%s  bash=%s  dir=%s\n", *model, *bashMode, abs)
	fmt.Fprintln(os.Stderr, "type a request, or :help for commands. ctrl-c to interrupt, ctrl-d to exit.")

	repl(ctx, agent)
}

func repl(ctx context.Context, agent gocode.Agent) {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var history []gocode.Message
	var totalIn, totalOut int

	for {
		fmt.Fprint(os.Stderr, "\n> ")
		if !scanner.Scan() {
			fmt.Fprintln(os.Stderr)
			return
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		switch input {
		case ":exit", ":quit":
			return
		case ":help":
			fmt.Fprintln(os.Stderr, ":exit | :quit       leave")
			fmt.Fprintln(os.Stderr, ":reset              clear conversation history")
			fmt.Fprintln(os.Stderr, ":tokens             print accumulated token usage")
			continue
		case ":reset":
			history = nil
			fmt.Fprintln(os.Stderr, "(history cleared)")
			continue
		case ":tokens":
			fmt.Fprintf(os.Stderr, "tokens: %d in, %d out\n", totalIn, totalOut)
			continue
		}

		history = append(history, gocode.NewUserMessage(input))

		turnCtx, cancel := signal.NotifyContext(ctx, syscall.SIGINT)
		result, err := agent.StepStream(turnCtx, history,
			func(b gocode.ContentBlock) {
				if b.Type == gocode.TypeText {
					fmt.Print(b.Text)
				}
			},
			func(results []gocode.ToolResult) {
				for _, r := range results {
					status := "ok"
					if r.IsError {
						status = "error"
					}
					fmt.Fprintf(os.Stderr, "\n[tool %s: %d bytes]\n", status, len(r.Content))
				}
			},
		)
		cancel()
		fmt.Println()

		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			// Roll back this turn so the user can retry without poisoned history.
			if n := len(history); n > 0 {
				history = history[:n-1]
			}
			continue
		}

		history = result.Messages
		totalIn += result.Usage.InputTokens
		totalOut += result.Usage.OutputTokens
	}
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
