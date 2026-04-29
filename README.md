# gocode

`gocode` is a Go library for building LLM-powered software that scales from one function call to serious agent systems without forcing the serious-agent shape onto the first function call.

The project is built around a simple idea:

> Make easy things easy without making advanced things harder.

You get small, inspectable primitives:

- `Client` for model calls
- `Provider` for Anthropic, OpenAI, OpenRouter, or your own backend
- `Message` and `ContentBlock` as plain conversation data
- `Tool` and `ToolFunc` for model-callable functions
- `Ask` for one model call
- `Loop` for tool-using agent loops
- `Parallel` for fan-out/fan-in workflows
- streaming, retries, typed errors, and usage tracking

You also get progressively higher-level assembly helpers where they reduce real boilerplate without taking over your application.

The core promise is:

> You own the data. You own the tools. You own the loop.

`gocode` does **not** make simple tasks pay a framework tax. One model call should stay one model call. A basic tool-using assistant should be easy to assemble. A complex production agent should remain explicit, inspectable, and customizable.

Requires Go 1.21+. No external dependencies.

See **[VISION.md](VISION.md)** for the longer product philosophy.

---

## Why gocode?

Many agent frameworks optimize for the fully-loaded case.

Once you accept their application model, hard things can become relatively easy: agents, runners, sessions, tools, memory, callbacks, artifacts, and lifecycle hooks all have a place.

The tradeoff is that simple things often require the same conceptual setup as complex things.

`gocode` aims for a smoother complexity curve:

| Task size | `gocode` experience |
|---|---|
| Simple task | Tiny setup |
| Medium task | Ergonomic assembly |
| Hard task | Explicit composition |

Use it when you want:

- one-off LLM calls without ceremony
- tool use without a heavyweight runtime
- practical agent patterns without hidden control flow
- transparent conversation history
- explicit context management
- easy testing through interfaces
- streaming output for CLIs, coding tools, services, or web UIs
- retry behavior and typed errors suitable for production
- APIs that are easy for humans and coding agents to inspect and modify

`gocode` is not anti-convenience. It is anti-trap.

Good abstractions should compress boilerplate, expose the primitives underneath, and be easy to bypass. Bad abstractions hide model calls, tool execution, memory mutation, persistence, or application lifecycle.

The goal is:

> Batteries included, control retained.

---

## Quickstart

See **[QUICKSTART.md](QUICKSTART.md)** for the fastest path.

The quickstart now focuses on:

1. making one model call with `Ask`
2. adding one simple tool with `Loop`
3. understanding the core shape without front-loading production details

If you want the short version:

~~~go
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

history := []agent.Message{
	agent.NewUserMessage("Give me three practical ideas for using LLMs in a Go service."),
}

reply, err := client.Ask(context.Background(), "You are concise.", history)
if err != nil {
	log.Fatal(err)
}

fmt.Println(agent.TextContent(reply))
~~~

That is the basic model:

- build a provider
- build a client
- pass plain message history
- get a plain message back

No hidden session. No framework runtime.

---

## Installation

~~~bash
go get github.com/lukemuz/gocode/agent
~~~

Set whichever API key matches your provider:

~~~bash
export ANTHROPIC_API_KEY=sk-ant-...
export OPENAI_API_KEY=sk-...
export OPENROUTER_API_KEY=sk-or-...
~~~

---

## The building blocks

### `Provider`

A `Provider` translates between `gocode`'s canonical data model and an LLM API.

Anthropic, OpenAI, and OpenRouter providers are included. You can implement your own provider for any backend.

~~~go
type Provider interface {
	Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error)
	Stream(ctx context.Context, req ProviderRequest, onDelta func(ContentBlock)) (ProviderResponse, error)
}
~~~

`ProviderRequest` and `ProviderResponse` are the shared internal contract:

~~~go
type ProviderRequest struct {
	Model     string
	MaxTokens int
	System    string
	Messages  []Message
	Tools     []Tool
}

