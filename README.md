# gocode

A small Go library for LLM calls, tools, and agent loops.

> Plain data. Plain functions. No framework magic.
>
> You own the data. You own the tools. You own the loop.

`gocode` scales from one model call to practical tool-using assistants without forcing a framework-shaped runtime onto simple programs.

It gives you:

- `Ask` and `AskStream` for model calls
- `Loop` and `LoopStream` for tool-using loops
- plain `[]Message` history and normal Go functions as tools
- providers for Anthropic, OpenAI, and OpenRouter
- typed tools, schema helpers, toolsets, middleware, context management, MCP, and a thin `Assistant` block
- safe built-in tools (clock, math, sandboxed workspace)
- session persistence with a five-method `Store` interface
- retries, typed errors, streaming, usage tracking

Requires Go 1.21+. No external dependencies in the core package.

## Install

~~~bash
go get github.com/lukemuz/gocode/agent
~~~

Set an API key for the provider you want:

~~~bash
export ANTHROPIC_API_KEY=sk-ant-...
export OPENAI_API_KEY=sk-...
export OPENROUTER_API_KEY=sk-or-...
~~~

## The smallest useful call

~~~go
client, err := agent.NewAnthropicClientFromEnv(agent.ModelSonnet)
if err != nil {
    log.Fatal(err)
}

history := []agent.Message{
    agent.NewUserMessage("Give me three practical ideas for using LLMs in a Go service."),
}

reply, _, err := client.Ask(context.Background(), "You are concise.", history)
if err != nil {
    log.Fatal(err)
}

fmt.Println(agent.TextContent(reply))
~~~

No hidden session. No runner. `history` is just data.

For a step-by-step walkthrough see [`QUICKSTART.md`](QUICKSTART.md). For the design philosophy see [`VISION.md`](VISION.md). For an honest comparison with Google's ADK see [`COMPARISON.md`](COMPARISON.md).

## Core building blocks

### `Provider`

A `Provider` translates between `gocode`'s data model and an LLM API.

~~~go
type Provider interface {
    Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error)
    Stream(ctx context.Context, req ProviderRequest, onDelta func(ContentBlock)) (ProviderResponse, error)
}
~~~

Anthropic, OpenAI, and OpenRouter are included. Any backend can implement the interface.

### `Client`

A `Client` holds provider, model, token limit, and retry config. It does not store conversation state, so the same client reuses across conversations, requests, jobs, and goroutines.

~~~go
client, err := agent.New(agent.Config{
    Provider:  provider,
    Model:     agent.ModelSonnet,
    MaxTokens: 4096,
})
~~~

### `Message`

Conversation history is plain data. Append replies yourself when you want to continue:

~~~go
history := []agent.Message{agent.NewUserMessage("Hello")}

reply, _, err := client.Ask(ctx, system, history)
history = append(history, reply, agent.NewUserMessage("Tell me more."))
~~~

### `Tool` and `ToolFunc`

A tool has two parts: a model-facing definition and a Go function.

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

Tools compile down to ordinary values. A `Toolset` is an ordered slice of `ToolBinding{Tool, Func, Meta}` you can build literally:

~~~go
tools := agent.Toolset{Bindings: []agent.ToolBinding{{Tool: tool, Func: fn}}}
~~~

No hidden registry.

## Three usage tiers

### Tier 1: one model call

~~~go
reply, usage, err := client.Ask(ctx, system, history)
~~~

`usage` reports the input/output tokens for that call so cost-conscious code doesn't have to drop down to `Loop`.

### Tier 2: parallel fan-out

~~~go
results := agent.Parallel(ctx,
    func(ctx context.Context) (string, error) { return summarize(ctx, client, "Rome") },
    func(ctx context.Context) (string, error) { return summarize(ctx, client, "Athens") },
)
~~~

It uses goroutines. It is a helper, not a scheduler.

### Tier 3: tool loop

~~~go
result, err := client.Loop(ctx, system, history, tools, 5)
history = result.Messages
fmt.Println(result.FinalText())
~~~

`Loop` calls the model, runs requested tools, appends tool results, and repeats until the model returns a final answer or the iteration limit. Multiple tool calls requested in one model turn run concurrently and return in original order.

Because `Ask`, `Loop`, and `Assistant.Step` are ordinary calls over plain data, they compose like any Go function — run two tool-using loops in parallel, then synthesize their outputs with a later `Ask`.

## Practical assembly

### Toolsets and middleware

~~~go
toolset := agent.MustJoin(clockTool.Toolset(), workspaceToolset).Wrap(
    agent.WithTimeout(5*time.Second),
    agent.WithResultLimit(20_000),
    agent.WithConfirmation(confirm),
)

result, err := client.Loop(ctx, system, history, toolset, 10)
~~~

`MustJoin` is for static composition where a duplicate tool name is a programmer error. `Join` returns an error for dynamic composition.

Available middleware: `WithTimeout`, `WithResultLimit`, `WithLogging`, `WithPanicRecovery`, `WithConfirmation`. Metadata is advisory; your application decides policy.

### Context management

`ContextManager` trims history explicitly before a call.

~~~go
cm := agent.ContextManager{MaxTokens: 8000, KeepFirst: 1, KeepRecent: 20}
trimmed, err := cm.Trim(ctx, history)
~~~

The original history is not mutated. Tool-use/tool-result integrity is preserved. Summarization happens only if you configure a summarizer.

### Assistant

`Assistant` is the blessed middle path: a thin block over a client, prompt, toolset, context manager, iteration limit, and hooks.

