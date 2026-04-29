// Tier 3 example: a full agentic loop with tool use.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/lukemuz/gocode"
	"github.com/lukemuz/gocode/providers/anthropic"
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
	listDirTool, listDirFn := gocode.NewTypedTool[ListDirInput](
		"list_dir",
		"List the files in a directory.",
		gocode.Object(
			gocode.String("path", "Path to the directory", gocode.Required()),
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
			return gocode.JSONResult(names)
		},
	)

	readFileTool, readFileFn := gocode.NewTypedTool[ReadFileInput](
		"read_file",
		"Read the contents of a file.",
		gocode.Object(
			gocode.String("path", "Path to the file", gocode.Required()),
		),
		func(_ context.Context, in ReadFileInput) (string, error) {
			data, err := os.ReadFile(in.Path)
			if err != nil {
				return "", err
			}
			return string(data), nil
		},
	)

	tools := gocode.Tools(
		gocode.Bind(listDirTool, listDirFn),
		gocode.Bind(readFileTool, readFileFn),
	)

	provider, err := anthropic.NewProviderFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	client, err := gocode.New(gocode.Config{
		Provider:  provider,
		Model:     gocode.ModelSonnet,
		MaxTokens: 2048,
	})
	if err != nil {
		log.Fatal(err)
	}

	history := []gocode.Message{
		gocode.NewUserMessage(
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
