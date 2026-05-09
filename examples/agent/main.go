// Tier 3 example: a full agentic loop with tool use.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/lukemuz/luft"
	"github.com/lukemuz/luft/providers/anthropic"
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
	listDirTool, listDirFn := luft.NewTypedTool[ListDirInput](
		"list_dir",
		"List the files in a directory.",
		luft.Object(
			luft.String("path", "Path to the directory", luft.Required()),
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
			return luft.JSONResult(names)
		},
	)

	readFileTool, readFileFn := luft.NewTypedTool[ReadFileInput](
		"read_file",
		"Read the contents of a file.",
		luft.Object(
			luft.String("path", "Path to the file", luft.Required()),
		),
		func(_ context.Context, in ReadFileInput) (string, error) {
			data, err := os.ReadFile(in.Path)
			if err != nil {
				return "", err
			}
			return string(data), nil
		},
	)

	tools := luft.Tools(
		luft.Bind(listDirTool, listDirFn),
		luft.Bind(readFileTool, readFileFn),
	)

	provider, err := anthropic.NewProviderFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	client, err := luft.New(luft.Config{
		Provider:  provider,
		Model:     luft.ModelSonnet,
		MaxTokens: 2048,
	})
	if err != nil {
		log.Fatal(err)
	}

	history := []luft.Message{
		luft.NewUserMessage(
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
