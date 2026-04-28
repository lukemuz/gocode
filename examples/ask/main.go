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
	client, err := agent.New(agent.Config{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
		Model:  agent.ModelSonnet,
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
