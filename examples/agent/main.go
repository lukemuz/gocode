// Tier 3 example: a full agentic loop with tool use.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/lukemuz/gocode/agent"
)

func main() {
	ctx := context.Background()

	// Define tools by name + description + explicit JSON Schema.
	// No framework magic — what you write is what the model sees.
	listDir, err := agent.NewTool("list_dir", "List the files in a directory.", agent.InputSchema{
		Type: "object",
		Properties: map[string]agent.SchemaProperty{
			"path": {Type: "string", Description: "Path to the directory"},
		},
		Required: []string{"path"},
	})
	if err != nil {
		log.Fatal(err)
	}

	readFile, err := agent.NewTool("read_file", "Read the contents of a file.", agent.InputSchema{
		Type: "object",
		Properties: map[string]agent.SchemaProperty{
			"path": {Type: "string", Description: "Path to the file"},
		},
		Required: []string{"path"},
	})
	if err != nil {
		log.Fatal(err)
	}

	// dispatch maps each tool name to its Go implementation.
	// The model calls the tool; this code runs it.
	dispatch := map[string]agent.ToolFunc{
		"list_dir": func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", err
			}
			entries, err := os.ReadDir(params.Path)
			if err != nil {
				return "", err
			}
			names := make([]string, len(entries))
			for i, e := range entries {
				names[i] = e.Name()
			}
			data, _ := json.Marshal(names)
			return string(data), nil
		},
		"read_file": func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", err
			}
			data, err := os.ReadFile(params.Path)
			if err != nil {
				return "", err
			}
			return string(data), nil
		},
	}

	client, err := agent.New(agent.Config{
		APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
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
		[]agent.Tool{listDir, readFile},
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
