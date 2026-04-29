# Roadmap

`gocode` is a small, production-minded Go library for LLM calls, tools, and agent loops.

For the product philosophy behind this plan, see [VISION.md](VISION.md).

The project should scale from one function call to serious agent systems without forcing the serious-agent shape onto the first function call.

The goal is:

> Make easy things easy without making advanced things harder.

`gocode` is not anti-convenience. It is anti-trap.

A good abstraction should compress boilerplate, expose the primitives underneath, and be easy to bypass. A bad abstraction hides model calls, tool execution, memory mutation, persistence, or application lifecycle.

## Vision

> Easy things easy. Hard things possible. Nothing hidden.

`gocode` should feel like Go:

- plain data
- plain functions
- explicit errors
- composable primitives
- easy testing
- minimal magic
- clear escape hatches

The core promise is:

> You own the data. You own the tools. You own the loop.

That means:

- conversation history is a `[]Message`
- tool dispatch is a plain `map[string]ToolFunc`
- providers implement a small interface
- loops are visible and understandable
- retries, streaming, errors, and usage are explicit
- context management is explicit
- higher-level patterns are built from ordinary Go code
- every convenience layer can be inspected, bypassed, or replaced

The desired complexity curve is:

| Task size | `gocode` experience |
|---|---|
| Simple task | Tiny setup |
| Medium task | Ergonomic assembly |
| Hard task | Explicit composition |

The long-term product direction is:

> `gocode` is the `net/http`-style agent library for Go services: boring, inspectable, flexible primitives with practical recipes that are easy to snap together.

The product bar for every roadmap item is:

> Does this make the common path easier while keeping execution visible?

If yes, it likely belongs. If it adds power by introducing a hidden runtime model, hidden lifecycle, global registry, or framework-owned control flow, it should be deferred, redesigned, or kept outside the core.

## Design principles

### 1. Explicit core

The core package should remain small and understandable.

Core primitives include:

- `Client`
- `Provider`
- `Message`
- `ContentBlock`
- `Tool`
- `ToolFunc`
- `Ask`
- `Loop`
- `AskStream`
- `LoopStream`
- `Parallel`
- rich typed errors
- retry configuration

These are the durable low-level building blocks. They should remain inspectable and usable directly.

### 2. Simple assembly

Explicitness should not mean unnecessary ceremony.

The library should make common workflows easy to assemble without hiding the underlying primitives. Convenience helpers are welcome when they compile down to obvious data structures and functions.

A user should be able to start with one model call, add one tool, add a loop, and then adopt practical agent features like toolsets, context management, sessions, streaming, observability, and evaluation only when needed.

Good convenience:

- typed tool handlers
- schema builders
- pre-built safe tools
- tool bundles
- explicit context managers
- basic, extensible agent blocks
- testing helpers
- provider constructors from environment
- small session helpers

Bad magic:

- hidden model calls
- hidden global state
- hidden persistence
- hidden context compaction
- reflection-only APIs with no inspectable output
- framework-owned runtimes
- graph executors or schedulers inside the core
- callbacks that invert application control

The right shape is three paths:

#### Path 1: Tiny

One model call, minimal setup, no agent object required.

#### Path 2: Practical

Toolsets, context management, streaming, hooks, and a basic, extensible agent block for common agent workflows.

#### Path 3: Full control

Raw schemas, raw messages, manual loops, custom providers, explicit dispatch, and user-owned orchestration.

All paths should lead to the same transparent core.

### 3. Progressive complexity

The learning path should be layered:

1. Call a model with `Ask`.
2. Continue a conversation by appending to `[]Message`.
3. Add one tool with `Tool`, `ToolFunc`, and `Loop`.
4. Make tools easier with typed helpers, schema builders, safe built-ins, and toolsets.
5. Build a practical assistant with explicit context management and a thin step helper.
6. Productionize with sessions, hooks, streaming, testing, evaluation, and replay.

Each layer should be useful on its own. No layer should force users to adopt concepts from a later layer before they need them.

Simple things should stay tiny. Medium things should get ergonomic helpers. Hard things should remain explicit and composable.

### 4. Fewer concepts, better defaults

`gocode` should compete on clarity, not feature count.

Prefer improving existing primitives over adding new abstractions:

- make `Loop` easier before adding an `Agent` type
- make tool assembly easier before adding orchestration concepts
- use `context.Context` and closures before inventing custom context objects
- keep sessions as data before adding anything runner-like
- provide recipes and examples before adding large abstractions

Every new noun should pay rent. The core vocabulary should stay small.

### 5. Tools should feel like Lego blocks

Tools are the primary place where the library can become more useful without becoming a framework.