type ProviderResponse struct {
	Content    []ContentBlock
	StopReason string
	Usage      Usage
}
~~~

Every provider maps its wire format to these types.

Your application code does not need to change when you switch providers.

---

### `Client`

A `Client` knows which provider, model, token limit, and retry policy to use.

~~~go
client, err := agent.New(agent.Config{
	Provider:  provider,
	Model:     agent.ModelSonnet,
	MaxTokens: 1024,
})
~~~

`Client` does not store conversation state. Every call receives a `[]Message`.

That makes one client safe to reuse across many conversations, HTTP requests, jobs, or goroutines.

---

### `Message`

Conversation history is plain data:

~~~go
history := []agent.Message{
	agent.NewUserMessage("Hello"),
}
~~~

A `Message` contains a role and a list of content blocks:

~~~go
type Message struct {
	Role    string
	Content []ContentBlock
}
~~~

Common content block types include:

| Type | Meaning |
|---|---|
| `text` | Assistant or user text |
| `tool_use` | A model-requested tool call |
| `tool_result` | The result of a tool function |

Helpers like `NewUserMessage`, `NewToolResultMessage`, and `TextContent` cover common cases.

---

### `Tool`

A tool has two parts:

1. the definition the model sees
2. the Go function your program runs

Definition (using the new schema helpers):

~~~go
readFile, err := agent.NewTool("read_file", "Read a file.", agent.Object(
	agent.String("path", "Path to read", agent.Required()),
))
~~~

Implementation:

~~~go
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
			return "", err
		}

		return string(data), nil
	},
}
~~~

This is intentionally transparent:

- the model sees the exact schema you wrote
- your function receives the exact JSON the model produced
- dispatch is a normal Go map
- there is no hidden registry or runtime

The low-level path is explicit by design.

At the same time, explicit does not have to mean tedious. Schema builders (`Object`, `String`, `Integer`, `Required`, etc.), `TypedToolFunc`, and `JSONResult` are now implemented to reduce boilerplate while preserving the explicit design. Pre-built tools and dispatch helpers are next.

---

## Three tiers

`gocode` is designed around three levels of complexity. Use the smallest one that solves your problem.

---

### Tier 1 — One model call

Use `Ask` when you want one response from the model.

~~~go
history := []agent.Message{
	agent.NewUserMessage("What is the capital of France?"),
}

reply, err := client.Ask(ctx, "", history)
if err != nil {
	log.Fatal(err)
}

fmt.Println(agent.TextContent(reply))
~~~

`Ask` does not mutate your history. If you want to continue the conversation, append the reply yourself:

~~~go
history = append(history, reply)
history = append(history, agent.NewUserMessage("What is its population?"))

reply, err = client.Ask(ctx, "", history)
~~~

You own the conversation.

---

### Tier 2 — Parallel steps

Use `Parallel` for independent fan-out work.

~~~go
results := agent.Parallel(ctx,
	func(ctx context.Context) (string, error) {
		return ask(ctx, client, "Summarize Rome in two sentences.")
	},
	func(ctx context.Context) (string, error) {
		return ask(ctx, client, "Summarize Athens in two sentences.")
	},
)
~~~

Results are index-aligned with the input steps:

~~~go
for i, r := range results {
	if r.Err != nil {
		log.Fatalf("step %d: %v", i, r.Err)
	}
}
~~~

Then compose the outputs however you want:

~~~go
comparison, err := ask(ctx, client, fmt.Sprintf(
	"Compare these:\n\nRome: %s\n\nAthens: %s",
	results[0].Value,
	results[1].Value,
))
~~~

`Parallel` uses goroutines. It is a helper, not a scheduler.

---

### Tier 3 — Tool loop

Use `Loop` when the model can call tools.

