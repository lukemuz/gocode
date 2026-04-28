# gocode

A Go library for building LLM agents where the execution flow stays in your code, not inside a framework.

**No external dependencies. Single binary. Zero magic.**

---

## The idea

Most agent frameworks invert control: you configure the framework, the framework runs your agent. You end up debugging the framework instead of your code.

This library does the opposite. You call it; it never calls you back through abstractions you didn't write. The agent loop is a `for` loop in your code. Tool dispatch is a `map`. Conversation history is a `[]Message` slice you own. Everything that happens is visible at the call site.

---

## Installation

```bash
go get github.com/lukemuz/gocode/agent
```

Requires Go 1.21+. No other dependencies.

---

## Three tiers

The library is designed around three levels of complexity. Pick the one that fits your problem — you don't need to understand Tier 3 to use Tier 1.

### Tier 1 — A single LLM call

```go
client, err := agent.New(agent.Config{
    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
    Model:  agent.ModelSonnet,
})

history := []agent.Message{
    agent.NewUserMessage("What is the capital of France?"),
}

reply, err := client.Ask(context.Background(), "", history)
fmt.Println(agent.TextContent(reply))
```

`Ask` sends the message list to the model and returns the assistant's reply. The history slice is not modified — append the reply yourself if you want to continue the conversation:

```go
history = append(history, reply)
history = append(history, agent.NewUserMessage("And what is its population?"))
reply, err = client.Ask(ctx, "", history)
```

### Tier 2 — Parallel steps

Use `Parallel` to fan out multiple independent calls and collect their results before proceeding:

```go
results := agent.Parallel(ctx,
    func(ctx context.Context) (string, error) {
        return ask(ctx, client, "Summarize the Roman Empire in two sentences.")
    },
    func(ctx context.Context) (string, error) {
        return ask(ctx, client, "Summarize the Athenian city-state in two sentences.")
    },
)

for i, r := range results {
    if r.Err != nil {
        log.Fatalf("step %d: %v", i, r.Err)
    }
}

comparison, err := ask(ctx, client, fmt.Sprintf(
    "Compare these:\n\nRome: %s\n\nAthens: %s",
    results[0].Value, results[1].Value,
))
```

`Parallel` launches each step in a goroutine and waits for all of them. Results are index-aligned — `results[i]` always corresponds to `steps[i]`. No step is cancelled if another fails; error policy is yours to decide.

`Parallel` is generic. The type parameter is inferred from your step functions:

```go
// T is inferred as string from the step return type
results := agent.Parallel[string](ctx, stepA, stepB)

// Mixed types? Use any and type-assert.
results := agent.Parallel[any](ctx, stepA, stepB)
```

### Tier 3 — Agentic loop with tools

`Loop` runs the model repeatedly, executing tools as the model requests them, until it signals it's done:

```go
result, err := client.Loop(
    ctx,
    "You are a helpful assistant with filesystem access.",
    history,
    []agent.Tool{listDirTool, readFileTool},
    dispatch,
    10, // max iterations (0 = no limit)
)

last := result.Messages[len(result.Messages)-1]
fmt.Println(agent.TextContent(last))
fmt.Printf("tokens: %d in, %d out\n", result.Usage.InputTokens, result.Usage.OutputTokens)
```

The loop body — what you'd have to write yourself if this were raw API calls:

```
1. Send messages + tools to the model
2. Receive response
3. If stop_reason == "end_turn"  → done, return
4. If stop_reason == "tool_use"  → run the requested tools, append results, goto 1
5. If stop_reason == "max_tokens" → return error
```

That's it. No scheduler, no hidden lifecycle, no event bus. The full loop is a `for` loop in [agent.go](agent/agent.go).

---

## Defining tools

A tool has two parts that are kept deliberately separate: its **definition** (what the model sees) and its **implementation** (what your code runs).

**The definition** — sent to the model on every Loop call:

```go
readFile, err := agent.NewTool("read_file", "Read the contents of a file.", agent.InputSchema{
    Type: "object",
    Properties: map[string]agent.SchemaProperty{
        "path": {Type: "string", Description: "Absolute or relative path to the file"},
    },
    Required: []string{"path"},
})
```

**The implementation** — a plain Go function:

```go
dispatch := map[string]agent.ToolFunc{
    "read_file": func(ctx context.Context, input json.RawMessage) (string, error) {
        var params struct {
            Path string `json:"path"`
        }
        if err := json.Unmarshal(input, &params); err != nil {
            return "", err
        }
        data, err := os.ReadFile(params.Path)
        if err != nil {
            return "", err  // returned as is_error=true; the model can see and recover
        }
        return string(data), nil
    },
}
```