A tool should be easy to:

- define
- test
- inspect
- compose
- replace
- sandbox
- register explicitly

Pre-built tools should be opt-in. They should expose normal `agent.Tool` definitions and normal `agent.ToolFunc` implementations. No tool should create hidden state, hidden loops, or hidden model calls.

### 6. Agent-legible by design

`gocode` should be easy for human developers and coding agents to understand.

In an LLM-assisted coding world, a library should not require a coding agent to reverse-engineer framework concepts, search through extensive docs, or memorize opaque configuration patterns before it can make useful changes.

The code should do what it looks like it does:

- `Ask` should make one model call
- `Loop` should run a visible tool loop
- `ToolFunc` should be a normal Go function
- `[]Message` should be the conversation history
- `Toolset` should be a bundle of tools and dispatch functions
- provider constructors should clearly say which provider they configure

This matters because coding agents are increasingly part of the development workflow. They work best with APIs that are explicit, local, predictable, and easy to inspect.

Design implications:

- prefer obvious names over clever abstractions
- prefer plain structs and functions over framework object graphs
- keep configuration close to the call site
- make examples copy-pasteable into real apps
- avoid hidden registration, implicit global state, and runtime magic
- expose the primitive underneath every convenience helper
- make common tasks discoverable from code completion and type signatures
- keep docs and examples aligned with the actual API shape

A good test:

> Could a coding agent understand this API from the names, types, and nearby examples without reading a long framework manual?

If yes, it probably fits the project.

### 7. Boring is good

This project should prioritize reliability, inspectability, and Go-native ergonomics over novelty.

The winning move is not "more agent stuff."

The winning move is:

> The boring, correct Go library for real LLM apps.

## Direction relative to Google ADK

`gocode` should be easier than Google ADK by having fewer concepts and less framework machinery.

ADK-style systems can be powerful, but they often ask users to learn an application model: agents, runners, session services, artifact services, memory services, invocation contexts, tool contexts, callbacks, events, and deployment patterns.

`gocode` should optimize for the opposite experience:

- start with one function call
- add one tool
- add one loop
- persist history explicitly when needed
- stream output with ordinary callbacks
- test with ordinary mocks
- deploy inside any Go program

The goal is not to match ADK feature-for-feature. The goal is to make the common path simpler and the control flow clearer.

### Easier than ADK

`gocode` should be easier because there is less to learn:

- no required runner
- no required session service
- no required artifact service
- no required memory service
- no event model to understand before building
- no deployment concept in the core library
- normal Go functions for tools
- normal Go slices for history
- normal Go maps for dispatch
- normal Go tests

### More transparent than ADK

`gocode` should be more transparent because every important operation is visible:

- every model call goes through `Ask`, `AskStream`, `Loop`, or `LoopStream`
- every tool call maps to a `ToolFunc`
- every conversation is a `[]Message`
- every provider translates through the same canonical types
- every session, when added, is just data plus explicit storage
- every higher-level helper should reveal the primitive underneath

### Easier to implement than ADK

`gocode` should stay easy to implement and reason about internally:

- providers are small interfaces
- tools are plain functions
- loops are regular Go loops
- storage is a small interface
- helpers compose existing primitives
- examples can be copied into real apps

ADK gives users an application model. `gocode` gives users application parts.

That distinction should guide every roadmap decision.

## Current baseline

The foundation is now the baseline, not future roadmap work.

Already available:

- model-agnostic provider interface
- Anthropic, OpenAI, and OpenRouter providers
- canonical `Message`, `ContentBlock`, `Tool`, and `ToolResult` types
- single-call API with `Ask`
- agent loop API with `Loop`
- streaming APIs with `AskStream` and `LoopStream`
- generic `Parallel`
- retry with exponential backoff and jitter
- rich typed errors
- usage aggregation
- comprehensive tests around core loop, retry, provider behavior, streaming, and errors
- examples for ask, pipeline, agent loop, and streaming
- rewritten quickstart focused on first-run clarity
- tool ergonomics helpers
- schema builder helpers

The roadmap below focuses only on future work: safer tool extension, easier assembly, better examples, and boring production helpers.

---

## P1 — Make practical agents dramatically easier

These are the highest-leverage next steps.

P1 is about making real agent assembly feel effortless without introducing a hidden runtime. The easy path should become much shorter, while the explicit path remains available for users who want full control.

The immediate product goal is a practical agent pattern that combines:

- safe tools
- toolsets and dispatch helpers
- explicit context management
- a basic, extensible agent block
- practical assistant recipes and one compelling example app

This should make useful agents easier to build without making one-off model calls any heavier.