~~~go
result, err := client.Loop(
	ctx,
	"You are a helpful assistant with filesystem access.",
	history,
	[]agent.Tool{readFile},
	dispatch,
	5,
)
if err != nil {
	log.Fatal(err)
}

last := result.Messages[len(result.Messages)-1]
fmt.Println(agent.TextContent(last))
~~~

Conceptually, `Loop` does this:

~~~text
1. Send messages and tools to the model.
2. Receive the model response.
3. If the model is done, return the full history.
4. If the model requested tools, run your ToolFunc values.
5. Append tool results to history.
6. Repeat until done or max iterations is reached.
~~~

There is no hidden graph, event bus, checkpoint manager, or autonomous runtime.

It is the common tool loop you would otherwise write yourself.

---

## Streaming

Use `AskStream` when you want tokens as they arrive:

~~~go
_, err := client.AskStream(ctx, system, history, func(delta agent.ContentBlock) {
	if delta.Type == agent.TypeText {
		fmt.Print(delta.Text)
	}
})
~~~

Use `LoopStream` for streaming tool-using loops:

~~~go
result, err := client.LoopStream(
	ctx,
	system,
	history,
	tools,
	dispatch,
	5,
	func(delta agent.ContentBlock) {
		if delta.Type == agent.TypeText {
			fmt.Print(delta.Text)
		}
		if delta.Type == agent.TypeToolUse && delta.Name != "" {
			fmt.Printf("\n[calling tool: %s]\n", delta.Name)
		}
	},
	func(results []agent.ToolResult) {
		for _, r := range results {
			if r.IsError {
				fmt.Printf("[tool error] %s\n", r.Content)
			} else {
				fmt.Printf("[tool result] %s\n", r.Content)
			}
		}
	},
)
~~~

Streaming callbacks fire synchronously as data arrives.

Both streaming methods still return the final complete `Message` or `LoopResult`.

---

## Tools: explicit core, easier assembly
The core tool API remains low-level and fully inspectable:

- `InputSchema` + `NewTool(...)`
- `ToolFunc` receives `json.RawMessage`
- dispatch is a plain `map[string]ToolFunc`

`TypedToolFunc`, `JSONResult`, and the schema builders (`Object`/`String`/`Required`/etc.) are now implemented to reduce boilerplate while preserving the explicit design.

```go
type CalculatorInput struct {
	Operation string  `json:"operation"`
	A         float64 `json:"a"`
	B         float64 `json:"b"`
}

fn := agent.TypedToolFunc(func(ctx context.Context, in CalculatorInput) (string, error) {
	switch in.Operation {
	case "add":
		return fmt.Sprintf("%f", in.A+in.B), nil
	// ...
	default:
		return "", fmt.Errorf("unknown operation: %s", in.Operation)
	}
})

// Use in dispatch map; pair with JSONResult for structured output:
return agent.JSONResult(map[string]any{"result": 42})
```

See `agent/tool.go` for `TypedToolFunc`, `JSONResult`, `Object`, `String`, `Required` and the other schema helpers. Pre-built tools and dispatch helpers are next.

These helpers compile down to the primitives and follow "Lego blocks, not a framework."

---

## Planned pre-built tools

Pre-built tools are part of the vision, not a departure from it.

The goal is a small library of safe, boring, opt-in tools that users can inspect, modify, or replace.

Likely starting points:

- current time
- filesystem read/list with root sandboxing
- HTTP GET/POST with allowlists
- JSON fetch
- shell command with explicit opt-in and timeouts
- web search adapter interfaces
- basic math/calculator
- maybe SQL helpers with strong caveats

The safety rule is simple:

> A pre-built tool should be explicit to enable, obvious to inspect, and conservative by default.

For example, filesystem tools should prefer sandboxed roots. HTTP tools should support allowlists and timeouts. Shell execution should be visibly dangerous and require explicit configuration.

Pre-built tools should make examples and real applications faster to assemble without turning `gocode` into a framework.

