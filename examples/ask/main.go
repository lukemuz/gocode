// Tier 1 example: a single LLM call.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/lukemuz/gocode/agent"
)

func main() {
	provider, err := agent.NewAnthropicProvider(agent.AnthropicConfig{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
	})
	if err != nil {
		log.Fatal(err)
	}
	client, err := agent.New(agent.Config{
		Provider: provider,
		Model:    agent.ModelSonnet,
	})
	if err != nil {
		log.Fatal(err)
	}

	history := []agent.Message{
		agent.NewUserMessage("What is the capital of France?"),
	}

	reply, err := client.Ask(context.Background(), "", history)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(agent.TextContent(reply))
}
