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
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lukemuz/gocode"
	"github.com/lukemuz/gocode/stores"
	"github.com/lukemuz/gocode/providers/anthropic"
	"github.com/lukemuz/gocode/tools/clock"
	"github.com/lukemuz/gocode/tools/workspace"
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
	sb := gocode.NewStreamBuffer(
		func(b gocode.ContentBlock) {
			if b.Type == gocode.TypeText {
				fmt.Print(b.Text)
			}
		},
		func() { fmt.Fprint(os.Stderr, "\n[retrying...]\n") },
	)

	// 2. Two clients: smart for the agent loop, cheap for the summarizer.
	//
	// Both wrap the same provider. Constructing two Client values is the
	// cost-tiering pattern made trivial by gocode's stateless Client design.
	provider, err := anthropic.NewProviderFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	smart, err := gocode.New(gocode.Config{
		Provider:  provider,
		Model:     gocode.ModelSonnet,
		MaxTokens: 4096,
		Retry: gocode.RetryConfig{
			MaxRetries:  3,
			InitialWait: time.Second,
			MaxWait:     10 * time.Second,
			OnRetry:     sb.OnRetry,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	cheap := smart.WithModel(gocode.ModelHaiku)

	// 2. Toolset: clock + read-only workspace sandboxed to -repo, with
	// per-tool middleware for safety and observability.
	ws, err := workspace.NewReadOnly(workspace.Config{Root: *repo})
	if err != nil {
		log.Fatal(err)
	}
	tools := gocode.MustJoin(clock.New().Toolset(), ws.Toolset()).Wrap(
		gocode.WithLogging(logger),
		gocode.WithTimeout(10*time.Second),
		gocode.WithResultLimit(32*1024),
	)

	// 3. Context management with a real summarizer.
	//
	// The summarizer is an ordinary Go function that calls Ask on the cheap
	// client. There's no hidden model invocation: if you remove this field,
	// trimming becomes lossy drop-only. The Summarizer signature takes the
	// trim zone and returns a string that becomes a single user message in
	// the trimmed history.
	cm := gocode.ContextManager{
		MaxTokens:  24000,
		KeepFirst:  1,  // the user's original question
		KeepRecent: 12, // recent turns and their tool cycles
		Summarizer: func(sctx context.Context, trimmed []gocode.Message) (string, error) {
			rendered := gocode.RenderForSummary(trimmed, 0)
			reply, _, err := cheap.Ask(sctx,
				"You compress earlier portions of an investigation transcript. "+
					"Preserve every concrete fact: file paths, line numbers, function "+
					"names, tool outputs the assistant relied on, and conclusions reached.",
				[]gocode.Message{gocode.NewUserMessage(
					"Summarize the following transcript in 4-8 sentences. " +
						"Be specific. Do not invent.\n\n" + rendered)},
			)
			if err != nil {
				return "", err
			}
			return "[summary of earlier turns] " + gocode.TextContent(reply), nil
		},
	}

	// 4. Agent assembly.
	a := gocode.Agent{
		Client: smart,
		System: "You are a code archaeologist. Use your tools to investigate the " +
			"repository before answering. Cite specific files and line numbers. " +
			"If you don't have enough information, say so and gather more.",
		Tools:   tools,
		Context: cm,
		MaxIter: 12,
	}

	// 5. Session: load if -session, append the new user turn, run, persist.
	var store gocode.Store
	sess := &gocode.Session{}
	if *sessionID != "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		store, err = stores.NewFileStore(filepath.Join(home, ".repo-explainer"))
		if err != nil {
			log.Fatal(err)
		}
		sess, err = gocode.Load(ctx, store, *sessionID)
		if err != nil {
			log.Fatal(err)
		}
	}
	sess.History = append(sess.History, gocode.NewUserMessage(question))

	// 6. Streamed step.
	result, err := a.StepStream(ctx, sess.History,
		sb.OnToken,
		func(results []gocode.ToolResult) {
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
		if err := gocode.Save(ctx, store, sess); err != nil {
			log.Fatal(err)
		}
		fmt.Fprintf(os.Stderr, "session %s: %d messages saved\n", sess.ID, len(sess.History))
	}
	fmt.Fprintf(os.Stderr, "tokens: %d in, %d out\n",
		result.Usage.InputTokens, result.Usage.OutputTokens)
}