---

## Error handling

`gocode` uses typed errors that work with `errors.Is` and `errors.As`.

Common cases:

~~~go
var apiErr *agent.APIError
if errors.As(err, &apiErr) {
	fmt.Println(apiErr.StatusCode, apiErr.Message)
}

if errors.Is(err, agent.ErrMissingTool) {
	// The model requested a tool that was not present in dispatch.
}

var loopErr *agent.LoopError
if errors.As(err, &loopErr) {
	fmt.Printf("loop failed at iteration %d: %v\n", loopErr.Iter, loopErr.Cause)
}

var retryErr *agent.RetryExhaustedError
if errors.As(err, &retryErr) {
	fmt.Printf("retries exhausted after %d attempts: %v\n", retryErr.Attempts, retryErr.Cause)
}

if errors.Is(err, agent.ErrMaxIter) {
	// The loop hit its iteration budget.
}
~~~

Tool errors are soft by default.

If a `ToolFunc` returns an error, the error is fed back to the model as a tool result with `IsError: true`. The model can retry, ask for clarification, or explain the failure.

A missing tool is different. If the model asks for a tool that is not in your dispatch map, `Loop` returns `ErrMissingTool`. That is treated as a programming/configuration error.

---

## Retries

Retries are built in for transient failures such as rate limits, temporary network errors, and 5xx responses.

The zero value enables sensible defaults.

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

Set `Disabled: true` to disable retries.

Retries do not change the programming model. If retries are exhausted, you get a typed `RetryExhaustedError`.

---

## Conversation history

`Client` holds no conversation state.

Every call receives a history slice. Every loop result returns an updated history slice.

~~~go
history := []agent.Message{
	agent.NewUserMessage("Hello"),
}

reply, err := client.Ask(ctx, "", history)
if err != nil {
	log.Fatal(err)
}

history = append(history, reply)
history = append(history, agent.NewUserMessage("Tell me more."))

reply, err = client.Ask(ctx, "", history)
~~~

`Loop` follows the same idea:

~~~go
result, err := client.Loop(ctx, system, history, tools, dispatch, 5)
if err != nil {
	log.Fatal(err)
}

history = result.Messages
~~~

You can store history in memory, a database, a file, an HTTP session, or your own domain model.

Future session helpers should remain boring wrappers around this same plain `[]Message` data.

---

## Testing

The `Provider` interface is the main testing seam.

You can test `Ask`, `Loop`, streaming behavior, tool execution, history shape, usage accounting, and error handling without making real API calls.

Tests should focus on contracts:

- messages appended in the expected order
- tool calls produce expected tool results
- errors match with `errors.Is` / `errors.As`
- callbacks fire in expected order
- usage is accumulated
- partial history is inspectable on failure

Run the package tests with:

~~~bash
go test ./agent
~~~

The repository includes tests for core loop behavior, retry logic, provider streaming parsing, errors, and parallel execution.

---

## Configuration

Client configuration:

~~~go
client, err := agent.New(agent.Config{
	Provider:  provider,
	Model:     agent.ModelSonnet,
	MaxTokens: 4096,
	Retry: agent.RetryConfig{
		MaxRetries:  3,
		InitialWait: time.Second,
		MaxWait:     30 * time.Second,
		Disabled:    false,
	},
})
~~~

Provider configuration:

~~~go
provider, err := agent.NewAnthropicProvider(agent.AnthropicConfig{
	APIKey:     os.Getenv("ANTHROPIC_API_KEY"),
	BaseURL:    "https://api.anthropic.com",
	HTTPClient: &http.Client{Timeout: 30 * time.Second},
})
~~~

~~~go
provider, err := agent.NewOpenAIProvider(agent.OpenAIConfig{
	APIKey:     os.Getenv("OPENAI_API_KEY"),
	BaseURL:    "https://api.openai.com",
	HTTPClient: &http.Client{Timeout: 30 * time.Second},
})
~~~

