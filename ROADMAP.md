# Roadmap

`gocode` is a small, production-minded Go library for LLM calls, tools, and agent loops.

For the product philosophy, see [`VISION.md`](VISION.md). For usage, see [`README.md`](README.md) and [`QUICKSTART.md`](QUICKSTART.md).

## North star

> Easy things easy. Hard things possible. Nothing hidden.

The core promise remains:

> You own the data. You own the tools. You own the loop.

Roadmap items should make the common path easier without introducing hidden model calls, hidden tool execution, hidden persistence, global registries, or framework-owned control flow.

## Current baseline

The foundation is implemented and should no longer be tracked as future work.

Available today:

- `Client`, `Provider`, `Message`, `ContentBlock`, `Tool`, `ToolFunc`, and `ToolResult`
- Anthropic, OpenAI, and OpenRouter providers
- provider and client constructors from environment variables
- `Ask` and `AskStream` for single model calls
- `Loop` and `LoopStream` for tool-using loops
- concurrent execution of multiple tool calls in one model turn
- `Parallel` for fan-out/fan-in workflows
- retry with exponential backoff and jitter
- typed errors and usage aggregation
- schema builders, `TypedToolFunc`, `NewTypedTool`, and `JSONResult`
- `ToolBinding`, `Toolset`, `Join`, and middleware wrappers
- `WithTimeout`, `WithResultLimit`, `WithLogging`, `WithPanicRecovery`, and `WithConfirmation`
- explicit `ContextManager` with optional summarization
- basic `Assistant` and `Assistant.StepStream`
- safe built-in tools:
  - `agent/tools/clock`
  - `agent/tools/math`
  - `agent/tools/workspace` with sandboxed list, find, search, read, file info, and exact-string edit
- MCP adapter in `agent/mcp`
- core tests and examples for ask, pipeline, agent loop, and streaming

## Product principles for future work

1. **Keep the primitive path tiny.** One model call should not require an agent object, session, runner, graph, or framework lifecycle.
2. **Make the practical path short.** Common agent assembly should use toolsets, context management, middleware, and the assistant block instead of repeated glue.
3. **Preserve the control path.** Raw messages, raw tools, manual loops, custom providers, and user-owned orchestration must remain first-class.
4. **Prefer ordinary Go.** Plain structs, functions, slices, maps, errors, `context.Context`, and interfaces beat framework vocabulary.
5. **No hidden ownership.** The library should not own persistence, scheduling, deployment, global registration, memory policy, or application lifecycle.
6. **Tools are Lego blocks.** Tools and MCP adapters should all compile down to inspectable `ToolBinding` values.
7. **Boring is good.** Reliability, inspectability, and testability matter more than adding agent vocabulary.

## P1 — Finish the practical agent path

P1 focuses on completing the path from primitives to useful local assistants.

### 1. Streaming retry semantics

**Priority:** high  
**Status:** done

Streaming callbacks may receive partial output from failed retry attempts before the successful attempt starts.

Shipped:

- `RetryConfig.OnRetry func(attempt int, wait time.Duration)` — called before each retry sleep, making retries observable
- `StreamBuffer` — pairs `OnToken` (pass to `AskStream`/`LoopStream`/`StepStream`) with `OnRetry` (set as `RetryConfig.OnRetry`); calls an `onReset` callback before each retry so callers can clear partial output in a CLI or SSE stream
- `AskStream` and `LoopStream` now accept nil callbacks without panicking
- README updated with usage examples and cross-references

### 3. Recipe documentation

**Priority:** medium-high  
**Status:** next

Add small, copy-pasteable recipes that teach one pattern at a time. Recipes should show both the convenient path and the primitive underneath when that improves understanding.

Initial recipes:

- ask a model
- continue a conversation
- add one tool
- use a typed tool
- use built-in clock/math/workspace tools
- compose toolsets
- wrap tools with timeout, result limit, logging, and confirmation
- build an assistant step
- add context management
- stream to a terminal
- handle streaming retries
- switch providers
- test with a mock provider
- fan out with `Parallel`
- connect MCP tools
- build a repo explainer

### 4. Compelling example app

**Priority:** high  
**Status:** next

Add one example that demonstrates practical value beyond toy snippets.

Recommended first app:

~~~text
examples/repo-explainer
~~~

Expected behavior:

1. accept a repository path
2. sandbox workspace tools to that path
3. list and read selected files
4. stream a summary or answer a question
5. use `Assistant`, `Toolset`, context management, and safe filesystem tools
6. keep history and execution visible in ordinary Go code

This example should act as an integration test for the practical agent path.

## P2 — Production helpers that preserve control

P2 adds production-oriented helpers without introducing a runner.

### 1. Assistant hardening

**Priority:** high after P1  
**Status:** done

Shipped:

- `Hooks` documentation now specifies the exact call order (Trim → OnStep → Loop/LoopStream → OnStepDone) and explicitly notes that `OnStepDone` is not called when `Trim` fails
- Desugared-step comments in `Step` and `StepStream` now show the correct error-returning form instead of silently swallowing the trim error
- `StepStream` doc cross-references `StreamBuffer` for retry-aware streaming
- `LoopStream` nil-callback guard added at the `Client` level (consistent with `StepStream`)

### 2. Sessions without a runner

**Priority:** medium

Add boring persistence for conversation history.

Possible shape:

~~~go
type Session struct {
    ID      string
    History []Message
    State   map[string]any
}

