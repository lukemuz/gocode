# gocode

`gocode` is a small Go library for LLM calls, tools, and agent loops.

> Plain data. Plain functions. No framework magic.

It scales from one model call to practical tool-using assistants without forcing a framework-shaped runtime onto simple programs.

The core promise:

> You own the data. You own the tools. You own the loop.

`gocode` gives you:

- model calls with `Ask` and `AskStream`
- tool loops with `Loop` and `LoopStream`
- plain `[]Message` conversation history
- normal Go functions as tools
- provider implementations for Anthropic, OpenAI, and OpenRouter
- retries, typed errors, streaming, usage tracking, and tests
- safe built-in tools, toolsets, middleware, context management, MCP, and a thin `Assistant` block

Requires Go 1.21+. No external dependencies in the core package.

## When to use it

Use `gocode` when you want:

- one-off LLM calls without ceremony
- tool use without a heavyweight runtime
- practical assistants with visible control flow
- explicit conversation history and context management
- easy testing through interfaces
- Go-native code that is easy for humans and coding agents to inspect

The project is not anti-convenience. It is anti-trap: helpers should compress boilerplate without hiding model calls, tool execution, memory mutation, persistence, or application lifecycle.

Complex workflows are built with Go control flow, not hidden orchestration. Run loops in goroutines, compose outputs with normal function calls, and keep the data visible at every step.

For the longer philosophy, see [`VISION.md`](VISION.md). For future work, see [`ROADMAP.md`](ROADMAP.md).

## Install

~~~bash
go get github.com/lukemuz/gocode/agent
~~~

Set an API key for the provider you want to use:

~~~bash
export ANTHROPIC_API_KEY=sk-ant-...
export OPENAI_API_KEY=sk-...
export OPENROUTER_API_KEY=sk-or-...
~~~

## Quickstart

For the guided first-run path, see [`QUICKSTART.md`](QUICKSTART.md).

The smallest useful call looks like this:

~~~go
client, err := agent.NewAnthropicClientFromEnv(agent.ModelSonnet)
if err != nil {
    log.Fatal(err)
}

history := []agent.Message{
    agent.NewUserMessage("Give me three practical ideas for using LLMs in a Go service."),
}

reply, err := client.Ask(context.Background(), "You are concise.", history)
if err != nil {
    log.Fatal(err)
}

fmt.Println(agent.TextContent(reply))
~~~

No hidden session. No runner. `history` is just data.

## Core building blocks

### `Provider`

A `Provider` translates between `gocode`'s canonical data model and an LLM API.

~~~go
type Provider interface {
    Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error)
    Stream(ctx context.Context, req ProviderRequest, onDelta func(ContentBlock)) (ProviderResponse, error)
}
~~~

Included providers:

- Anthropic
- OpenAI
- OpenRouter

You can implement this interface for any backend.

### `Client`

A `Client` holds provider, model, token limit, and retry configuration. It does not store conversation state.

~~~go
client, err := agent.New(agent.Config{
    Provider:  provider,
    Model:     agent.ModelSonnet,
    MaxTokens: 4096,
})
~~~

The same client can be reused across conversations, HTTP requests, jobs, and goroutines.

### `Message`

Conversation history is plain data:

~~~go
history := []agent.Message{
    agent.NewUserMessage("Hello"),
}
~~~

Append replies yourself when you want to continue:

~~~go
reply, err := client.Ask(ctx, system, history)
history = append(history, reply)
history = append(history, agent.NewUserMessage("Tell me more."))
~~~

### `Tool` and `ToolFunc`

A tool has two parts:

1. a model-facing definition
2. a Go function your program runs

~~~go
tool, fn, err := agent.NewTypedTool(
    "calculator",
    "Do basic arithmetic.",
    agent.Object(
        agent.String("operation", "add, subtract, multiply, or divide", agent.Required()),
        agent.Number("a", "First number", agent.Required()),
        agent.Number("b", "Second number", agent.Required()),
    ),
    func(ctx context.Context, in CalculatorInput) (string, error) {
        return calculate(in)
    },
)
~~~

Tools still compile down to ordinary values:

~~~go
tools := []agent.Tool{tool}
dispatch := map[string]agent.ToolFunc{tool.Name: fn}
~~~

There is no hidden registry.

## Three usage tiers

### Tier 1: one model call

Use `Ask` when you want one response.

~~~go
reply, err := client.Ask(ctx, system, history)
~~~

### Tier 2: parallel steps

Use `Parallel` for independent fan-out/fan-in work.

~~~go
results := agent.Parallel(ctx,
    func(ctx context.Context) (string, error) { return summarize(ctx, client, "Rome") },
    func(ctx context.Context) (string, error) { return summarize(ctx, client, "Athens") },
)
~~~

It uses goroutines. It is a helper, not a scheduler.

### Tier 3: tool loop

Use `Loop` when the model can call tools.

