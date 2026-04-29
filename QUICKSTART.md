# Quickstart

`gocode` is a small Go library for LLM calls, tools, and agent loops.

The goal is simple:

> Plain data. Plain functions. No framework magic.

You own the conversation history. You own the tools. You own the loop.

This guide gets you from zero to a working model call, then adds one simple tool loop. For provider details, streaming, retries, testing, and production patterns, see `README.md`.

## Install

~~~bash
go get github.com/lukemuz/gocode/agent
~~~

Set an API key. This guide uses Anthropic:

~~~bash
export ANTHROPIC_API_KEY=sk-...
~~~

OpenAI and OpenRouter work similarly. See `README.md` for provider setup.

## 1. Make one LLM call

Create `main.go`:

~~~go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/lukemuz/gocode/agent"
)

func main() {
	ctx := context.Background()

	provider, err := agent.NewAnthropicProvider(agent.AnthropicConfig{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
	})
	if err != nil {
		log.Fatal(err)
	}

	client, err := agent.New(agent.Config{
		Provider:  provider,
		Model:     agent.ModelSonnet,
		MaxTokens: 1024,
	})
	if err != nil {
		log.Fatal(err)
	}

	system := "You are a concise assistant."

	history := []agent.Message{
		agent.NewUserMessage("Give me three practical ideas for using LLMs in a Go service."),
	}

	reply, err := client.Ask(ctx, system, history)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(agent.TextContent(reply))
}
~~~

Run it:

~~~bash
go run .
~~~

That is the smallest useful shape:

1. create a provider
2. create a client
3. create message history
4. call `Ask`

The important part is that `history` is just data:

~~~go
history := []agent.Message{
	agent.NewUserMessage("Give me three practical ideas for using LLMs in a Go service."),
}
~~~

There is no hidden session or framework state. Store it, trim it, append to it, or test it however you want.

## 2. Add one tool

Now give the model a tool.

Tools have two parts:

1. a definition the model sees
2. a Go function you run

This example gives the model access to the current time.

Replace `main.go` with:

~~~go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/lukemuz/gocode/agent"
)

func main() {
	ctx := context.Background()

	provider, err := agent.NewAnthropicProvider(agent.AnthropicConfig{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
	})
	if err != nil {
		log.Fatal(err)
	}

	client, err := agent.New(agent.Config{
		Provider:  provider,
		Model:     agent.ModelSonnet,
		MaxTokens: 1024,
	})
	if err != nil {
		log.Fatal(err)
	}

	nowTool, err := agent.NewTool("now", "Get the current time.", agent.Object())
	if err != nil {
		log.Fatal(err)
	}

	tools := []agent.Tool{nowTool}

	dispatch := map[string]agent.ToolFunc{
		"now": func(ctx context.Context, input json.RawMessage) (string, error) {
			return time.Now().Format(time.RFC3339), nil
		},
	}

	system := "You are a concise assistant. Use tools when they help."

	history := []agent.Message{
		agent.NewUserMessage("What time is it? Then give me a one-sentence suggestion for what to work on next."),
	}

	result, err := client.Loop(ctx, system, history, tools, dispatch, 5)
	if err != nil {
		log.Fatal(err)
	}

	last := result.Messages[len(result.Messages)-1]
	fmt.Println(agent.TextContent(last))
}
~~~

Run it:

~~~bash
go run .
~~~

The model can now decide to call `now`.

`Loop` handles the cycle:

1. call the model
2. detect requested tool calls
3. run your Go function from `dispatch`
4. append the tool result to the message history
5. call the model again
6. stop when the model returns a final answer

The result contains the full updated history:

~~~go
result.Messages
~~~

You still own that data.

## 3. A tool with input

The `now` tool has no input. Tools with input use JSON schema for the model-facing definition and `json.RawMessage` for the Go handler.

Here is a small calculator tool definition:

~~~go
calculator, err := agent.NewTool("calculator", "Do basic arithmetic.", agent.Object(
	agent.String("operation", "One of: add, subtract, multiply", agent.Required(), agent.Enum("add", "subtract", "multiply")),
	agent.Number("a", "First number", agent.Required()),
	agent.Number("b", "Second number", agent.Required()),
))
if err != nil {
	log.Fatal(err)
}
~~~

And the matching handler:

~~~go
dispatch := map[string]agent.ToolFunc{
	"calculator": func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Operation string  `json:"operation"`
			A         float64 `json:"a"`
			B         float64 `json:"b"`
		}

		if err := json.Unmarshal(input, &params); err != nil {
			return "", err
		}

		switch params.Operation {
		case "add":
			return fmt.Sprintf("%f", params.A+params.B), nil
		case "subtract":
			return fmt.Sprintf("%f", params.A-params.B), nil
		case "multiply":
			return fmt.Sprintf("%f", params.A*params.B), nil
		default:
			return "", fmt.Errorf("unknown operation: %s", params.Operation)
		}
	},
}
~~~

This is intentionally explicit:

- the model sees exactly the schema you provide (now built ergonomically with `Object`/`String`/`Number`/`Required`/`Enum`)
- your Go code parses exactly the input you expect
- there is no reflection-based magic
- the dispatch table is a normal Go map

The schema builders reduce verbosity for the model-facing definition while keeping everything fully transparent, inspectable, and explicit. The low-level `InputSchema` path remains available. See `TypedToolFunc` and the schema helpers in `agent/tool.go` for further ergonomics (pre-built tools next).

## 4. When to use `Ask` vs `Loop`

Use `Ask` when you want one model response:

~~~go
reply, err := client.Ask(ctx, system, history)
~~~

Use `Loop` when the model can call tools:

~~~go
result, err := client.Loop(ctx, system, history, tools, dispatch, 5)
~~~

Use `AskStream` or `LoopStream` when you want streaming output.

See `README.md` for streaming examples.

## 5. Production notes

This guide keeps the first path short. In real services, you will usually also want:

- `context.WithTimeout`
- retry configuration
- structured logging
- token usage tracking
- conversation trimming
- typed error handling with `errors.Is` and `errors.As`
- tests with a custom mock `Provider`

The core shape stays the same:

~~~go
history := []agent.Message{
	agent.NewUserMessage("..."),
}

result, err := client.Loop(ctx, system, history, tools, dispatch, maxIterations)
~~~

You can persist `history`, inspect it, trim it, replay it in tests, or build your own loop around `Ask`.

## Next steps

- Read `README.md` for providers, streaming, retries, errors, testing, and configuration.
- Run the examples in `examples/`.
- Try adding one tool from your own application.
- Keep the loop boring and explicit.

That is the point of `gocode`: useful LLM workflows in Go without giving up control of your program.