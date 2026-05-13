// Tier 1 example: a single LLM call.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/lukemuz/luft"
	"github.com/lukemuz/luft/providers/anthropic"
)

func main() {
	client, err := anthropic.NewClientFromEnv(luft.ModelSonnet)
	if err != nil {
		log.Fatal(err)
	}

	history := []luft.Message{
		luft.NewUserMessage("What is the capital of France?"),
	}

	reply, usage, err := client.Ask(context.Background(), "", history)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(luft.TextContent(reply))
	fmt.Printf("\n(tokens: %d in / %d out)\n", usage.InputTokens, usage.OutputTokens)
}
