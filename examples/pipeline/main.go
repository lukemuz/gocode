// Tier 2 example: parallel steps feeding a sequential step.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/lukemuz/gocode/agent"
)

func main() {
	ctx := context.Background()

	client, err := agent.NewAnthropicClientFromEnv(agent.ModelSonnet)
	if err != nil {
		log.Fatal(err)
	}

	// Summarize two subjects in parallel, then compare them in a single follow-up call.
	results := agent.Parallel(ctx,
		func(ctx context.Context) (string, error) {
			return singleAsk(ctx, client, "Summarize the rise of the Roman Empire in two sentences.")
		},
		func(ctx context.Context) (string, error) {
			return singleAsk(ctx, client, "Summarize the rise of the Athenian city-state in two sentences.")
		},
	)

	for i, r := range results {
		if r.Err != nil {
			log.Fatalf("step %d failed: %v", i, r.Err)
		}
	}

	comparison, err := singleAsk(ctx, client, fmt.Sprintf(
		"Compare these two civilizations based on their summaries.\n\nRome: %s\n\nAthens: %s",
		results[0].Value, results[1].Value,
	))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(comparison)
}

func singleAsk(ctx context.Context, client *agent.Client, prompt string) (string, error) {
	reply, _, err := client.Ask(ctx, "", []agent.Message{agent.NewUserMessage(prompt)})
	if err != nil {
		return "", err
	}
	return agent.TextContent(reply), nil
}