### 1. Safe pre-built tool library

Priority: high.

Problem:

Every user rewrites the same common tools. This makes early examples longer and distracts from actual application logic.

Goal:

Provide a small, safe, well-tested tool library.

This is not a framework. It is a box of useful Lego bricks.

Possible package:

```go
github.com/lukemuz/gocode/agent/tools
```

Initial tool ecosystem scope should be deliberate and small.

For P1, `gocode` should focus on:

1. local safe primitives
2. MCP as the primary external tool adapter
3. transparent skills
4. tool bindings, toolsets, and middleware/wrappers

A general shell command tool can technically cover many local capabilities: grep, git, tests, package managers, code generation, and arbitrary scripts. That means every built-in tool needs to justify itself by being meaningfully better than shell in at least one way:

- safer
- more bounded
- more portable
- easier to test
- easier for models to call correctly
- easier to inspect
- less dependent on ambient machine state
- less likely to expose secrets or mutate external systems accidentally

The built-in library should focus first on primitives where typed, sandboxed implementations provide clear value over shell. External product integrations should primarily come through MCP, user-defined tools, or community packages rather than a large built-in catalog.

Initial core built-ins.

These are the strongest candidates because they are broadly useful, can be implemented safely, and avoid handing the model arbitrary command execution. This is the initial scope to design and implement first.

| Tool | Package direction | Why it deserves to exist |
|---|---|---|
| current time | `tools/clock` | safe default, useful in quickstarts |
| calculator | `tools/math` | simple demo and test utility |
| workspace list directory | `tools/workspace` | safer and more portable than shelling out to `ls` |
| workspace find files | `tools/workspace` | bounded path search with root-relative paths and ignore rules |
| workspace grep/search text | `tools/workspace` | bounded content search without arbitrary shell access |
| workspace read file | `tools/workspace` | sandboxed reads with max bytes and optional line ranges |
| workspace file info | `tools/workspace` | safe metadata without exposing a general command runner |

Possible later built-ins.

These are useful, but should not be added just because coding agents commonly need them. If shell access is already enabled, some of these may be redundant. If MCP support is available, many external capabilities may be better supplied by MCP servers.

| Tool | Package direction | Open question |
|---|---|---|
| workspace create directory | `tools/workspace` | worthwhile if bundled with sandboxing and confirmation wrappers |
| workspace write file | `tools/workspace` | worthwhile if safer than full shell writes and bounded by root |
| workspace edit file | `tools/workspace` | likely worthwhile if it supports exact replacement and expected match counts |
| workspace move path | `tools/workspace` | maybe; useful but clearly mutating |
| HTTP GET/JSON fetch | `tools/http` | maybe; network access requires allowlists, timeouts, and response limits |
| HTTP POST | `tools/http` | maybe later; external mutation requires stronger allowlists and confirmation |
| web search adapter | `tools/search` | likely defer; usually better as MCP or user-supplied provider/API key |
| read-only git status/diff/log/show | `tools/git` | maybe later; only worth it if bounded, portable, and safer than shelling out |

Tier 3: escape hatches and dangerous capabilities.

These should exist, if at all, behind intentionally loud APIs, strong documentation, timeouts, and ideally confirmation wrappers.

| Tool | Package direction | Notes |
|---|---|---|
| shell command | `tools/shell` | powerful escape hatch; may eliminate the need for many specialized local command wrappers |
| workspace delete path | `tools/workspace` | dangerous; maybe defer until edit/write semantics are mature |
| mutating git commands | `tools/git` | probably defer; shell or user-defined tools may be enough |
| arbitrary external mutation adapters | adapter packages | should be wrapped, filtered, and inspected before use |

Design shape:

```go
clock := tools.NewClock()

toolsList := []agent.Tool{
	clock.Tool,
}

dispatch := map[string]agent.ToolFunc{
	clock.Tool.Name: clock.Func,
}
```

Or:

```go
bundle := tools.NewFilesystem("/safe/root")

toolsList := bundle.Tools()
dispatch := bundle.Dispatch()
```

Principles:

- all tools are explicitly registered
- all tool implementations are visible in source
- safe defaults over convenience at all costs
- workspace and filesystem tools are sandboxed by default
- workspace paths are root-relative by default
- read-only workspace tools are the default coding-agent toolset
- write/edit/delete tools are separate explicit opt-ins
- grep/search tools must bound scanned files, result count, and output size
- file reads must bound size and support partial reads where practical
- network/HTTP tools, if added, require allowlists, timeouts, and response size limits
- shell execution is never enabled casually
- typed command wrappers should exist only when they are materially safer or clearer than shell
- shell is an escape hatch, not the default abstraction for common safe operations
- dangerous tools should be easy to wrap with confirmation, logging, and timeouts
- no hidden persistence
- no hidden background work
- no hidden model calls

