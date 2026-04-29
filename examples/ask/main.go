// Tier 1 example: a single LLM call.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/lukemuz/gocode/agent"
)

func main() {
	client, err := agent.NewAnthropicClientFromEnv(agent.ModelSonnet)
	if err != nil {
		log.Fatal(err)
	}

	history := []agent.Message{
		agent.NewUserMessage("What is the capital of France?"),
	}

	reply, usage, err := client.Ask(context.Background(), "", history)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(agent.TextContent(reply))
	fmt.Printf("\n(tokens: %d in / %d out)\n", usage.InputTokens, usage.OutputTokens)
}