type Store interface {
    Create(ctx context.Context, session *Session) error
    Get(ctx context.Context, id string) (*Session, error)
    Update(ctx context.Context, session *Session) error
    Delete(ctx context.Context, id string) error
    List(ctx context.Context, prefix string, limit int) ([]*Session, error)
}
~~~

Likely built-ins:

- memory store for tests and development
- file store for simple local apps

Principles:

- sessions do not call models
- sessions do not run tools
- sessions do not trim automatically
- no runner abstraction
- `Session.History` stays plain `[]Message`

### 3. Durable tool execution

**Priority:** medium

Conversation persistence does not solve mid-tool-call crashes. Side-effectful tools need opt-in idempotency.

Possible shape:

~~~go
type ToolResultStore interface {
    Get(ctx context.Context, toolUseID string) (string, bool, error)
    Put(ctx context.Context, toolUseID string, result string) error
}

func WithIdempotency(store ToolResultStore) Middleware
~~~

Principles:

- use existing tool-use IDs as idempotency keys
- middleware is opt-in
- document the boundary: this prevents duplicate agent-level execution after crash-resume, not universal exactly-once delivery
- compose with confirmation, logging, timeout, and result-limit middleware

### 4. Observability hooks and OpenTelemetry adapter

**Priority:** medium

Expand hooks from assistant-level observation toward request, response, tool call, tool result, latency, and error events.

Possible future hooks:

~~~go
type Hooks struct {
    OnRequest    func(ctx context.Context, iter int, req ProviderRequest)
    OnResponse   func(ctx context.Context, iter int, resp ProviderResponse, dur time.Duration)
    OnToolCall   func(ctx context.Context, iter int, name string, input json.RawMessage)
    OnToolResult func(ctx context.Context, iter int, name string, result ToolResult, dur time.Duration)
    OnError      func(ctx context.Context, iter int, err error)
}
~~~

Add an `agent/otel` subpackage for OpenTelemetry translation so the core package does not depend on OTel.

### 5. Extended model configuration

**Priority:** medium

Thread optional generation controls through `ProviderRequest`.

Candidates:

- temperature
- top-p
- stop sequences

Principles:

- zero values preserve provider defaults
- avoid a large vendor-specific config surface
- provider-specific escape hatches belong in provider configs

### 6. Testing helpers

**Priority:** medium

The `Provider` interface is already the main testing seam. Add tiny helpers for deterministic tests.

Candidates:

- static mock provider
- scripted provider
- assertions for history shape, tool calls, usage, and typed errors

### 7. ADK comparison doc

**Priority:** medium

Add a focused, fair comparison that explains when `gocode` is a better fit and when ADK-style systems are useful.

Possible file:

~~~text
COMPARISON.md
~~~

Keep the README short and avoid turning it into a comparison essay.

### 8. HTTP/SSE service example

**Priority:** medium

Add a small `net/http` example that streams model output over Server-Sent Events.

Expected shape:

1. handler receives a user message
2. handler loads history
3. handler appends the user turn
4. handler calls `AskStream` or `Assistant.StepStream`
5. handler writes SSE events
6. handler stores updated history

No web framework, runner, or hidden session lifecycle.

## P3 — Useful, but design carefully

### 1. Evaluation helpers

**Priority:** medium-low

Small test helpers for regression-testing agent behavior. Avoid hosted-platform concepts, hidden model management, or required databases.

### 2. Lightweight multi-agent composition

**Priority:** low

Maybe add functions for routing, critique, fan-out/fan-in, or delegation. Keep them outside the core unless the shape is obviously just functions over existing primitives.

Avoid graph runtimes, autonomous registries, hidden schedulers, and opaque lifecycle management.

### 3. Cross-session memory

**Priority:** low / likely separate package

Search across prior sessions or external knowledge pulls in embeddings, vector stores, chunking, ranking, and persistence. Keep this separate from the core loop and session model.

## Explicit non-goals

The core library should not become:

- a graph executor
- a visual workflow builder
- a no-code agent configuration system
- a managed deployment platform
- a hidden scheduler
- a vector database
- a global tool registry
- an autonomous background-agent runtime
- a framework-owned runner
- an ADK-style application object graph

Higher-level systems can be built on top of `gocode`. The core should remain small, explicit, composable, and easy to reason about.

## Implementation order

| Order | Item | Status |
|---|---|---|
| 1 | Streaming retry helper and docs | Done |
| 2 | Recipe documentation | Next |
| 3 | Repo explainer example | Next |
| 5 | Assistant hardening | Done |
| 6 | Session/store helpers | Planned |
| 7 | Durable tool execution middleware | Planned |
| 8 | Observability hooks and OTel adapter | Planned |
| 9 | Extended model configuration | Planned |
| 10 | Testing helpers | Planned |
| 11 | ADK comparison doc | Planned |
| 12 | HTTP/SSE service example | Planned |
| 13 | Evaluation helpers | Future |
| 14 | Lightweight multi-agent helpers | Future / cautious |
| 15 | Cross-session memory | Future / likely separate package |

## Next focus

The next coherent milestone is:

1. recipes that show the practical path
2. repo explainer example that ties together `Assistant`, `Toolset`, context management, workspace tools, and streaming

That milestone should make `gocode` feel complete for local, practical agents while preserving the same simple foundation: ordinary Go code, visible data flow, explicit control.