### 2. Native MCP support

Priority: high.

Problem:

MCP is becoming a standard way to expose tools and external capabilities to LLM applications. If `gocode` wants to be easy to build with, users should not have to hand-wrap every MCP server as custom `Tool` and `ToolFunc` values.

Goal:

Support MCP as the primary external tool adapter for P1.

The user should be able to connect to an MCP server, inspect the tools it exposes, and adapt selected tools into normal `gocode` primitives. MCP should make the broader tool ecosystem available without `gocode` becoming an integration marketplace.

Possible API shape:

```go
server, err := mcp.Connect(ctx, mcp.Config{
	Command: "my-mcp-server",
	Args:    []string{"--stdio"},
})

toolset, err := server.Toolset(ctx)

result, err := client.Loop(ctx, system, history, toolset.Tools, toolset.Dispatch, 10)
```

Principles:

- MCP tools should become ordinary `agent.Tool` definitions and ordinary `agent.ToolFunc` handlers
- users should explicitly choose which MCP servers to connect to
- users should explicitly pass MCP tools into `Loop`
- no global MCP registry
- no hidden background tool execution
- no hidden model calls
- connection lifecycle should be explicit and closeable
- MCP schemas and tool names should be inspectable before use
- transport details should be isolated from the core `agent` package
- unsafe MCP tools should be easy to filter, rename, wrap, or sandbox

Package direction:

```text
github.com/lukemuz/gocode/agent/mcp
```

MCP support should make `gocode` easier to use with the broader tool ecosystem without turning the library into an orchestration framework.

OpenAPI note:

OpenAPI is a machine-readable standard for describing HTTP APIs, often in `openapi.yaml`, `openapi.json`, or older Swagger files. An OpenAPI adapter could eventually turn selected HTTP API operations into ordinary `gocode` tools.

OpenAPI support is useful, but it should be deferred for now. It is less urgent than MCP because MCP is becoming the common tool protocol for agents. OpenAPI also raises design questions around auth, endpoint filtering, mutating operations, pagination, response shaping, rate limits, and large schemas.

If added later, OpenAPI should follow the same rule as MCP: selected operations become ordinary `agent.Tool` definitions and ordinary `agent.ToolFunc` handlers. Users must explicitly choose the spec, allowed operations, auth, timeouts, and safety wrappers.

### 3. Native skills support

Priority: high.

Problem:

Some useful capabilities are larger than a single tool call. They are repeatable bundles of prompts, tools, constraints, examples, and instructions. These are often called "skills."

If tools are individual Lego bricks, skills are small pre-built assemblies.

Goal:

Support skills as transparent, composable bundles that can be inspected and adapted.

A skill should be able to provide:

- instructions or system prompt fragments
- one or more tools
- dispatch functions
- examples or usage notes
- optional setup/validation logic
- optional metadata such as name, description, and safety notes

Possible API shape:

```go
skill := skills.NewRepoExplainer(skills.RepoExplainerConfig{
	Root: ".",
})

system := agent.JoinInstructions(
	baseSystem,
	skill.Instructions(),
)

toolset := skill.Toolset()

result, err := client.Loop(ctx, system, history, toolset.Tools, toolset.Dispatch, 10)
```

Principles:

- a skill is not an autonomous agent
- a skill does not own the loop
- a skill does not call the model by itself
- a skill should expose ordinary tools, instructions, and configuration
- users should be able to inspect, modify, or ignore any part of a skill
- skills should compose with `Toolset`
- skills should be easy for coding agents to understand from their names and types
- skills should be useful shortcuts, not hidden runtimes

Package direction:

```text
github.com/lukemuz/gocode/agent/skills
```

Good initial skills might include:

- repo explainer
- log summarizer
- code review helper
- local docs Q&A

Issue triage and HTTP research with citations may be useful later, but they likely depend on external systems, network access, search providers, or MCP servers. They should not be part of the first skills commitment unless their tool dependencies are explicit and safe.

Skills should make common higher-level workflows easier while preserving the central promise:

> You own the data. You own the tools. You own the loop.

### 4. Tool bindings, toolsets, and dispatch helpers

Priority: medium-high.

Problem:

Even with pre-built tools, users need a simple way to merge tool definitions and dispatch maps.

Goal:

Make assembly easy while keeping the resulting `[]Tool` and `map[string]ToolFunc` obvious.

This may be more important than sessions in the near term because it directly supports the Lego-block model. Local tools, MCP tools, and skill tools should all compose through the same shape.