~~~go
result, err := client.Loop(ctx, system, history, tools, dispatch, 5)
if err != nil {
    log.Fatal(err)
}

history = result.Messages
~~~

`Loop` calls the model, runs requested tools, appends tool results, and repeats until the model returns a final answer or the iteration limit is reached.

Multiple tool calls requested in the same model turn run concurrently and are returned to the model in the original order.

### Composing loops

Because `Ask`, `Loop`, and `Assistant.Step` are ordinary Go calls over plain data, they compose like any other functions. For example, you can run two independent tool-using loops at the same time, then feed their outputs into a later synthesis call.

~~~go
type SubagentOutput struct {
    Name string
    Text string
}

results := agent.Parallel(ctx,
    func(ctx context.Context) (SubagentOutput, error) {
        result, err := client.Loop(
            ctx,
            "You are a research assistant. Use your tools to gather facts.",
            []agent.Message{agent.NewUserMessage(task)},
            researchTools.Tools(),
            researchTools.Dispatch(),
            8,
        )
        if err != nil {
            return SubagentOutput{}, err
        }

        last := result.Messages[len(result.Messages)-1]
        return SubagentOutput{Name: "research", Text: agent.TextContent(last)}, nil
    },
    func(ctx context.Context) (SubagentOutput, error) {
        result, err := client.Loop(
            ctx,
            "You are an implementation assistant. Use your tools to inspect code.",
            []agent.Message{agent.NewUserMessage(task)},
            codeTools.Tools(),
            codeTools.Dispatch(),
            8,
        )
        if err != nil {
            return SubagentOutput{}, err
        }

        last := result.Messages[len(result.Messages)-1]
        return SubagentOutput{Name: "implementation", Text: agent.TextContent(last)}, nil
    },
)

for _, r := range results {
    if r.Err != nil {
        log.Fatal(r.Err)
    }
}

synthesisHistory := []agent.Message{
    agent.NewUserMessage(fmt.Sprintf(`Original task:

%s

Research output:

%s

Implementation output:

%s

Synthesize these into one final answer.`,
        task,
        results[0].Value.Text,
        results[1].Value.Text,
    )),
}

final, err := client.Ask(ctx, "You synthesize multiple agent outputs.", synthesisHistory)
~~~

The parallel branches can use different prompts, histories, tools, models, or clients. The synthesis step can be a simple `Ask`, as shown here, or another `Loop` if the synthesizer also needs tools.

## Practical assembly

The primitive APIs remain available, but common agent assembly has helpers.

### Toolsets and middleware

`Toolset` bundles tool definitions with their implementations and metadata.

~~~go
toolset, err := agent.Join(clockTool.Toolset(), workspaceToolset)
if err != nil {
    log.Fatal(err)
}

toolset = toolset.Wrap(
    agent.WithTimeout(5*time.Second),
    agent.WithResultLimit(20_000),
    agent.WithConfirmation(confirm),
)

result, err := client.Loop(ctx, system, history, toolset.Tools(), toolset.Dispatch(), 10)
~~~

Available middleware:

- `WithTimeout`
- `WithResultLimit`
- `WithLogging`
- `WithPanicRecovery`
- `WithConfirmation`

Metadata is advisory and inspectable. Your application decides what policy to enforce.

### Context management

`ContextManager` trims history explicitly before a call or assistant step.

~~~go
cm := agent.ContextManager{
    MaxTokens:  8000,
    KeepFirst:  1,
    KeepRecent: 20,
}

trimmed, err := cm.Trim(ctx, history)
result, err := client.Loop(ctx, system, trimmed, tools, dispatch, 10)
~~~

The original history is not mutated. Tool-use/tool-result integrity is preserved. Summarization happens only if you configure a summarizer.

### Assistant

`Assistant` is the blessed middle path: a thin block that wires together a client, system prompt, toolset, context manager, iteration limit, and hooks.

~~~go
a := agent.Assistant{
    Client:  client,
    System:  "You are a helpful assistant.",
    Tools:   toolset,
    Context: agent.ContextManager{MaxTokens: 8000, KeepRecent: 20},
    MaxIter: 10,
}

result, err := a.Step(ctx, history)
history = result.Messages
~~~

It is equivalent to:

~~~go
trimmed, err := a.Context.Trim(ctx, history)
result, err := a.Client.Loop(ctx, a.System, trimmed, a.Tools.Tools(), a.Tools.Dispatch(), a.MaxIter)
~~~

No persistence, scheduler, runner, global registry, or hidden application lifecycle is introduced.

## Built-in tools

Built-ins are opt-in Lego blocks. They expose normal `agent.Tool`, `agent.ToolFunc`, and `agent.Toolset` values.

Current packages:

| Package | Tools |
|---|---|
| `agent/tools/clock` | current UTC time |
| `agent/tools/math` | safe calculator |
| `agent/tools/workspace` | sandboxed list, find, search, read, file info, and exact-string edit |

Example:

