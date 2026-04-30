package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lukemuz/gocode"
)

// session is the mutable state owned by the REPL. It bundles the
// running history, accumulated usage, and the auxiliary clients we
// need for slash commands like :compact and :model.
type session struct {
	agent      gocode.Agent
	summarizer *gocode.Client // typically Haiku, used by :compact
	provider   gocode.Provider
	memory     string // loaded project memory, for :memory

	history []gocode.Message
	usage   gocode.Usage // accumulated across the session
}

func (s *session) repl(ctx context.Context) {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for {
		fmt.Fprint(os.Stderr, "\n> ")
		if !scanner.Scan() {
			fmt.Fprintln(os.Stderr)
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") || strings.HasPrefix(line, ":") {
			if quit := s.runCommand(ctx, line); quit {
				return
			}
			continue
		}

		s.runTurn(ctx, line)
	}
}

func (s *session) runTurn(ctx context.Context, input string) {
	s.history = append(s.history, gocode.NewUserMessage(input))

	turnCtx, cancel := signal.NotifyContext(ctx, syscall.SIGINT)
	defer cancel()

	result, err := s.agent.StepStream(turnCtx, s.history,
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
	fmt.Println()

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		if n := len(s.history); n > 0 {
			s.history = s.history[:n-1]
		}
		return
	}

	s.history = result.Messages
	s.usage.InputTokens += result.Usage.InputTokens
	s.usage.OutputTokens += result.Usage.OutputTokens
	s.usage.CacheCreationTokens += result.Usage.CacheCreationTokens
	s.usage.CacheReadTokens += result.Usage.CacheReadTokens
}

// runCommand dispatches a slash command. Returns true if the REPL
// should exit. Both `/cmd` and `:cmd` styles are accepted.
func (s *session) runCommand(ctx context.Context, line string) bool {
	line = strings.TrimLeft(line, "/:")
	parts := strings.SplitN(line, " ", 2)
	cmd := parts[0]
	args := ""
	if len(parts) == 2 {
		args = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case "exit", "quit":
		return true
	case "help", "?":
		s.printHelp()
	case "reset", "clear":
		s.history = nil
		fmt.Fprintln(os.Stderr, "(history cleared)")
	case "compact":
		s.doCompact(ctx, args)
	case "tokens":
		s.printTokens()
	case "memory":
		s.printMemory()
	case "tools":
		s.printTools()
	case "model":
		s.changeModel(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: /%s (try /help)\n", cmd)
	}
	return false
}

func (s *session) printHelp() {
	for _, line := range []string{
		"/help                    show this list",
		"/exit | /quit            leave",
		"/reset | /clear          clear conversation history",
		"/compact [instructions]  summarize older turns to free context (cache-resetting)",
		"/tokens                  print accumulated token usage and cache stats",
		"/memory                  print the loaded project memory (AGENTS.md / CLAUDE.md)",
		"/tools                   list the tools currently available to the agent",
		"/model <id>              switch the main-agent model (e.g. claude-opus-4-7)",
	} {
		fmt.Fprintln(os.Stderr, line)
	}
}

func (s *session) doCompact(ctx context.Context, instructions string) {
	if len(s.history) < 2 {
		fmt.Fprintln(os.Stderr, "(nothing to compact)")
		return
	}
	before := len(s.history)
	fmt.Fprintln(os.Stderr, "compacting...")
	out, usage, err := compact(ctx, s.summarizer, s.history, 4, instructions)
	if err != nil {
		fmt.Fprintf(os.Stderr, "compact failed: %v\n", err)
		return
	}
	s.history = out
	s.usage.InputTokens += usage.InputTokens
	s.usage.OutputTokens += usage.OutputTokens
	s.usage.CacheCreationTokens += usage.CacheCreationTokens
	s.usage.CacheReadTokens += usage.CacheReadTokens

	fmt.Fprintf(os.Stderr, "compacted: %d → %d messages (summarizer used %d in / %d out tokens)\n",
		before, len(s.history), usage.InputTokens, usage.OutputTokens)
}

func (s *session) printTokens() {
	u := s.usage
	total := u.InputTokens + u.OutputTokens
	fmt.Fprintf(os.Stderr, "tokens this session:\n")
	fmt.Fprintf(os.Stderr, "  input:           %d\n", u.InputTokens)
	fmt.Fprintf(os.Stderr, "  output:          %d\n", u.OutputTokens)
	fmt.Fprintf(os.Stderr, "  cache reads:     %d\n", u.CacheReadTokens)
	fmt.Fprintf(os.Stderr, "  cache writes:    %d\n", u.CacheCreationTokens)
	fmt.Fprintf(os.Stderr, "  total billable:  %d (input+output, cache reads billed at ~10%%)\n", total)
	if u.InputTokens > 0 {
		ratio := float64(u.CacheReadTokens) / float64(u.InputTokens+u.CacheReadTokens) * 100
		fmt.Fprintf(os.Stderr, "  cache hit rate:  %.1f%%\n", ratio)
	}
}

func (s *session) printMemory() {
	if s.memory == "" {
		fmt.Fprintln(os.Stderr, "(no project memory loaded — drop an AGENTS.md or CLAUDE.md in the workspace root)")
		return
	}
	fmt.Fprintln(os.Stderr, s.memory)
}

func (s *session) printTools() {
	for _, b := range s.agent.Tools.Bindings {
		flag := ""
		if b.Meta.RequiresConfirmation {
			flag = " [confirm]"
		}
		fmt.Fprintf(os.Stderr, "  %s%s\n", b.Tool.Name, flag)
	}
}

func (s *session) changeModel(model string) {
	if model == "" {
		fmt.Fprintln(os.Stderr, "usage: /model <model-id>")
		return
	}
	s.agent.Client = s.agent.Client.WithModel(model)
	fmt.Fprintf(os.Stderr, "main-agent model set to %s (subagent models unchanged)\n", model)
}
