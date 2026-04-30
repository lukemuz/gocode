// Tier 1 example: a single LLM call.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/lukemuz/gocode"
	"github.com/lukemuz/gocode/providers/anthropic"
)

func main() {
	client, err := anthropic.NewClientFromEnv(gocode.ModelSonnet)
	if err != nil {
		log.Fatal(err)
	}

	history := []gocode.Message{
		gocode.NewUserMessage("What is the capital of France?"),
	}

	reply, usage, err := client.Ask(context.Background(), "", history)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(gocode.TextContent(reply))
	fmt.Printf("\n(tokens: %d in / %d out)\n", usage.InputTokens, usage.OutputTokens)
}