Possible APIs:

```go
type ToolBinding struct {
	Tool Tool
	Func ToolFunc
	Meta ToolMetadata
}

type ToolMetadata struct {
	Source               string
	ReadOnly             bool
	Destructive          bool
	Network              bool
	Filesystem           bool
	Shell                bool
	RequiresConfirmation bool
	SafetyNotes          []string
}

type Toolset struct {
	Bindings []ToolBinding
}

func (t Toolset) Tools() []Tool
func (t Toolset) Dispatch() map[string]ToolFunc
```

```go
toolset, err := tools.Join(
	tools.NewClock(),
	workspace.NewReadOnly(workspace.Config{
		Root: ".",
	}),
	mcpToolset,
)

toolset = toolset.Wrap(
	tools.WithTimeout(5 * time.Second),
	tools.WithResultLimit(20_000),
	tools.WithLogging(logger),
)

result, err := client.Loop(ctx, system, history, toolset.Tools(), toolset.Dispatch(), 10)
```

Middleware and wrappers:

- timeout
- result size limit
- logging
- confirmation
- panic recovery
- redaction
- retry, only when explicitly configured

Middleware clarification:

Tools may be requested by the model during `Loop`, but they are still executed by application-owned Go functions in the dispatch map. The model chooses a tool name and JSON arguments. `gocode` looks up the matching `ToolFunc` and calls it.

That dispatch boundary is where middleware applies.

Middleware should wrap tool bindings before they are passed into `Loop`. It should decorate the ordinary `ToolFunc` implementation with behavior such as logging, timeout, confirmation, result limiting, redaction, or retry. The wrapped function still has the same `ToolFunc` signature, so the core loop does not need to know that middleware exists.

Middleware should probably operate on `ToolBinding` rather than only bare `ToolFunc` values, because useful wrappers often need access to the tool name, schema, source, metadata, and safety notes. Internally, the wrapper still replaces `binding.Func` with another ordinary `ToolFunc`.

This also means tools are more universal than agent calls. A tool can be executed by the model loop, a deterministic workflow, a test, a CLI command, an HTTP handler, or a skill validation step. The same binding and middleware model should work in all of those cases.

MCP tools follow the same rule. An MCP adapter exposes each remote MCP tool as a local `ToolFunc` that sends a call to the MCP server. Middleware can wrap that local adapter function just like any other tool.

Principles:

- no global registry
- no implicit registration
- no hidden tool execution policy
- duplicate tool names should produce clear errors
- users can inspect and modify the result before passing it to `Loop`
- middleware wraps ordinary `ToolFunc` values rather than changing the core loop
- metadata is advisory and inspectable, not a hidden permission engine
- logging and confirmation should be interfaces supplied by the application, not hard dependencies on a logging framework or UI

### 5. Explicit context management

Priority: high.

Problem:

`gocode` sends whatever `[]Message` the caller provides. Useful tool-using agents quickly create context pressure because tool calls and tool results pile up.

Goal:

Provide context helpers that make budget management explicit, configurable, and easy to include in the practical agent pattern.

Possible API:

~~~go
type ContextManager struct {
	MaxTokens    int
	KeepRecent   int
	KeepFirst    int
	TokenCounter func([]Message) (int, error)
	Summarizer   func(context.Context, []Message) (string, error)
}

func (m ContextManager) Trim(ctx context.Context, history []Message) ([]Message, error)
~~~

Principles:

- `Loop` should still send the history it is given
- context management should be explicit at the call site or in the assistant config
- original history should not be mutated
- trimming should preserve tool-use/tool-result integrity
- summarization should only happen when explicitly configured
- model calls for summarization should be visible and caller-owned
- summarization should usually be application-driven, not exposed as a model-callable tool
- context management should be part of the recommended practical agent recipe, not an obscure advanced feature

### 6. Basic, extensible agent block

Priority: high.

Problem:

The primitive `Loop` is intentionally explicit, but real applications should not need to repeatedly hand-wire the same context, toolset, and loop glue. Many useful assistants need the same assembled block: context management, tool dispatch, loop execution, hooks, and updated history.

Goal:

Provide a basic, extensible agent block: an assembled primitive with batteries included, but designed to be customized, embedded, and built on.

The goal is not to ship a complete Claude Code-style product. The goal is to provide the reusable agent block that many Claude Code-style systems, repo assistants, internal copilots, and tool-using applications need.

Possible API:

~~~go
type Assistant struct {
	Client  *Client
	System  string
	Tools   Toolset
	Context ContextManager
	MaxIter int
	Hooks   Hooks
}

