// Recipe 01: a single Agent with a curated toolset and streaming output.
//
// This is the entry-point recipe: the smallest "I'm building a real thing"
// example. It shows the practical assembly path — Agent + Toolset +
// middleware + ContextManager + streaming with retry-aware buffering —
// without subagents, persistence, or HTTP. Each later recipe adds one
// dimension to this base.
//
// Run:
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run ./examples/recipes/02-agent-with-tools "What time is it, and what is 17 * 23?"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lukemuz/gocode"
	"github.com/lukemuz/gocode/providers/anthropic"
	"github.com/lukemuz/gocode/tools/clock"
	"github.com/lukemuz/gocode/tools/math"
	"github.com/lukemuz/gocode/tools/workspace"
)

func main() {
	dir := flag.String("dir", ".", "directory the workspace tools may read from")
	flag.Parse()

	question := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if question == "" {
		log.Fatal("usage: agent-with-tools [-dir PATH] \"your question\"")
	}

	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// 1. Streaming with retry awareness.
	//
	// StreamBuffer pairs an OnToken callback (forwarded to AskStream/StepStream)
	// with an OnRetry callback (wired into RetryConfig). When a retry happens
	// mid-stream, the buffer clears partial output before the next attempt
	// starts so the user doesn't see duplicated tokens.
	sb := gocode.NewStreamBuffer(
		func(b gocode.ContentBlock) {
			if b.Type == gocode.TypeText {
				fmt.Print(b.Text)
			}
		},
		func() { fmt.Fprint(os.Stderr, "\n[retrying...]\n") },
	)

	// 2. Client with a smart model and retries that notify the StreamBuffer.
	provider, err := anthropic.NewProviderFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	client, err := gocode.New(gocode.Config{
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

	// 3. Curated toolset.
	//
	// Built-ins composed with gocode.Join, then wrapped with three middlewares
	// applied to every tool: a 5-second timeout per call, a 16 KiB cap on
	// tool output (so a chatty tool can't blow out the context window), and
	// structured logging at Info level. The order of Wrap arguments is
	// outermost-first: WithLogging sees the timeout-wrapped function.
	ws, err := workspace.NewReadOnly(workspace.Config{Root: *dir})
	if err != nil {
		log.Fatal(err)
	}
	tools := gocode.MustJoin(
		clock.New().Toolset(),
		math.New().Toolset(),
		ws.Toolset(),
	).Wrap(
		gocode.WithLogging(logger),
		gocode.WithTimeout(5*time.Second),
		gocode.WithResultLimit(16*1024),
	)

	// 4. Context management.
	//
	// For a one-shot question this is essentially a no-op (history fits in
	// budget), but configuring it now keeps the recipe honest about what a
	// real long-running agent needs. KeepFirst pins the user's original
	// task; KeepRecent always preserves the recent tool cycle.
	cm := gocode.ContextManager{
		MaxTokens:  16000,
		KeepFirst:  1,
		KeepRecent: 20,
	}

	// 5. Agent assembly.
	a := gocode.Agent{
		Client:  client,
		System:  "You are a concise helper. Use your tools when they would give a more accurate answer than guessing.",
		Tools:   tools,
		Context: cm,
		MaxIter: 8,
	}

	// 6. One streamed step.
	history := []gocode.Message{gocode.NewUserMessage(question)}
	result, err := a.StepStream(ctx, history,
		sb.OnToken,
		func(results []gocode.ToolResult) {
			// Tool results are not part of the model's stream; surface them
			// on stderr so the user can see what the agent did.
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
	fmt.Fprintf(os.Stderr, "tokens: %d in, %d out\n",
		result.Usage.InputTokens, result.Usage.OutputTokens)
}