~~~go
a := agent.Assistant{
    Client:  client,
    System:  "You are a helpful assistant.",
    Tools:   toolset,
    Context: agent.ContextManager{MaxTokens: 8000, KeepRecent: 20},
    MaxIter: 10,
}
result, err := a.Step(ctx, history)
~~~

Equivalent to `Trim` then `Loop`. No persistence, scheduler, runner, or hidden lifecycle.

## Built-in tools

| Package | Tools |
|---|---|
| `agent/tools/clock` | current UTC time |
| `agent/tools/math` | safe calculator |
| `agent/tools/workspace` | sandboxed list, find, search, read, file info, exact-string edit |

~~~go
clockTool := clock.New()
ws, err := workspace.NewReadOnly(workspace.Config{Root: "."})
toolset := agent.MustJoin(clockTool.Toolset(), ws.Toolset())
~~~

`workspace.NewReadOnly` is read-only. `workspace.New` includes `edit_file` — wrap it with `WithConfirmation` before letting writes run.

## MCP

`agent/mcp` adapts Model Context Protocol tools into ordinary toolsets.

~~~go
srv, err := mcp.Connect(ctx, mcp.Config{Command: "my-mcp-server"})
defer srv.Close()
mcpTools, err := srv.Toolset(ctx)
result, err := client.Loop(ctx, system, history, mcpTools, 10)
~~~

You choose the server, inspect the tools, and pass them in.

## Streaming

~~~go
_, _, err := client.AskStream(ctx, system, history, func(delta agent.ContentBlock) {
    if delta.Type == agent.TypeText {
        fmt.Print(delta.Text)
    }
})
~~~

Use `LoopStream` or `Assistant.StepStream` for streamed tool loops.

Retries can restart a stream after partial output, so callbacks may see partial text from failed attempts. Use `StreamBuffer` with `RetryConfig.OnRetry` to react and clear:

~~~go
sb := agent.NewStreamBuffer(
    func(b agent.ContentBlock) { fmt.Print(b.Text) },
    func() { fmt.Print("\n[retrying…]\n") },
)
client, _ := agent.New(agent.Config{..., Retry: agent.RetryConfig{OnRetry: sb.OnRetry}})
msg, _, err := client.AskStream(ctx, system, history, sb.OnToken)
~~~

## Sessions

`Session` is plain data. You load it, pass `History` to a model call, and persist the result yourself.

~~~go
sess, err := store.Get(ctx, sessionID)
if errors.Is(err, agent.ErrSessionNotFound) {
    sess = &agent.Session{ID: sessionID}
} else if err != nil {
    return err
}

sess.History = append(sess.History, agent.NewUserMessage(input))
result, err := assistant.Step(ctx, sess.History)
if err != nil {
    return err
}
sess.History = result.Messages

if len(sess.History) == 1 {
    err = store.Create(ctx, sess)
} else {
    err = store.Update(ctx, sess)
}
~~~

Two built-in stores: `MemoryStore` (in-memory, concurrent-safe) and `FileStore` (one JSON file per session, atomic writes). Both implement:

~~~go
type Store interface {
    Create(ctx context.Context, session *Session) error
    Get(ctx context.Context, id string) (*Session, error)
    Update(ctx context.Context, session *Session) error
    Delete(ctx context.Context, id string) error
    List(ctx context.Context, prefix string, limit int) ([]*Session, error)
}
~~~

`Create` returns `ErrSessionExists`; `Update` returns `ErrSessionNotFound`. Both work with `errors.Is`.

## Errors and retries

~~~go
client, err := agent.New(agent.Config{
    Provider:  provider,
    Model:     agent.ModelSonnet,
    MaxTokens: 4096,
    Retry: agent.RetryConfig{
        MaxRetries:  5,
        InitialWait: time.Second,
        MaxWait:     30 * time.Second,
        OnRetry: func(attempt int, wait time.Duration) {
            log.Printf("retry %d, waiting %s", attempt, wait)
        },
    },
})
~~~

Errors are typed and work with `errors.Is` / `errors.As`: `APIError`, `ToolError`, `LoopError`, `RetryExhaustedError`, `ErrMissingTool`, `ErrMaxIter`.

Tool execution errors are soft by default: the error returns to the model as a tool result with `IsError: true`. Missing tools are configuration errors.

## Testing

The `Provider` interface is the main testing seam. You can test calls, loops, streaming, tool execution, history shape, usage, and errors without real API calls.

~~~bash
go test ./agent/...
~~~

Good tests assert contracts (message order, tool calls, error types, callback order, usage accumulation), not exact LLM prose.

## Examples

Smaller examples in `examples/`:

~~~bash
go run ./examples/ask        # one model call
go run ./examples/pipeline   # parallel + sequential composition
go run ./examples/agent      # tool-using loop
go run ./examples/stream     # streaming
~~~

Larger runnable patterns in `examples/recipes/`:

- `01-assistant-with-tools` — curated toolset, middleware, context management
- `02-repo-explainer` — sandboxed workspace tools, streaming, file-backed sessions
- `04-router-subagents` — orchestrator delegates to specialist subagents
- `05-persistent-chat` — long-running conversation with `FileStore`

Set the relevant API key first.

## Non-goals

`gocode` will not become a graph executor, visual workflow builder, managed agent platform, no-code configurator, hidden scheduler, deployment framework, vector database, global tool registry, or cross-session memory platform in core. Higher-level systems can be built on top.

See [`ROADMAP.md`](ROADMAP.md) for forward-looking work.