`input` is the raw JSON the model produced — your function owns its own `json.Unmarshal`. The library never inspects or transforms tool arguments, which means it places no constraints on what schemas you can express.

**Tool errors are soft faults.** If your function returns an error, it becomes an `is_error: true` result fed back to the model. The model can see the error message and decide what to do — retry, ask for clarification, or give up gracefully. Only one thing causes a hard abort: the model requesting a tool that isn't in `dispatch`. That's a programming error and `Loop` returns immediately with `ErrMissingTool`.

---

## Conversation history

`Client` holds no state. Every call takes a `[]Message` and returns a result. You own the slice.

```go
// Start a conversation
history := []agent.Message{
    agent.NewUserMessage("Hello"),
}

reply, err := client.Ask(ctx, "", history)
// history is unchanged
history = append(history, reply)

// Continue it
history = append(history, agent.NewUserMessage("Tell me more."))
reply, err = client.Ask(ctx, "", history)
history = append(history, reply)
```

`Loop` follows the same pattern. It copies the history you pass in, appends all new turns internally, and returns the full updated slice in `LoopResult.Messages`. Your original slice is never modified.

This means one `Client` can drive multiple concurrent conversations — just keep separate history slices.

---

## The data model

Every message in a conversation is a `Message` with a role and a content array:

```go
type Message struct {
    Role    string         // "user" or "assistant"
    Content []ContentBlock
}
```

A `ContentBlock` is polymorphic on `Type`:

| `Type`        | Populated fields                        | When it appears              |
|---------------|-----------------------------------------|------------------------------|
| `"text"`      | `Text`                                  | Every assistant response     |
| `"tool_use"`  | `ID`, `Name`, `Input`                   | When the model calls a tool  |
| `"tool_result"` | `ToolUseID`, `Content`, `IsError`     | Your tool's return value     |

You rarely need to construct `ContentBlock` values directly. `NewUserMessage`, `NewToolResultMessage`, and the return from `Ask`/`Loop` cover the common cases. `TextContent(msg)` extracts all text from a message without needing a type switch.

---

## Error handling

Three concrete error types, all implementing `Unwrap` for use with `errors.Is`:

```go
// API responded with a non-2xx status
var apiErr *agent.APIError
if errors.As(err, &apiErr) {
    fmt.Println(apiErr.StatusCode, apiErr.Message)
}

// Model called a tool not in the dispatch map
if errors.Is(err, agent.ErrMissingTool) { ... }

// Loop aborted (max iterations, max_tokens, or API error mid-loop)
var loopErr *agent.LoopError
if errors.As(err, &loopErr) {
    fmt.Printf("failed at iteration %d: %v\n", loopErr.Iter, loopErr.Cause)
    // loopErr.Cause unwraps to the original error
}

// Exhausted the iteration budget
if errors.Is(err, agent.ErrMaxIter) { ... }
```

When `Loop` returns an error, `LoopResult.Messages` still contains the history up to the point of failure. You can inspect it, log it, or resume from it.

---

## Configuration

```go
client, err := agent.New(agent.Config{
    APIKey:     "sk-ant-...",          // required
    Model:      agent.ModelSonnet,     // required
    MaxTokens:  4096,                  // default: 1024
    BaseURL:    "https://...",         // default: https://api.anthropic.com
    HTTPClient: &http.Client{...},     // default: 60-second timeout
})
```

Available models:

```go
agent.ModelOpus   // claude-opus-4-7
agent.ModelSonnet // claude-sonnet-4-6
agent.ModelHaiku  // claude-haiku-4-5-20251001
```

`MaxTokens` is the per-response limit. If the model hits it, `Loop` returns a `LoopError` with a message suggesting you increase it. For long-running loops where individual responses can be large, set this to at least 4096.

---

## Running the examples

```bash
export ANTHROPIC_API_KEY=sk-ant-...

# Tier 1: single call
go run ./examples/ask

# Tier 2: parallel + sequential
go run ./examples/pipeline

# Tier 3: agentic loop with filesystem tools
go run ./examples/agent
```

---

## Package layout

```
agent/
  agent.go     Client, New(), Ask(), Loop(), LoopResult
  message.go   Message, ContentBlock, NewUserMessage(), TextContent()
  tool.go      Tool, ToolFunc, NewTool(), InputSchema, ToolResult
  client.go    Anthropic Messages API HTTP transport (unexported)
  parallel.go  Parallel[T]() generic fan-out
  errors.go    APIError, ToolError, LoopError, ErrMaxIter, ErrMissingTool
```

The HTTP layer (`client.go`) is the only file that talks to the network. Everything else is pure Go. If you want to swap in a different model provider, that's the only file that changes.