~~~go
clockTool := clock.New()
ws, err := workspace.NewReadOnly(workspace.Config{Root: "."})
if err != nil {
    log.Fatal(err)
}

toolset, err := agent.Join(clockTool.Toolset(), ws.Toolset())
~~~

Use `workspace.NewReadOnly` for read-only filesystem access. Use `workspace.New` only when you want the `edit_file` tool, and consider wrapping it with `WithConfirmation`.

## MCP

`agent/mcp` adapts Model Context Protocol tools into ordinary `gocode` toolsets.

~~~go
srv, err := mcp.Connect(ctx, mcp.Config{Command: "my-mcp-server"})
if err != nil {
    log.Fatal(err)
}
defer srv.Close()

mcpTools, err := srv.Toolset(ctx)
result, err := client.Loop(ctx, system, history, mcpTools.Tools(), mcpTools.Dispatch(), 10)
~~~

MCP support is explicit: you choose the server, inspect the tools, and pass them into the loop.

## Streaming

Use `AskStream` for one streamed response:

~~~go
_, err := client.AskStream(ctx, system, history, func(delta agent.ContentBlock) {
    if delta.Type == agent.TypeText {
        fmt.Print(delta.Text)
    }
})
~~~

Use `LoopStream` or `Assistant.StepStream` for streamed tool loops.

Streaming callbacks fire synchronously as provider deltas arrive. Because retries can restart a stream after partial output, callbacks may see partial text from failed attempts. See the roadmap for the planned helper and recipe around this behavior.

## Errors and retries

Retries are built in for transient API failures such as rate limits, temporary network errors, and 5xx responses.

~~~go
client, err := agent.New(agent.Config{
    Provider:  provider,
    Model:     agent.ModelSonnet,
    MaxTokens: 4096,
    Retry: agent.RetryConfig{
        MaxRetries:  5,
        InitialWait: time.Second,
        MaxWait:     30 * time.Second,
    },
})
~~~

Errors are typed and work with `errors.Is` and `errors.As`.

Common cases:

- `APIError`
- `ToolError`
- `LoopError`
- `RetryExhaustedError`
- `ErrMissingTool`
- `ErrMaxIter`

Tool execution errors are soft by default: the error is sent back to the model as a tool result with `IsError: true`. Missing tools are treated as configuration errors.

## Testing

The `Provider` interface is the main testing seam. You can test calls, loops, streaming behavior, tool execution, history shape, usage accounting, and errors without real API calls.

Run tests:

~~~bash
go test ./agent/...
~~~

Good tests usually assert contracts rather than exact LLM prose:

- messages appended in the expected order
- tool calls produce expected tool results
- errors match with `errors.Is` and `errors.As`
- callbacks fire in expected order
- usage is accumulated
- partial history is inspectable on failure

## Examples

~~~bash
# Single model call
go run ./examples/ask

# Parallel and sequential composition
go run ./examples/pipeline

# Tool-using loop
go run ./examples/agent

# Streaming output
go run ./examples/stream
~~~

Set the relevant API key first.

## Package layout

~~~text
agent/
  agent.go                  Client, Ask, AskStream, Loop, LoopStream, Config
  assistant.go              Assistant, Hooks
  context.go                ContextManager
  provider.go               Provider, ProviderRequest, ProviderResponse
  anthropic.go              Anthropic provider and env helpers
  openai.go                 OpenAI provider and env helpers
  openrouter.go             OpenRouter provider and env helpers
  message.go                Message, ContentBlock, helpers
  tool.go                   Tool, ToolFunc, typed tools, schema helpers
  toolset.go                ToolBinding, Toolset, middleware
  parallel.go               Parallel[T]
  retry.go                  RetryConfig and retry helpers
  errors.go                 typed errors
  mcp/                      MCP adapter
  tools/clock/              current time tool
  tools/math/               calculator tool
  tools/workspace/          sandboxed workspace tools
examples/
  ask/
  pipeline/
  agent/
  stream/
~~~

## Roadmap summary

Completed foundation:

- providers, core model calls, tool loops, streaming, retries, typed errors
- parallel tool execution
- typed tool helpers and schema builders
- safe built-in clock/math/workspace tools
- toolsets and middleware
- explicit context management
- basic assistant block
- MCP adapter

Next focus:

1. streaming retry helper and documentation
2. recipe-style documentation
3. repo explainer example app
5. production helpers: sessions, durable tool execution, observability, extended model config, testing helpers, and HTTP/SSE example

See [`ROADMAP.md`](ROADMAP.md) for details.

## Non-goals

`gocode` should not become:

- a graph executor
- a visual workflow builder
- a managed agent platform
- a no-code agent configuration system
- a hidden scheduler
- a deployment framework
- a vector database
- a global tool registry
- a cross-session memory platform in the core package
- a replacement for your application architecture

Higher-level systems can be built on top of `gocode`. The library itself should remain small, explicit, composable, and easy to reason about.
