# Quickstart

`gocode` is a small Go library for LLM calls, tools, and agent loops. This guide gets you from zero to a model call to a tool loop. For the full reference see [`README.md`](README.md).

## Install

~~~bash
go get github.com/lukemuz/gocode/agent
export ANTHROPIC_API_KEY=sk-ant-...
~~~

OpenAI and OpenRouter work the same way; see the README.

## 1. One model call

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

    reply, err := client.Ask(ctx, "You are concise.", history)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(agent.TextContent(reply))
}
~~~

`history` is just data. There is no hidden session.

## 2. Continue the conversation

`Ask` does not mutate history. Append the reply yourself:

~~~go
history = append(history, reply)
history = append(history, agent.NewUserMessage("Pick the most practical idea."))
reply, err = client.Ask(ctx, "You are concise.", history)
~~~

That is the core data model: `[]agent.Message` in, `agent.Message` out.

## 3. Add a tool

A tool is a model-facing definition plus a Go function. The built-in clock tool is the smallest example:

~~~go
import "github.com/lukemuz/gocode/agent/tools/clock"

clockTool := clock.New()
toolset := clockTool.Toolset()

history := []agent.Message{
    agent.NewUserMessage("What time is it? Then suggest one thing to work on next."),
}

result, err := client.Loop(ctx, "Use tools when they help.", history, toolset, 5)
if err != nil {
    log.Fatal(err)
}
fmt.Println(result.FinalText())
~~~

`Loop` calls the model, runs requested tools, appends results, and repeats until a final answer or the iteration limit. You still own the resulting history via `result.Messages`.

## 4. Define your own typed tool

~~~go
type CalculatorInput struct {
    Operation string  `json:"operation"`
    A, B      float64 `json:"a" json:"b"`
}

calc, calcFn, err := agent.NewTypedTool(
    "calculator",
    "Do basic arithmetic.",
    agent.Object(
        agent.String("operation", "add, subtract, multiply", agent.Required(), agent.Enum("add", "subtract", "multiply")),
        agent.Number("a", "First number", agent.Required()),
        agent.Number("b", "Second number", agent.Required()),
    ),
    func(ctx context.Context, in CalculatorInput) (string, error) {
        switch in.Operation {
        case "add":      return fmt.Sprintf("%g", in.A+in.B), nil
        case "subtract": return fmt.Sprintf("%g", in.A-in.B), nil
        case "multiply": return fmt.Sprintf("%g", in.A*in.B), nil
        }
        return "", fmt.Errorf("unknown op: %s", in.Operation)
    },
)
~~~

Use it in a `Toolset`:

~~~go
tools := agent.Toolset{Bindings: []agent.ToolBinding{{Tool: calc, Func: calcFn}}}
~~~

The model sees the schema; your handler receives a typed Go value; dispatch is a normal map. No reflection registry, no hidden runtime.

## 5. Compose built-ins and middleware

~~~go
ws, err := workspace.NewReadOnly(workspace.Config{Root: "."})
if err != nil {
    log.Fatal(err)
}

tools := agent.MustJoin(clockTool.Toolset(), ws.Toolset()).Wrap(
    agent.WithTimeout(5*time.Second),
    agent.WithResultLimit(20_000),
)
~~~

Use `workspace.NewReadOnly` for safe filesystem reads. `workspace.New` includes `edit_file` — wrap it with `agent.WithConfirmation` before letting writes run.

## 6. Use Assistant when the glue repeats

`Assistant` bundles a client, system prompt, toolset, context manager, and iteration cap.

~~~go
a := agent.Assistant{
    Client:  client,
    System:  "You are a helpful assistant.",
    Tools:   tools,
    Context: agent.ContextManager{MaxTokens: 8000, KeepRecent: 20},
    MaxIter: 10,
}

result, err := a.Step(ctx, history)
history = result.Messages
~~~

It is equivalent to:

~~~go
trimmed, _ := a.Context.Trim(ctx, history)
result, _ := client.Loop(ctx, a.System, trimmed, a.Tools, a.MaxIter)
~~~

No persistence, runner, scheduler, or hidden lifecycle.

## Ask vs Loop vs Assistant

| Need | Use |
|---|---|
| One response | `Ask` |
| Streaming response | `AskStream` |
| Model can call tools | `Loop` |
| Streaming tool loop | `LoopStream` |
| Repeated assistant glue | `Assistant.Step` / `Assistant.StepStream` |
| Independent fan-out | `Parallel` |

## Next steps

- [`README.md`](README.md) for providers, streaming, retries, errors, sessions, MCP, and testing.
- [`VISION.md`](VISION.md) for the design philosophy.
- [`examples/recipes/`](examples/recipes/) for runnable patterns.
