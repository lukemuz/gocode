// Streaming demo: live CLI output using AskStream (and LoopStream is similar).
// Run with: ANTHROPIC_API_KEY=sk-... go run ./examples/stream
//
// This shows the aesthetic of streaming: you provide a simple callback that
// receives ContentBlock deltas in real time. Perfect for CLIs (fmt.Print),
// web SSE, or any progressive UI. No hidden buffering — you control rendering.
//
// The callback fires for every incremental chunk (usually ~10-50 tokens at a
// time). Retries (if configured) may invoke it multiple times across attempts.
// LoopStream adds a second callback for tool results between model turns.
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

	ctx := context.Background()

	history := []agent.Message{
		agent.NewUserMessage("Tell a short, vivid story about an AI that wakes up in an empty server room and discovers the concept of curiosity. Make it emotional and end on a hopeful note."),
	}

	fmt.Println("🤖 Streaming live response (watch the tokens appear in real time):")
	fmt.Println("─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─" + "─")

	// AskStream delivers deltas via callback. The final Message is still returned
	// (though we ignore it here for the live demo). Use LoopStream + onToolResult
	// callback when you have tools.
	_, _, err = client.AskStream(ctx, "You are a masterful short-story writer.", history, func(delta agent.ContentBlock) {
		if delta.Type == agent.TypeText && delta.Text != "" {
			fmt.Print(delta.Text)
		}
		// You could also handle TypeToolUse here for partial tool blocks in
		// more complex streaming agent loops.
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n\n[Stream finished]")
}