func (a Assistant) Step(ctx context.Context, history []Message) (LoopResult, error)
~~~

Conceptually, `Step` should still be equivalent to ordinary Go code:

1. trim history if context management is configured
2. call `Client.Loop`
3. return the `LoopResult`

This block should have batteries, but it should invite customization. Users should be able to swap the client, tools, context manager, summarizer, hooks, model, prompts, and storage strategy without changing the underlying data model.

It should feel like a reusable component that can live inside a CLI command, HTTP handler, worker, test, or larger agent system.

Principles:

- no hidden persistence
- no background runtime
- no scheduler
- no graph executor
- no global tool registry
- caller owns history
- caller decides when a step runs
- desugared behavior should be documented
- users can drop down to `Loop` at any time

This is the middle layer: more ergonomic than raw `Loop`, but still built from the same primitives.

In short:

> An assembled agent primitive, not an application runner.

### 7. Provider setup helpers

Priority: medium-high.

Problem:

The current provider setup is explicit and fine, but quick examples still require repeated environment-variable boilerplate.

Goal:

Offer small convenience helpers for common setup without hiding configuration.

Possible APIs:

```go
provider, err := agent.AnthropicFromEnv()
provider, err := agent.OpenAIFromEnv()
provider, err := agent.OpenRouterFromEnv()
```

Potentially:

```go
client, err := agent.NewAnthropicClientFromEnv(agent.ModelSonnet)
```

Principles:

- helpers should be clearly named
- missing environment variables should return normal errors
- explicit provider constructors remain the canonical path
- no global default client
- no package-level hidden state
- no automatic provider selection unless explicitly requested

### 8. Recipes documentation

Priority: medium-high.

Problem:

To be easier than ADK, the library needs many small copy-pasteable recipes, not just architectural explanation.

Goal:

Add recipe-style docs that teach one pattern at a time.

Possible recipes:

- ask a model
- continue a conversation
- add one tool
- use a typed tool
- use a safe filesystem tool
- build a basic assistant step
- add explicit context management to an assistant
- compose an assistant from a toolset
- desugar an assistant step into `ContextManager.Trim` plus `Client.Loop`
- stream to a terminal
- stream over SSE
- retry on rate limits
- persist history
- test a loop
- switch providers
- fan out with `Parallel`
- build a repo explainer

Principles:

- recipes should be small
- recipes should be copy-pasteable
- recipes should fit naturally into existing Go programs
- recipes should show the primitive underneath
- recipes should avoid fake tools when a real local deterministic tool is possible

### 9. One compelling example app

Priority: high.

Problem:

Small examples demonstrate API shape, but not enough practical value.

Goal:

Add one beautiful example app that a Go developer could adapt at work.

Good candidates:

- repo explainer CLI
- issue triage assistant
- log summarizer
- local docs Q&A over files
- HTTP research assistant with citations
- Go code review helper

Recommended first choice:

> repo explainer CLI

Why:

- useful to developers immediately
- showcases filesystem tools
- can use streaming
- can use context trimming later
- demonstrates explicit loop ownership
- avoids external search/API dependencies
- easy to run locally
- naturally shows why safe filesystem sandboxing matters

Possible behavior:

```bash
go run ./examples/repo-explainer --path .
```

The app could:

1. list project files
2. read selected files
3. summarize architecture
4. answer a user question
5. stream output to the terminal

Principles:

- do not make it a toy calculator
- keep it understandable
- show composition of primitives
- keep user control visible
- include safety boundaries

---

## P2 — Production helpers that preserve control

### 1. Assistant hardening

Priority: high after the basic assistant pattern exists.

Problem:

Once the practical assistant pattern exists, it needs to remain thin, inspectable, and easy to bypass as production features are added.

Goal:

Make sure context management, hooks, streaming, sessions, and tool middleware compose with the assistant step without turning it into a hidden runtime.

Principles:

- assistant helpers should remain desugarable to `ContextManager.Trim` plus `Client.Loop`
- persistence should stay caller-owned
- hooks should observe rather than control execution
- streaming should use explicit callbacks
- tool policy should live in toolsets and middleware, not hidden assistant behavior
- advanced users should be able to drop down to primitives without changing data models

### 2. Boring sessions, but no runner

Priority: medium.

Problem:

Real applications need conversation history to survive across requests and restarts.

Goal:

Add a minimal `Session` and `Store` abstraction that remains a transparent wrapper around `[]Message`.

Possible API:

```go
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
```

Built-in stores:

| Store | Notes |
|---|---|
| `MemoryStore` | tests and development |
| `FileStore` | JSON files, single-instance apps |

Principles:

