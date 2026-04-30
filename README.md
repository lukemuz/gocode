# gocode

A small Go library for LLM calls, tools, and agent loops.

> Plain data. Plain functions. No framework magic.
>
> You own the data. You own the tools. You own the loop.

`gocode` scales from one model call to practical tool-using assistants without forcing a framework-shaped runtime onto simple programs.

It gives you:

- `Ask` and `AskStream` for model calls
- `Loop` and `LoopStream` for tool-using loops
- `Extract[T]` for typed structured output (with or without intermediate tool use)
- plain `[]Message` history and normal Go functions as tools
- providers for Anthropic, OpenAI, and OpenRouter
- typed tools, schema helpers, toolsets, middleware, context management, MCP, and a thin `Agent` block
- safe built-in tools (clock, math, sandboxed workspace)
- session persistence with a five-method `Store` interface
- retries, typed errors, streaming, usage tracking

Requires Go 1.21+. No external dependencies in the core package.

## Install

~~~bash
go get github.com/lukemuz/gocode
~~~

Set an API key for the provider you want:

~~~bash
export ANTHROPIC_API_KEY=sk-ant-...
export OPENAI_API_KEY=sk-...
export OPENROUTER_API_KEY=sk-or-...
~~~

## The smallest useful call

~~~go
client, err := anthropic.NewClientFromEnv(gocode.ModelSonnet)
if err != nil {
    log.Fatal(err)
}

history := []gocode.Message{
    gocode.NewUserMessage("Give me three practical ideas for using LLMs in a Go service."),
}

reply, _, err := client.Ask(context.Background(), "You are concise.", history)
if err != nil {
    log.Fatal(err)
}

fmt.Println(gocode.TextContent(reply))
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

Anthropic, OpenAI Chat Completions, OpenAI Responses, and OpenRouter are included. Any backend can implement the interface.

### `Client`

A `Client` holds provider, model, token limit, and retry config. It does not store conversation state, so the same client reuses across conversations, requests, jobs, and goroutines.

~~~go
client, err := gocode.New(gocode.Config{
    Provider:  provider,
    Model:     gocode.ModelSonnet,
    MaxTokens: 4096,
})
~~~

### `Message`

Conversation history is plain data. Append replies yourself when you want to continue:

~~~go
history := []gocode.Message{gocode.NewUserMessage("Hello")}

reply, _, err := client.Ask(ctx, system, history)
history = append(history, reply, gocode.NewUserMessage("Tell me more."))
~~~

### `Tool` and `ToolFunc`

A tool has two parts: a model-facing definition and a Go function.

~~~go
tool, fn := gocode.NewTypedTool(
    "calculator",
    "Do basic arithmetic.",
    gocode.Object(
        gocode.String("operation", "add, subtract, multiply, or divide", gocode.Required()),
        gocode.Number("a", "First number", gocode.Required()),
        gocode.Number("b", "Second number", gocode.Required()),
    ),
    func(ctx context.Context, in CalculatorInput) (string, error) {
        return calculate(in)
    },
)
~~~

Tools compile down to ordinary values. A `Toolset` is an ordered slice of `ToolBinding{Tool, Func, Meta}`. `gocode.Tools(...)` and `gocode.Bind(tool, fn)` are variadic constructors for the common case:

~~~go
tools := gocode.Tools(
    gocode.Bind(tool, fn),
    gocode.Bind(other, otherFn),
)
~~~

No hidden registry.

## From one call to a tool loop

### One model call

~~~go
reply, usage, err := client.Ask(ctx, system, history)
~~~

`usage` reports input/output tokens so cost-conscious code doesn't have to drop down to `Loop`.

### Tool loop

~~~go
result, err := client.Loop(ctx, system, history, tools, 5)
history = result.Messages
fmt.Println(result.FinalText())
~~~

`Loop` calls the model, runs requested tools, appends tool results, and repeats until the model returns a final answer or the iteration limit. Multiple tool calls in one model turn run concurrently and return in original order.

Because `Ask`, `Loop`, and `Agent.Step` are ordinary calls over plain data, they compose like any Go function — run two tool-using loops in parallel with `gocode.Parallel`, then synthesize their outputs with a later `Ask`.

### Typed extraction

When you want a typed Go value back — with or without intermediate tool use — `Extract` runs a loop in which the model must call a single "submit" tool whose typed argument is the return value:

~~~go
type Plan struct {
    Steps []string `json:"steps"`
}

plan, result, err := gocode.Extract[Plan](ctx, client, system, history,
    gocode.ExtractParams[Plan]{
        Description: "Submit the final plan as a list of ordered steps.",
        Schema: gocode.Object(
            gocode.Array("steps", "ordered steps",
                gocode.SchemaProperty{Type: "string"}, gocode.Required()),
        ),
        // Tools: searchTools,           // optional: search-then-submit
        // Validate: func(p Plan) error  // optional: reject and let the model retry
    })
