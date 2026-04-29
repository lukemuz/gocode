# Quickstart

`gocode` is a small Go library for LLM calls, tools, and agent loops.

> Plain data. Plain functions. No framework magic.

This guide gets you from zero to one model call, then one tool loop. For the full reference, see [`README.md`](README.md).

## Install

~~~bash
go get github.com/lukemuz/gocode/agent
~~~

This guide uses Anthropic:

~~~bash
export ANTHROPIC_API_KEY=sk-ant-...
~~~

OpenAI and OpenRouter work similarly; see the README for provider setup.

## 1. Make one LLM call

Create `main.go`:

~~~go
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

    history := []agent.Message{
        agent.NewUserMessage("Give me three practical ideas for using LLMs in a Go service."),
    }

    reply, err := client.Ask(ctx, "You are a concise assistant.", history)
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

The important part is that `history` is just data. There is no hidden session or framework state. Store it, append to it, trim it, or test it however you want.

## 2. Continue the conversation

`Ask` does not mutate history. Append the reply yourself:

~~~go
history = append(history, reply)
history = append(history, agent.NewUserMessage("Pick the most practical idea and explain the first implementation step."))

reply, err = client.Ask(ctx, "You are a concise assistant.", history)
~~~

That is the core data model: `[]agent.Message` in, `agent.Message` out.

## 3. Add one tool

Tools have two parts:

1. a definition the model sees
2. a Go function your program runs

Use the built-in clock tool for the first loop:

~~~go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/lukemuz/gocode/agent"
    "github.com/lukemuz/gocode/agent/tools/clock"
)

func main() {
    ctx := context.Background()

    client, err := agent.NewAnthropicClientFromEnv(agent.ModelSonnet)
    if err != nil {
        log.Fatal(err)
    }

    clockTool := clock.New()
    toolset := clockTool.Toolset()

    history := []agent.Message{
        agent.NewUserMessage("What time is it? Then give me one sentence about what to work on next."),
    }

    result, err := client.Loop(
        ctx,
        "You are a concise assistant. Use tools when they help.",
        history,
        toolset.Tools(),
        toolset.Dispatch(),
        5,
    )
    if err != nil {
        log.Fatal(err)
    }

    last := result.Messages[len(result.Messages)-1]
    fmt.Println(agent.TextContent(last))
}
~~~

`Loop` handles the cycle:

1. call the model
2. detect requested tool calls
3. run the matching Go function
4. append tool results to the message history
5. call the model again
6. stop when the model returns a final answer

You still own the resulting history:

~~~go
history = result.Messages
~~~

## 4. Define your own typed tool

For application tools, use `NewTypedTool` to pair a model-facing schema with a typed Go handler.

~~~go
type CalculatorInput struct {
    Operation string  `json:"operation"`
    A         float64 `json:"a"`
    B         float64 `json:"b"`
}

calculator, calculatorFunc, err := agent.NewTypedTool(
    "calculator",
    "Do basic arithmetic.",
    agent.Object(
        agent.String("operation", "One of: add, subtract, multiply", agent.Required(), agent.Enum("add", "subtract", "multiply")),
        agent.Number("a", "First number", agent.Required()),
        agent.Number("b", "Second number", agent.Required()),
    ),
    func(ctx context.Context, in CalculatorInput) (string, error) {
        switch in.Operation {
        case "add":
            return fmt.Sprintf("%g", in.A+in.B), nil
        case "subtract":
            return fmt.Sprintf("%g", in.A-in.B), nil
        case "multiply":
            return fmt.Sprintf("%g", in.A*in.B), nil
        default:
            return "", fmt.Errorf("unknown operation: %s", in.Operation)
        }
    },
)
if err != nil {
    log.Fatal(err)
}

_ = calculator
_ = calculatorFunc
~~~

Use it like any other tool:

~~~go
tools := []agent.Tool{calculator}
dispatch := map[string]agent.ToolFunc{calculator.Name: calculatorFunc}
~~~

This stays explicit:

- the model sees the schema you provide
- your handler receives a typed Go value
- dispatch is a normal map
- there is no reflection-only registry or hidden runtime

## 5. Compose built-in tools

Built-in tools expose `Toolset` values, so they compose cleanly.

~~~go
clockTool := clock.New()
ws, err := workspace.NewReadOnly(workspace.Config{Root: "."})
if err != nil {
    log.Fatal(err)
}

toolset, err := agent.Join(clockTool.Toolset(), ws.Toolset())
if err != nil {
    log.Fatal(err)
}

toolset = toolset.Wrap(
    agent.WithTimeout(5*time.Second),
    agent.WithResultLimit(20_000),
)
~~~

Use `workspace.NewReadOnly` for safe filesystem reads. Use `workspace.New` only when you want the `edit_file` tool, and consider adding `agent.WithConfirmation` before writes can run.

## 6. Use an Assistant when the glue repeats

`Assistant` is the practical middle path. It bundles a client, prompt, toolset, context manager, and loop limit into one step.

~~~go
a := agent.Assistant{
    Client:  client,
    System:  "You are a helpful assistant.",
    Tools:   toolset,
    Context: agent.ContextManager{MaxTokens: 8000, KeepRecent: 20},
    MaxIter: 10,
}

result, err := a.Step(ctx, history)
if err != nil {
    log.Fatal(err)
}

history = result.Messages
~~~

It is just a thin helper over:

~~~go
trimmed, err := a.Context.Trim(ctx, history)
result, err := client.Loop(ctx, a.System, trimmed, a.Tools.Tools(), a.Tools.Dispatch(), a.MaxIter)
~~~

No persistence, runner, scheduler, or hidden lifecycle is introduced.

## Ask vs Loop vs Assistant

Use the smallest shape that solves the problem:

| Need | Use |
|---|---|
| One model response | `Ask` |
| Streaming one response | `AskStream` |
| Model can call tools | `Loop` |
| Streaming tool loop | `LoopStream` |
| Repeated assistant glue | `Assistant.Step` or `Assistant.StepStream` |
| Independent fan-out work | `Parallel` |

## Production notes

In real services, you will usually add:

- `context.WithTimeout`
- retry configuration
- structured logging
- token usage tracking
- conversation trimming
- typed error handling with `errors.Is` and `errors.As`
- tests with a custom mock `Provider`
- explicit persistence for `[]agent.Message`

The core shape stays the same:

~~~go
history := []agent.Message{agent.NewUserMessage("...")}
result, err := client.Loop(ctx, system, history, tools, dispatch, maxIterations)
history = result.Messages
~~~

## Next steps

- Read [`README.md`](README.md) for providers, streaming, retries, errors, testing, MCP, and package layout.
- Read [`VISION.md`](VISION.md) to understand the design philosophy.
- Check [`ROADMAP.md`](ROADMAP.md) for current priorities.
- Run the examples in `examples/`.
- Try adding one tool from your own application.

That is the point of `gocode`: useful LLM workflows in Go without giving up control of your program.