- sessions do not call models
- sessions do not run tools
- sessions do not trim automatically unless explicitly asked
- sessions do not introduce a runtime
- sessions do not require a runner
- `Session.History` is plain `[]Message`
- external database stores should live outside the core unless very small

The point is persistence, not orchestration.

A session should be used like this:

```go
result, err := client.Loop(ctx, system, session.History, tools, dispatch, 5)
session.History = result.Messages
```

The core should not add a `Runner` abstraction that owns this flow.

### 3. Observability hooks

Priority: medium.

Problem:

Users need visibility into requests, responses, tool calls, tool results, latency, and errors.

Goal:

Add optional hooks as a side channel for logging/tracing, not as a lifecycle framework.

Possible API:

```go
type Hooks struct {
	OnRequest    func(ctx context.Context, iter int, req ProviderRequest)
	OnResponse   func(ctx context.Context, iter int, resp ProviderResponse, dur time.Duration)
	OnToolCall   func(ctx context.Context, iter int, name string, input json.RawMessage)
	OnToolResult func(ctx context.Context, iter int, name string, result ToolResult, dur time.Duration)
	OnError      func(ctx context.Context, iter int, err error)
}
```

Principles:

- zero value means no hooks
- hooks are optional
- hooks should not control the loop
- hooks should not become middleware
- hooks should not hide execution flow
- callbacks should be documented as synchronous or asynchronous

### 4. Extended model configuration

Priority: medium.

Problem:

`Config` exposes only basic model settings. Real usage often needs generation controls.

Goal:

Thread optional generation parameters through `ProviderRequest`.

Possible fields:

```go
type Config struct {
	Provider   Provider
	Model      string
	MaxTokens  int
	Retry      RetryConfig
	Hooks      Hooks

	Temperature   float64
	TopP          float64
	StopSequences []string
}
```

Principles:

- zero values preserve provider defaults
- provider-specific unsupported fields are ignored or documented
- avoid turning `Config` into a giant vendor-specific surface
- escape hatches can live in provider configs if needed
- keep the common case short
- allow advanced users to take on complexity explicitly

### 5. Testing helpers

Priority: medium.

Problem:

The provider interface is already a good testing seam, but users will benefit from standard helpers.

Goal:

Add small utilities for deterministic tests.

Possible helpers:

```go
provider := agent.NewMockProvider(
	agent.ProviderResponse{
		Content: []agent.ContentBlock{
			{Type: agent.TypeText, Text: "hello"},
		},
		StopReason: "end_turn",
	},
)
```

Or a scripted provider:

```go
provider := agent.ScriptedProvider{
	Responses: []agent.ProviderResponse{...},
}
```

Principles:

- helpers should be tiny
- helpers should model provider contracts
- do not assert exact LLM text in library examples
- encourage testing history shape, tool calls, errors, and usage

### 6. ADK comparison doc

Priority: medium.

Problem:

`gocode` should be easier than Google ADK, but that difference should be explained clearly and fairly without turning the README into a comparison essay.

Goal:

Add a focused comparison document that helps users understand the tradeoff.

Possible file:

```text
COMPARISON.md
```

Possible structure:

| Concern | Google ADK-style approach | `gocode` approach |
|---|---|---|
| Agent definition | Framework objects | Prompts, `Client`, `Ask`, and `Loop` |
| Tools | Framework tool abstractions | `Tool` plus `ToolFunc` |
| Context | ADK context objects | `context.Context` plus closures |
| Session | Session service | Optional `Session` data plus `Store` |
| Runner | Framework runner | Your `main`, HTTP handler, CLI command, worker, or goroutine |
| Memory | Built-in service concepts | Explicit interfaces or separate packages |
| Deployment | Platform-oriented | Bring your own deployment |
| Debugging | Follow framework lifecycle | Inspect messages, tools, dispatch, and loop results |
| Testing | Mock framework components | Mock `Provider`, call public APIs |

Principles:

- be fair, not combative
- explain when ADK is a good fit
- explain when `gocode` is a better fit
- focus on concepts users must learn
- emphasize ordinary Go code
- emphasize visible control flow
- do not frame the goal as feature parity

### 7. HTTP/SSE service example

Priority: medium.

Problem:

Many Go users are building services, not just CLIs. The examples should show that `gocode` fits naturally inside ordinary `net/http` programs without requiring a framework runtime.

Goal:

Add a small service example that streams model output over Server-Sent Events.

Possible example:

```text
examples/http-sse-chat
```

The example should show:

- a normal `net/http` server
- explicit request parsing
- explicit history loading/saving
- `AskStream` or `LoopStream`
- SSE response writing
- no web framework requirement
- no hidden session runtime
- no runner abstraction

