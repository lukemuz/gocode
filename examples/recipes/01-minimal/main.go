// Recipe 01-minimal: the smallest tool-using agent gocode can express
// with primitives alone. No streaming, no middleware, no context manager,
// no Agent block. Just Client + tools + Loop.
//
// This recipe exists as a baseline: how short can a useful agent be
// before any helpers are added? See 01-agent-with-tools for the
// production-shaped version that layers retries, streaming, middleware,
// and context management on top of the same primitives.
//
// Run:
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run ./examples/recipes/01-minimal "What time is it, and what is 17 * 23?"
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/lukemuz/gocode"
	"github.com/lukemuz/gocode/providers/anthropic"
	"github.com/lukemuz/gocode/tools/clock"
	"github.com/lukemuz/gocode/tools/math"
)

func main() {
	question := strings.TrimSpace(strings.Join(os.Args[1:], " "))
	if question == "" {
		log.Fatal(`usage: minimal "your question"`)
	}

	client, err := anthropic.NewClientFromEnv(gocode.ModelSonnet)
	if err != nil {
		log.Fatal(err)
	}

	tools := gocode.MustJoin(clock.New().Toolset(), math.New().Toolset())

	result, err := client.Loop(
		context.Background(),
		"You are a concise helper. Use your tools when they would give a more accurate answer than guessing.",
		[]gocode.Message{gocode.NewUserMessage(question)},
		tools,
		5,
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.FinalText())
}
