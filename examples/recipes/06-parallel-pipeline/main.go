// Recipe 06: parallel steps feeding a sequential step.
//
// Two model calls run concurrently via gocode.Parallel; their outputs are
// stitched into a single follow-up prompt. The "pipeline" is just Go control
// flow — there is no graph runtime, no DAG type.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/lukemuz/gocode"
	"github.com/lukemuz/gocode/providers/anthropic"
)

func main() {
	ctx := context.Background()

	client, err := anthropic.NewClientFromEnv(gocode.ModelSonnet)
	if err != nil {
		log.Fatal(err)
	}

	// Fan out: summarize two subjects in parallel.
	results := gocode.Parallel(ctx,
		func(ctx context.Context) (string, error) {
			return ask(ctx, client, "Summarize the rise of the Roman Empire in two sentences.")
		},
		func(ctx context.Context) (string, error) {
			return ask(ctx, client, "Summarize the rise of the Athenian city-state in two sentences.")
		},
	)
	for i, r := range results {
		if r.Err != nil {
			log.Fatalf("step %d failed: %v", i, r.Err)
		}
	}

	// Fan in: a single follow-up call consumes both outputs.
	comparison, err := ask(ctx, client, fmt.Sprintf(
		"Compare these two civilizations based on their summaries.\n\nRome: %s\n\nAthens: %s",
		results[0].Value, results[1].Value,
	))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(comparison)
}

func ask(ctx context.Context, client *gocode.Client, prompt string) (string, error) {
	reply, _, err := client.Ask(ctx, "", []gocode.Message{gocode.NewUserMessage(prompt)})
	if err != nil {
		return "", err
	}
	return gocode.TextContent(reply), nil
}