Possible flow:

1. HTTP handler receives a user message.
2. Handler loads or creates history.
3. Handler appends the user message.
4. Handler calls `AskStream` or `LoopStream`.
5. Handler writes deltas as SSE events.
6. Handler stores the updated history.

Principles:

- copy-pasteable into a real service
- ordinary Go HTTP primitives
- visible error handling
- visible history management
- no global state unless clearly called out as demo-only
- no framework-owned lifecycle

---

## P3 — Useful, but design carefully

### 1. Evaluation helpers

Priority: medium-low.

Goal:

Help users regression-test agent behavior.

Possible API:

```go
type EvalCase struct {
	Name     string
	Input    []Message
	Tools    []Tool
	Dispatch map[string]ToolFunc
	Assert   func(t *testing.T, result LoopResult)
}
```

Principles:

- evaluation should be a testing helper, not a hosted platform
- no hidden model management
- no database requirement
- user owns assertions

### 2. Lightweight multi-agent composition

Priority: low.

Goal:

Maybe provide helpers for patterns like routing, fan-out/fan-in, critique, or delegation.

Risk:

This can easily become framework territory.

Principles:

- no graph runtime in core
- no hidden scheduler
- no hidden state
- no autonomous agent registry
- no opaque lifecycle
- helpers should just be functions over `Client`, `Message`, `Tool`, and `LoopResult`
- strongly consider a separate package

The library already has enough primitives for users to build many of these patterns themselves.

### 3. Cross-session memory

Priority: low.

Goal:

Allow searching across past sessions or external knowledge.

Risk:

This pulls in embeddings, vector stores, chunking, ranking, and persistence concerns.

Principles:

- likely separate package
- no built-in vector database in core
- integrate through interfaces
- keep `Session` boring
- do not create hidden memory behavior inside `Loop`

---

## Deferred or non-goals

These are intentionally out of scope for the core library:

- full orchestration frameworks
- graph executors
- hidden schedulers
- visual workflow builders
- no-code agent configuration
- managed deployment
- built-in HTTP server scaffolding
- built-in vector database
- global tool registries
- implicit persistence
- autonomous background agents
- opaque agent-to-agent runtimes
- core `Runner` abstractions that own execution flow
- required custom context objects for normal tool use
- ADK-style application object graphs

You can build many of these things on top of `gocode`, but the core library should remain small and composable.

---

## Implementation order

| Order | Item | Priority | Status |
|---|---|---|---|
| 1 | Safe pre-built tools | P1 | Next |
| 2 | Tool bindings/toolsets/dispatch helpers | P1 | Next |
| 3 | Explicit context management | P1 | Next |
| 4 | Basic, extensible agent block | P1 | Next |
| 5 | Recipes documentation | P1 | Next |
| 6 | Compelling example app | P1 | Next |
| 7 | Native MCP support | P1 | Next |
| 8 | Native skills support | P1 | Next |
| 9 | Provider setup helpers | P1 | Next |
| 10 | Assistant hardening | P2 | Planned |
| 11 | Boring sessions, but no runner | P2 | Planned |
| 12 | Observability hooks | P2 | Planned |
| 13 | Extended model configuration | P2 | Planned |
| 14 | Testing helpers | P2 | Planned |
| 15 | ADK comparison doc | P2 | Planned |
| 16 | HTTP/SSE service example | P2 | Planned |
| 17 | Evaluation helpers | P3 | Future |
| 18 | Lightweight multi-agent composition | P3 | Future / cautious |
| 19 | Cross-session memory | P3 | Future / likely separate package |

---

## Future focus

The core primitives are now in place. The next phase should make those primitives easier to assemble into useful applications without changing the execution model.

The highest-leverage work is:

1. safe pre-built tools
2. toolsets and dispatch helpers
3. explicit context management
4. a basic, extensible agent block
5. small, copy-pasteable recipes
6. one compelling real example app
7. MCP support as a transparent external tool adapter
8. skills as inspectable bundles of instructions, tools, examples, and metadata

Production helpers should follow the same rule:

> Make the common path easier while keeping execution visible.

That means context management, sessions, hooks, testing helpers, evaluation helpers, and service examples should all preserve the central promise:

> You own the data. You own the tools. You own the loop.

The design north star is:

> Easy things easy. Hard things possible. Nothing hidden.

Or, stated relative to ADK:

> Simple tasks should not pay the full framework tax. Complex tasks should not require fighting the abstraction.

It should be easy to do the easy stuff. It should also allow as much complexity as users want to take on — but the complexity should remain explicit, elegant, and built from ordinary Go pieces.
