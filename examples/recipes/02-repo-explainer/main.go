// Recipe 02: repo-explainer.
//
// A practical tool: answer questions about a code repository. Builds on
// recipe 01 by adding the three things 01 deliberately omitted —
// persistent sessions, summarization, and longer multi-turn conversations.
//
// Shape:
//
//	repo-explainer -repo PATH "your question"          # one-shot
//	repo-explainer -repo PATH -session ID "question"   # persisted
//
// When -session is given, history is loaded from (and written to)
// ~/.repo-explainer/<id>.json. Repeated invocations with the same -session
// continue the conversation.
//
// The Summarizer in the ContextManager calls a cheaper model to compress
// older turns when the context budget is exceeded — proving that
// summarization is caller-owned and visible, not framework magic.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lukemuz/gocode/agent"
	"github.com/lukemuz/gocode/agent/tools/clock"
	"github.com/lukemuz/gocode/agent/tools/workspace"
)

func main() {
	repo := flag.String("repo", ".", "repository directory to explore")
	sessionID := flag.String("session", "", "optional session ID for persistent conversation history")
	flag.Parse()

	question := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if question == "" {
		log.Fatal(`usage: repo-explainer -repo PATH [-session ID] "your question"`)
	}

	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// 1. Streaming with retry awareness, set up before client construction
	// so RetryConfig.OnRetry can clear partial output between attempts.
	sb := agent.NewStreamBuffer(
		func(b agent.ContentBlock) {
			if b.Type == agent.TypeText {
				fmt.Print(b.Text)
			}
		},
		func() { fmt.Fprint(os.Stderr, "\n[retrying...]\n") },
	)

	// 2. Two clients: smart for the agent loop, cheap for the summarizer.
	//
	// Both wrap the same provider. Constructing two Client values is the
	// cost-tiering pattern made trivial by gocode's stateless Client design.
	provider, err := agent.NewAnthropicProviderFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	smart, err := agent.New(agent.Config{
		Provider:  provider,
		Model:     agent.ModelSonnet,
		MaxTokens: 4096,
		Retry: agent.RetryConfig{
			MaxRetries:  3,
			InitialWait: time.Second,
			MaxWait:     10 * time.Second,
			OnRetry:     sb.OnRetry,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	cheap, err := agent.New(agent.Config{
		Provider:  provider,
		Model:     agent.ModelHaiku,
		MaxTokens: 1024,
	})
	if err != nil {
		log.Fatal(err)
	}

	// 2. Toolset: clock + read-only workspace sandboxed to -repo, with
	// per-tool middleware for safety and observability.
	ws, err := workspace.NewReadOnly(workspace.Config{Root: *repo})
	if err != nil {
		log.Fatal(err)
	}
	tools := agent.MustJoin(clock.New().Toolset(), ws.Toolset()).Wrap(
		agent.WithLogging(logger),
		agent.WithTimeout(10*time.Second),
		agent.WithResultLimit(32*1024),
	)

	// 3. Context management with a real summarizer.
	//
	// The summarizer is an ordinary Go function that calls Ask on the cheap
	// client. There's no hidden model invocation: if you remove this field,
	// trimming becomes lossy drop-only. The Summarizer signature takes the
	// trim zone and returns a string that becomes a single user message in
	// the trimmed history.
	cm := agent.ContextManager{
		MaxTokens:  24000,
		KeepFirst:  1,  // the user's original question
		KeepRecent: 12, // recent turns and their tool cycles
		Summarizer: func(sctx context.Context, trimmed []agent.Message) (string, error) {
			rendered := renderForSummary(trimmed)
			reply, err := cheap.Ask(sctx,
				"You compress earlier portions of an investigation transcript. "+
					"Preserve every concrete fact: file paths, line numbers, function "+
					"names, tool outputs the assistant relied on, and conclusions reached.",
				[]agent.Message{agent.NewUserMessage(
					"Summarize the following transcript in 4-8 sentences. " +
						"Be specific. Do not invent.\n\n" + rendered)},
			)
			if err != nil {
				return "", err
			}
			return "[summary of earlier turns] " + agent.TextContent(reply), nil
		},
	}

	// 4. Assistant assembly.
	a := agent.Assistant{
		Client: smart,
		System: "You are a code archaeologist. Use your tools to investigate the " +
			"repository before answering. Cite specific files and line numbers. " +
			"If you don't have enough information, say so and gather more.",
		Tools:   tools,
		Context: cm,
		MaxIter: 12,
	}

	// 5. Session: load if -session, append the new user turn, run, persist.
	store, sess, err := loadOrCreateSession(ctx, *sessionID)
	if err != nil {
		log.Fatal(err)
	}
	sess.History = append(sess.History, agent.NewUserMessage(question))

	// 6. Streamed step.
	result, err := a.StepStream(ctx, sess.History,
		sb.OnToken,
		func(results []agent.ToolResult) {
			for _, r := range results {
				status := "ok"
				if r.IsError {
					status = "error"
				}
				fmt.Fprintf(os.Stderr, "[tool %s: %d bytes]\n", status, len(r.Content))
			}
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println()

	// 7. Persist updated history if a session was requested.
	sess.History = result.Messages
	if store != nil {
		if err := agent.Save(ctx, store, sess); err != nil {
			log.Fatal(err)
		}
		fmt.Fprintf(os.Stderr, "session %s: %d messages saved\n", sess.ID, len(sess.History))
	}
	fmt.Fprintf(os.Stderr, "tokens: %d in, %d out\n",
		result.Usage.InputTokens, result.Usage.OutputTokens)
}

// loadOrCreateSession returns (nil, fresh-session, nil) when no -session was
// requested, or (store, loaded-or-new-session, nil) when persistence is on.
// A session ID resolves to ~/.repo-explainer/<id>.json.
func loadOrCreateSession(ctx context.Context, id string) (agent.Store, *agent.Session, error) {
	if id == "" {
		return nil, &agent.Session{}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}
	dir := filepath.Join(home, ".repo-explainer")
	store, err := agent.NewFileStore(dir)
	if err != nil {
		return nil, nil, err
	}
	sess, err := store.Get(ctx, id)
	switch {
	case err == nil:
		return store, sess, nil
	case errors.Is(err, agent.ErrSessionNotFound):
		return store, &agent.Session{ID: id}, nil
	default:
		return nil, nil, err
	}
}

// renderForSummary produces a plain-text rendering of message history for
// passing to the summarizer. Tool results are abridged so the summarizer
// can see the structure without paying the full tool-output token cost.
func renderForSummary(msgs []agent.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case agent.RoleUser:
			text := agent.TextContent(m)
			if text != "" {
				fmt.Fprintf(&b, "USER: %s\n", text)
				continue
			}
			// Pure tool_result message.
			for _, c := range m.Content {
				if c.Type == agent.TypeToolResult {
					fmt.Fprintf(&b, "TOOL_RESULT (%s): %s\n", c.ToolUseID, abbreviate(c.Content, 400))
				}
			}
		case agent.RoleAssistant:
			for _, c := range m.Content {
				switch c.Type {
				case agent.TypeText:
					fmt.Fprintf(&b, "ASSISTANT: %s\n", c.Text)
				case agent.TypeToolUse:
					fmt.Fprintf(&b, "ASSISTANT_TOOL_USE: %s(%s)\n", c.Name, abbreviate(string(c.Input), 200))
				}
			}
		}
	}
	return b.String()
}

func abbreviate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}