~~~

`Extract` is built on `ToolMetadata.Terminal` — a flag that tells `Loop` to short-circuit when a tool is invoked successfully. You can set it yourself for hand-rolled submit patterns; `Extract` is the headline sugar.

## Practical assembly

### Toolsets and middleware

~~~go
toolset := gocode.MustJoin(clockTool.Toolset(), workspaceToolset).Wrap(
    gocode.WithTimeout(5*time.Second),
    gocode.WithResultLimit(20_000),
    gocode.WithConfirmation(confirm),
)

result, err := client.Loop(ctx, system, history, toolset, 10)
~~~

`MustJoin` is for static composition where a duplicate tool name is a programmer error. `Join` returns an error for dynamic composition.

Available middleware: `WithTimeout`, `WithResultLimit`, `WithLogging`, `WithPanicRecovery`, `WithConfirmation`. Metadata is advisory; your application decides policy.

### Context management

`ContextManager` trims history explicitly before a call.

~~~go
cm := gocode.ContextManager{MaxTokens: 8000, KeepFirst: 1, KeepRecent: 20}
trimmed, err := cm.Trim(ctx, history)
~~~

The original history is not mutated. Tool-use/tool-result integrity is preserved. Summarization happens only if you configure a summarizer.

### Agent

`Agent` is the blessed middle path: a thin block over a client, prompt, toolset, context manager, iteration limit, and hooks.

~~~go
a := gocode.Agent{
    Client:  client,
    System:  "You are a helpful assistant.",
    Tools:   toolset,
    Context: gocode.ContextManager{MaxTokens: 8000, KeepRecent: 20},
    MaxIter: 10,
}

// One-shot autonomous task: pass the goal as a single user message.
result, err := a.Step(ctx, []gocode.Message{gocode.NewUserMessage("do the thing")})

// Multi-turn: call Step once per human turn, threading history.
result, err = a.Step(ctx, history)
~~~

`Step` trims history once up front and again before every model call inside the loop (when a `ContextManager` is configured), so long autonomous runs don't silently blow the context window. `Hooks.OnIteration` observes each iteration; the underlying `Loop` and `ContextManager.Trim` primitives stay available if you want a different policy. No persistence, scheduler, runner, or hidden lifecycle.

## Built-in tools

| Package | Tools |
|---|---|
| `tools/clock` | current UTC time |
| `tools/math` | safe calculator |
| `tools/workspace` | sandboxed list, find, search, read, file info, exact-string edit |

~~~go
clockTool := clock.New()
ws, err := workspace.NewReadOnly(workspace.Config{Root: "."})
toolset := gocode.MustJoin(clockTool.Toolset(), ws.Toolset())
~~~

`workspace.NewReadOnly` is read-only. `workspace.New` includes `edit_file` — wrap it with `WithConfirmation` before letting writes run.

## Provider tools

Some tools live on the provider side: Anthropic and OpenAI ship a set of tools the model is already trained to use. They split into two shapes.

**Server-executed (category 1):** the provider runs the tool and returns the result inline. There is no Go function to write. Attach via `ProviderTools`:

~~~go
import (
    "github.com/lukemuz/gocode"
    "github.com/lukemuz/gocode/providers/anthropic"
    "github.com/lukemuz/gocode/providers/openai"
)

// Anthropic — works against the standard Messages API.
toolset := gocode.Tools(myLocalBinding).
    WithProviderTools(
        anthropic.WebSearch(anthropic.WebSearchOpts{MaxUses: 3}),
        anthropic.CodeExecution(),
    )

// OpenAI Responses — needs openai.NewResponsesProvider.
toolset := gocode.Tools(myLocalBinding).
    WithProviderTools(
        openai.WebSearch(),
        openai.CodeInterpreter(openai.CodeInterpreterOpts{}),
        openai.FileSearch(openai.FileSearchOpts{VectorStoreIDs: []string{"vs_..."}}),
        openai.ImageGeneration(),
    )
~~~

The agent loop never dispatches these — the response carries provider-specific result items (`server_tool_use`, `web_search_call`, `code_interpreter_call`, …) that round-trip verbatim via `ContentBlock.Raw`.

**Provider-defined schema, you execute (category 2):** the model has been trained on the tool's name and arguments, but you supply the runtime — `bash`, `text_editor`, `computer`. The wire declaration is `{type, name}` instead of `{name, description, input_schema}`, and the dispatch flow is identical to a normal tool. Constructors return ordinary `gocode.ToolBinding`s:

~~~go
bash := anthropic.BashTool(func(ctx context.Context, in json.RawMessage) (string, error) {
    // run the model's command in your sandbox of choice
})
toolset := gocode.Tools(bash).Wrap(gocode.WithConfirmation(promptUser))
~~~

