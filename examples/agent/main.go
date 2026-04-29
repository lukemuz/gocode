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
	listDirTool, listDirFn := agent.NewTypedTool[ListDirInput](
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

	readFileTool, readFileFn := agent.NewTypedTool[ReadFileInput](
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

	tools := agent.Tools(
		agent.Bind(listDirTool, listDirFn),
		agent.Bind(readFileTool, readFileFn),
	)

	provider, err := agent.NewAnthropicProviderFromEnv()
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
		tools,
		10, // max iterations
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.FinalText())
	fmt.Printf("\ntokens: %d in, %d out\n", result.Usage.InputTokens, result.Usage.OutputTokens)
}