~~~go
provider, err := agent.NewOpenRouterProvider(agent.OpenRouterConfig{
	APIKey:     os.Getenv("OPENROUTER_API_KEY"),
	BaseURL:    "https://openrouter.ai",
	HTTPClient: &http.Client{Timeout: 30 * time.Second},
})
~~~

Common Anthropic model constants:

~~~go
agent.ModelOpus
agent.ModelSonnet
agent.ModelHaiku
~~~

You can also pass provider-specific model strings directly.

---

## Running examples

Set an API key first:

~~~bash
export ANTHROPIC_API_KEY=sk-ant-...
~~~

Then run examples:

~~~bash
# Tier 1: single model call
go run ./examples/ask

# Tier 2: parallel and sequential composition
go run ./examples/pipeline

# Tier 3: tool-using loop
go run ./examples/agent

# Streaming output
go run ./examples/stream
~~~

---

## Roadmap

See **[ROADMAP.md](ROADMAP.md)** for the detailed future plan and **[VISION.md](VISION.md)** for the design philosophy.

The completed foundation is now the baseline. The next phase is about progressive complexity: keep simple calls tiny, make practical agents easier to assemble, and keep advanced systems explicit and composable.

Near-term priorities:

1. Add a small, safe pre-built tool library.
2. Add tool bindings, toolsets, and dispatch helpers for composing local tools, MCP tools, and skills.
3. Add explicit context management that can be used directly and bundled into the practical agent pattern.
4. Add a basic, extensible agent block: an assembled primitive with batteries included, designed to be customized, embedded, and built on.
5. Add recipe-style documentation for common patterns.
6. Add one compelling example app, likely a repo explainer CLI.
7. Support MCP as a transparent adapter to ordinary `Tool` and `ToolFunc` values.
8. Add transparent skills as inspectable bundles of prompts, tools, examples, and metadata.

Production-focused follow-ups:

1. Add boring session persistence without a runner.
2. Add hooks, traces, and observability.
3. Add extended model configuration.
4. Add provider setup helpers such as environment-based constructors.
5. Add testing, evaluation, and replay helpers.
6. Add an HTTP/SSE service example.

The guiding principle is:

> Primitives first. Useful agent blocks second. No hidden runner.

The library should support three paths:

1. **Tiny path:** use `Ask` for one model call.
2. **Practical path:** use toolsets, context management, and a basic agent block to build useful assistants with less glue while still owning history, storage, and execution.
3. **Control path:** drop down to raw messages, raw tools, manual loops, custom providers, and explicit dispatch whenever needed.

All paths should lead to the same transparent primitives.

---

## Package layout

~~~text
agent/
  agent.go        Client, New, Ask, AskStream, Loop, LoopStream, Config
  provider.go     Provider, ProviderRequest, ProviderResponse
  anthropic.go    AnthropicProvider
  openai.go       OpenAIProvider
  openrouter.go   OpenRouterProvider
  message.go      Message, ContentBlock, NewUserMessage, TextContent
  tool.go         Tool, ToolFunc, NewTool, InputSchema, ToolResult
  client.go       Usage and shared constants
  parallel.go     Parallel[T]
  retry.go        RetryConfig and retry helpers
  errors.go       APIError, ToolError, LoopError, RetryExhaustedError
~~~

The network boundary lives in provider implementations.

The core orchestration types remain plain Go.

That plainness is part of the product. It makes the library easier for humans to debug and easier for coding agents to understand, extend, and safely modify.

---

## Non-goals

`gocode` should not become:

- a graph executor
- a visual workflow builder
- a managed agent platform
- a no-code agent configuration system
- a hidden scheduler
- a deployment framework
- a vector database
- a cross-session memory platform
- a replacement for your application architecture

Higher-level systems can be built on top of `gocode`.

The library itself should remain small, explicit, composable, and easy to reason about.