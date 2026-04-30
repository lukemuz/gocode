// Recipe 04: a router orchestrator that delegates to specialist subagents.
//
// The headline claim: in gocode, a subagent is a ToolFunc that happens to call
// Loop. There is no SubAgent type. The parent's dispatch map is the routing
// mechanism. This file demonstrates that pattern end-to-end.
//
// Topology:
//
//	orchestrator (smart model, no domain tools)
//	├── research(task)   — workspace + clock tools, cheaper model
//	└── write(brief)     — no tools, cheaper model
//
// Run:
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run ./examples/recipes/04-router-subagents -dir . "What does this project do, and what's the testing story?"
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/lukemuz/gocode"
	"github.com/lukemuz/gocode/providers/anthropic"
	"github.com/lukemuz/gocode/tools/clock"
	"github.com/lukemuz/gocode/tools/workspace"
)

func main() {
	dir := flag.String("dir", ".", "directory the researcher subagent may inspect")
	flag.Parse()

	question := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if question == "" {
		log.Fatal("usage: router-subagents [-dir PATH] \"your question\"")
	}

	ctx := context.Background()

	// Two clients: a smarter model for the orchestrator, a cheaper one for
	// the specialists. This is the cost-tiering pattern that "subagents are
	// tools" makes trivial — there's no shared session to coordinate.
	provider, err := anthropic.NewProviderFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	smart, err := gocode.New(gocode.Config{Provider: provider, Model: gocode.ModelSonnet, MaxTokens: 4096})
	if err != nil {
		log.Fatal(err)
	}
	cheap := smart.WithModel(gocode.ModelHaiku)

	// Research subagent: workspace + clock, sandboxed to -dir.
	ws, err := workspace.NewReadOnly(workspace.Config{Root: *dir})
	if err != nil {
		log.Fatal(err)
	}
	researchTools := gocode.MustJoin(clock.New().Toolset(), ws.Toolset())

	researchTool, researchFn := subagentTool(
		"research",
		"Delegate an investigation task. The researcher has read-only filesystem tools "+
			"sandboxed to the project directory and a clock. Call this when the question "+
			"requires inspecting files. Pass a focused, self-contained task description.",
		cheap,
		"You are a research specialist. Use your tools to investigate the project "+
			"directory and return a concise factual summary. Cite specific files and line "+
			"numbers where relevant. Do not speculate beyond what you can verify.",
		researchTools,
		8,
	)

	// Writer subagent: no tools.
	writeTool, writeFn := subagentTool(
		"write",
		"Delegate a writing task. The writer has no tools and turns research notes "+
			"into a clear, well-structured answer for the user. Pass the user's original "+
			"question and the research notes you want polished.",
		cheap,
		"You are a writing specialist. Turn the supplied notes into a clear, "+
			"well-structured answer. Be specific. Do not invent facts beyond the notes.",
		gocode.Toolset{},
		2,
	)

	orchestrator := gocode.Agent{
		Client: smart,
		System: "You are an orchestrator. You have two specialists available as tools: " +
			"`research` (can inspect the project directory) and `write` (turns notes into prose). " +
			"For factual questions about the codebase, call `research` first, then `write` to " +
			"format the final answer. For pure writing tasks, skip `research`. " +
			"Return only the final polished answer to the user.",
		Tools: gocode.Tools(
			gocode.ToolBinding{Tool: researchTool, Func: researchFn, Meta: gocode.ToolMetadata{Source: "subagent/research"}},
			gocode.ToolBinding{Tool: writeTool, Func: writeFn, Meta: gocode.ToolMetadata{Source: "subagent/write"}},
		),
		MaxIter: 6,
	}

	history := []gocode.Message{gocode.NewUserMessage(question)}
	result, err := orchestrator.Step(ctx, history)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.FinalText())
	fmt.Fprintf(os.Stderr, "\norchestrator tokens: %d in, %d out\n",
		result.Usage.InputTokens, result.Usage.OutputTokens)
}

// subagentTool packages a Client + system prompt + Toolset + iteration cap as
// a single tool the parent can call. The schema is a fixed {task: string}.
//
// This is *not* a library API — it lives in this example precisely because
// the gocode position is that subagents do not need a dedicated type. If this
// helper proves useful across multiple recipes, that's the point at which it
// might earn a place in the library, not before.
func subagentTool(
	name, description string,
	client *gocode.Client,
	system string,
	tools gocode.Toolset,
	maxIter int,
) (gocode.Tool, gocode.ToolFunc) {
	type input struct {
		Task string `json:"task"`
	}
	return gocode.NewTypedTool[input](
		name,
		description,
		gocode.Object(
			gocode.String("task", "Self-contained task description for the specialist", gocode.Required()),
		),
		func(ctx context.Context, in input) (string, error) {
			result, err := client.Loop(
				ctx,
				system,
				[]gocode.Message{gocode.NewUserMessage(in.Task)},
				tools,
				maxIter,
			)
			if err != nil {
				// Surface the subagent's accumulated work in the error so the
				// parent's tool result is still informative on failure.
				return summarizeOnError(result), fmt.Errorf("subagent %q: %w", name, err)
			}
			text := result.FinalText()
			if text == "" {
				return "", fmt.Errorf("subagent %q returned no text", name)
			}
			return text, nil
		},
	)
}

// summarizeOnError extracts whatever text the subagent managed to produce
// before failing. This is best-effort context for the parent agent.
func summarizeOnError(result gocode.LoopResult) string {
	for i := len(result.Messages) - 1; i >= 0; i-- {
		if t := gocode.TextContent(result.Messages[i]); t != "" {
			b, _ := json.Marshal(map[string]string{"partial": t})
			return string(b)
		}
	}
	return ""
}
