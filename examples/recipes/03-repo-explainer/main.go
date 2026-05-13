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

	"github.com/lukemuz/luft"
	"github.com/lukemuz/luft/providers/anthropic"
	"github.com/lukemuz/luft/stores"
	"github.com/lukemuz/luft/tools/clock"
	"github.com/lukemuz/luft/tools/workspace"
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
	sb := luft.NewStreamBuffer(
		func(b luft.ContentBlock) {
			if b.Type == luft.TypeText {
				fmt.Print(b.Text)
			}
		},
		func() { fmt.Fprint(os.Stderr, "\n[retrying...]\n") },
	)

	// 2. Two clients: smart for the agent loop, cheap for the summarizer.
	//
	// Both wrap the same provider. Constructing two Client values is the
	// cost-tiering pattern made trivial by luft's stateless Client design.
	provider, err := anthropic.NewProviderFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	smart, err := luft.New(luft.Config{
		Provider:  provider,
		Model:     luft.ModelSonnet,
		MaxTokens: 4096,
		Retry: luft.RetryConfig{
			MaxRetries:  3,
			InitialWait: time.Second,
			MaxWait:     10 * time.Second,
			OnRetry:     sb.OnRetry,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	cheap := smart.WithModel(luft.ModelHaiku)

	// 2. Toolset: clock + read-only workspace sandboxed to -repo, with
	// per-tool middleware for safety and observability.
	ws, err := workspace.NewReadOnly(workspace.Config{Root: *repo})
	if err != nil {
		log.Fatal(err)
	}
	tools := luft.MustJoin(clock.New().Toolset(), ws.Toolset()).Wrap(
		luft.WithLogging(logger),
		luft.WithTimeout(10*time.Second),
		luft.WithResultLimit(32*1024),
	)

	// 3. Context management with a real summarizer.
	//
	// The summarizer is an ordinary Go function that calls Ask on the cheap
	// client. There's no hidden model invocation: if you remove this field,
	// trimming becomes lossy drop-only. The Summarizer signature takes the
	// trim zone and returns a string that becomes a single user message in
	// the trimmed history.
	cm := luft.ContextManager{
		MaxTokens:  24000,
		KeepFirst:  1,  // the user's original question
		KeepRecent: 12, // recent turns and their tool cycles
		Summarizer: func(sctx context.Context, trimmed []luft.Message) (string, error) {
			rendered := luft.RenderForSummary(trimmed, 0)
			reply, _, err := cheap.Ask(sctx,
				"You compress earlier portions of an investigation transcript. "+
					"Preserve every concrete fact: file paths, line numbers, function "+
					"names, tool outputs the assistant relied on, and conclusions reached.",
				[]luft.Message{luft.NewUserMessage(
					"Summarize the following transcript in 4-8 sentences. " +
						"Be specific. Do not invent.\n\n" + rendered)},
			)
			if err != nil {
				return "", err
			}
			return "[summary of earlier turns] " + luft.TextContent(reply), nil
		},
	}

	// 4. Agent assembly.
	a := luft.Agent{
		Client: smart,
		System: "You are a code archaeologist. Use your tools to investigate the " +
			"repository before answering. Cite specific files and line numbers. " +
			"If you don't have enough information, say so and gather more.",
		Tools:   tools,
		Context: cm,
		MaxIter: 12,
	}

	// 5. Session: load if -session, append the new user turn, run, persist.
	var store luft.Store
	sess := &luft.Session{}
	if *sessionID != "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		store, err = stores.NewFileStore(filepath.Join(home, ".repo-explainer"))
		if err != nil {
			log.Fatal(err)
		}
		sess, err = luft.Load(ctx, store, *sessionID)
		if err != nil {
			log.Fatal(err)
		}
	}
	sess.History = append(sess.History, luft.NewUserMessage(question))

	// 6. Streamed step.
	result, err := a.StepStream(ctx, sess.History,
		sb.OnToken,
		func(results []luft.ToolResult) {
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
		if err := luft.Save(ctx, store, sess); err != nil {
			log.Fatal(err)
		}
		fmt.Fprintf(os.Stderr, "session %s: %d messages saved\n", sess.ID, len(sess.History))
	}
	fmt.Fprintf(os.Stderr, "tokens: %d in, %d out\n",
		result.Usage.InputTokens, result.Usage.OutputTokens)
}
