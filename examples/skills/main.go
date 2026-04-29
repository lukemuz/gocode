// Skills example: composing a skill with a base system prompt and running
// it in the agent loop.
//
// Run with ANTHROPIC_API_KEY set:
//
//	go run ./examples/skills --skill repo --root .
//	go run ./examples/skills --skill review --root ./agent
//	go run ./examples/skills --skill logs --root /var/log
//	go run ./examples/skills --skill docs --root ./docs
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/lukemuz/gocode/agent"
	"github.com/lukemuz/gocode/agent/skills"
)

func main() {
	skillName := flag.String("skill", "repo", "skill to use: repo, review, logs, docs")
	root := flag.String("root", ".", "root directory for the skill")
	question := flag.String("q", "", "question to ask (defaults to a skill-appropriate prompt)")
	flag.Parse()

	ctx := context.Background()

	sk, defaultQ, err := buildSkill(*skillName, *root)
	if err != nil {
		log.Fatalf("build skill: %v", err)
	}

	q := *question
	if q == "" {
		q = defaultQ
	}

	provider, err := agent.NewAnthropicProviderFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	client, err := agent.New(agent.Config{
		Provider:  provider,
		Model:     agent.ModelSonnet,
		MaxTokens: 4096,
	})
	if err != nil {
		log.Fatal(err)
	}

	system := agent.JoinInstructions(
		"You are a helpful assistant.",
		sk.Instructions(),
	)
	toolset := sk.Toolset()

	history := []agent.Message{
		agent.NewUserMessage(q),
	}

	fmt.Fprintf(os.Stderr, "skill: %s  root: %s\n", sk.Meta().Name, *root)
	fmt.Fprintf(os.Stderr, "question: %s\n\n", q)

	result, err := client.Loop(
		ctx,
		system,
		history,
		toolset.Tools(),
		toolset.Dispatch(),
		20,
	)
	if err != nil {
		log.Fatalf("loop: %v", err)
	}

	last := result.Messages[len(result.Messages)-1]
	fmt.Println(agent.TextContent(last))
	fmt.Fprintf(os.Stderr, "\ntokens: %d in, %d out\n", result.Usage.InputTokens, result.Usage.OutputTokens)
}

func buildSkill(name, root string) (skills.Skill, string, error) {
	switch name {
	case "repo":
		s, err := skills.NewRepoExplainer(skills.RepoExplainerConfig{Root: root})
		return s, "Give me an overview of this repository: its purpose, main packages, and how the pieces fit together.", err

	case "review":
		s, err := skills.NewCodeReview(skills.CodeReviewConfig{Root: root})
		return s, "Review the Go files in this directory for correctness, security, and idiomatic style.", err

	case "logs":
		s, err := skills.NewLogSummarizer(skills.LogSummarizerConfig{Root: root})
		return s, "Summarize the log files in this directory, highlighting errors, warnings, and any notable patterns.", err

	case "docs":
		s, err := skills.NewDocsQA(skills.DocsQAConfig{Root: root})
		return s, "What topics are covered by the documentation in this directory? Give me a brief summary of each.", err

	default:
		return nil, "", fmt.Errorf("unknown skill %q — choose from: repo, review, logs, docs", name)
	}
}
