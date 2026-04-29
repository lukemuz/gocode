// Tier 3 example: a full agentic loop with tool use.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/lukemuz/gocode/agent"
)

type ListDirInput struct {
	Path string `json:"path"`
}

type ReadFileInput struct {
	Path string `json:"path"`
}

func main() {
	ctx := context.Background()

	// NewTypedTool + schema builders (Object/String/Required) provide
	// both Tool and ToolFunc with minimal boilerplate. Schema helpers
	// are now implemented (see ROADMAP.md).
	listDirTool, listDirFn, err := agent.NewTypedTool[ListDirInput](
		"list_dir",
		"List the files in a directory.",
		agent.Object(
			agent.String("path", "Path to the directory", agent.Required()),
		),
		func(_ context.Context, in ListDirInput) (string, error) {
			entries, err := os.ReadDir(in.Path)
			if err != nil {
				return "", err
			}
			names := make([]string, len(entries))
			for i, e := range entries {
				names[i] = e.Name()
			}
			return agent.JSONResult(names)
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	readFileTool, readFileFn, err := agent.NewTypedTool[ReadFileInput](
		"read_file",
		"Read the contents of a file.",
		agent.Object(
			agent.String("path", "Path to the file", agent.Required()),
		),
		func(_ context.Context, in ReadFileInput) (string, error) {
			data, err := os.ReadFile(in.Path)
			if err != nil {
				return "", err
			}
			return string(data), nil
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	dispatch := map[string]agent.ToolFunc{
		"list_dir":  listDirFn,
		"read_file": readFileFn,
	}

	provider, err := agent.NewAnthropicProvider(agent.AnthropicConfig{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
	})
	if err != nil {
		log.Fatal(err)
	}
	client, err := agent.New(agent.Config{
		Provider:  provider,
		Model:     agent.ModelSonnet,
		MaxTokens: 2048,
	})
	if err != nil {
		log.Fatal(err)
	}

	history := []agent.Message{
		agent.NewUserMessage(
			"List the files in the current directory, then read go.mod and tell me what Go version this project requires.",
		),
	}

	result, err := client.Loop(
		ctx,
		"You are a helpful assistant with access to the local filesystem.",
		history,
		[]agent.Tool{listDirTool, readFileTool},
		dispatch,
		10, // max iterations
	)
	if err != nil {
		log.Fatal(err)
	}

	// The last message in the history is always the final assistant reply.
	last := result.Messages[len(result.Messages)-1]
	fmt.Println(agent.TextContent(last))
	fmt.Printf("\ntokens: %d in, %d out\n", result.Usage.InputTokens, result.Usage.OutputTokens)
}