Tools and `ProviderTool`s are tagged for one provider; passing them to a different one fails at request build with a clear error.

**OpenAI: Chat Completions vs. Responses.** Hosted tools (`web_search`, `file_search`, `code_interpreter`, `image_generation`) live on `/v1/responses`, not `/v1/chat/completions`. Use `openai.NewResponsesClientFromEnv(model)` (or build one from `openai.NewResponsesProvider`) when you want them. Plain function calling works on both endpoints; OpenAI has signaled Responses as the path forward, so prefer it for new code.

## Prompt caching

Long, stable prompts (system instructions, tool definitions, big context blocks) can be cached so subsequent turns pay a fraction of the input-token cost. Caching is provider-specific in mechanism but exposed uniformly via `gocode.CacheControl`:

~~~go
// The most common pattern: cache the system prompt and the tool prefix
// for any subsequent turn within the cache window.
client, _ := gocode.New(gocode.Config{
    Provider:    provider,
    Model:       gocode.ModelSonnet,
    SystemCache: gocode.Ephemeral(),       // 5-minute TTL
})
toolset := gocode.Tools(...).CacheLast(gocode.Ephemeral())
~~~

Per-provider behavior:

| Provider | Caching mechanism | Honors markers? |
|---|---|---|
| `anthropic.Provider` | Explicit `cache_control` blocks (cumulative; up to 4 breakpoints) | Yes — system, tools, message blocks |
| `openrouter.Provider` | Translates markers to OpenAI-compatible typed-parts content; routed through to Anthropic backends | Yes — system, tools, message blocks |
| `openai.Provider` | Automatic for prefixes ≥1024 tokens; no field needed | Markers ignored (dropped before send) |
| `openai.ResponsesProvider` | Automatic, same as Chat Completions | Markers ignored |

Use `gocode.EphemeralExtended()` for the 1-hour TTL when a prefix will be reused across long sessions.

`Usage` reports cache stats when the provider returns them — `CacheCreationTokens` (Anthropic only — tokens written to cache this turn) and `CacheReadTokens` (Anthropic and OpenAI/OpenRouter — tokens served from cache at a discount).

## MCP

`mcp` adapts Model Context Protocol tools into ordinary toolsets.

~~~go
srv, err := mcp.Connect(ctx, mcp.Config{Command: "my-mcp-server"})
defer srv.Close()
mcpTools, err := srv.Toolset(ctx)
result, err := client.Loop(ctx, system, history, mcpTools, 10)
~~~

You choose the server, inspect the tools, and pass them in.

## Streaming

~~~go
_, _, err := client.AskStream(ctx, system, history, func(delta gocode.ContentBlock) {
    if delta.Type == gocode.TypeText {
        fmt.Print(delta.Text)
    }
})
~~~

Use `LoopStream` or `Agent.StepStream` for streamed tool loops.

Retries can restart a stream after partial output, so callbacks may see partial text from failed attempts. Use `StreamBuffer` with `RetryConfig.OnRetry` to react and clear:

~~~go
sb := gocode.NewStreamBuffer(
    func(b gocode.ContentBlock) { fmt.Print(b.Text) },
    func() { fmt.Print("\n[retrying…]\n") },
)
client, _ := gocode.New(gocode.Config{..., Retry: gocode.RetryConfig{OnRetry: sb.OnRetry}})
msg, _, err := client.AskStream(ctx, system, history, sb.OnToken)
~~~

## Sessions

`Session` is plain data. You load it, pass `History` to a model call, and persist the result yourself.

~~~go
sess, err := store.Get(ctx, sessionID)
if errors.Is(err, gocode.ErrSessionNotFound) {
    sess = &gocode.Session{ID: sessionID}
} else if err != nil {
    return err
}

sess.History = append(sess.History, gocode.NewUserMessage(input))
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
client, err := gocode.New(gocode.Config{
    Provider:  provider,
    Model:     gocode.ModelSonnet,
    MaxTokens: 4096,
    Retry: gocode.RetryConfig{
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
go test ./...
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

- `02-agent-with-tools` — curated toolset, middleware, context management
- `03-repo-explainer` — sandboxed workspace tools, streaming, file-backed sessions
- `04-router-subagents` — orchestrator delegates to specialist subagents
- `05-persistent-chat` — long-running conversation with `FileStore`
- `06-parallel-pipeline` — parallel fan-out then sequential fan-in
- `07-web-service` — deploy-shaped HTTP server (JSON + SSE) with a Dockerfile

Set the relevant API key first.

See [`VISION.md`](VISION.md) for design philosophy and [`ROADMAP.md`](ROADMAP.md) for forward-looking work and what stays out of core.
