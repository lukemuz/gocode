# Vision

`gocode` is a Go library for building LLM-powered software that scales from one function call to serious agent systems without forcing the serious-agent shape onto the first function call.

The project is built around a simple idea:

> Make easy things easy without making advanced things harder.

That means `gocode` should provide small, inspectable primitives for developers who want full control, while also offering thin, practical assembly layers for developers who want to build useful agents without rewriting the same glue code over and over.

The goal is not to avoid every framework-shaped convenience. The goal is to avoid abstractions that take agency away from the developer.

Convenience should compress boilerplate, not hide control.

## The core promise

> You own the data. You own the tools. You own the loop.

In practice:

- conversation history is plain data
- tools are normal Go functions
- providers implement small interfaces
- loops are visible and understandable
- model calls are explicit
- persistence is explicit
- context management is explicit
- higher-level helpers are built from ordinary primitives
- every convenience layer can be inspected, bypassed, or replaced

`gocode` should feel like Go:

- plain structs
- plain functions
- explicit errors
- explicit configuration
- easy testing
- minimal magic
- clear escape hatches

## Progressive complexity

Many agent frameworks optimize for the fully-loaded case.

Once you accept their application model, hard things can become relatively easy. But simple things often pay the same setup cost as complex things: agents, runners, sessions, callbacks, artifacts, memory services, graph runtimes, and framework-owned lifecycle concepts.

`gocode` should avoid that flat complexity curve.

The desired shape is:

| Task size | `gocode` experience |
|---|---|
| Simple task | Tiny setup |
| Medium task | Ergonomic assembly |
| Hard task | Explicit composition |

A one-off model call should not require an agent object.

A basic tool-using assistant should not require hand-writing the same context, toolset, and loop glue every time.

A complex production system should not require fighting the abstraction or rewriting into a different conceptual model.

The library should scale in layers:

1. **Primitives** — `Client`, `Provider`, `Message`, `Tool`, `ToolFunc`, `Ask`, `Loop`, streaming, retries, typed errors.
2. **Assembly helpers** — typed tools, schema helpers, toolsets, context managers, session stores, hooks, provider setup helpers.
3. **Recipes** — practical patterns such as a basic assistant, repo explainer, HTTP/SSE chat, and tool-using agent with context management.
4. **Advanced composition** — MCP, skills, evaluation, replay, multi-step workflows, and user-owned orchestration.

Each layer should be useful on its own. No layer should force users to adopt concepts from a later layer before they need them.

## Not anti-framework. Anti-trap.

The project is not allergic to convenience. It is allergic to loss of control.

A good abstraction:

- removes repetitive glue
- has obvious behavior
- exposes the primitives underneath
- is easy to bypass
- composes with user code
- fails visibly
- keeps data inspectable
- can be explained in one sentence
- can be rewritten by a user in a small amount of ordinary Go

A bad abstraction:

- hides model calls
- hides tool execution
- hides memory mutation
- hides persistence
- requires global registration
- owns application lifecycle
- introduces a scheduler or graph runtime in the core
- makes simple things require framework setup
- makes advanced things require fighting the framework

The enemy is not the word "framework."

The enemy is forcing every user into the same application model.

`gocode` should provide batteries-included paths, but the batteries should be removable.

## The basic agent pattern

The explicit primitives are the foundation, but a practical library also needs a blessed middle path.

A useful agent application usually needs:

- a client
- a system prompt
- a toolset
- a context budget
- optional summarization
- max tool-loop iterations
- optional hooks
- caller-owned history

Developers should not need to rewrite that assembly every time.

So `gocode` should provide a basic, extensible agent block: an assembled primitive that trims context if configured, calls `Loop`, and returns updated messages.

This is acceptable because it does not introduce a new runtime. It is one commonly needed block packaged in an inspectable form.

The goal is not to ship a complete Claude Code-style product. The goal is to provide the reusable agent block that many Claude Code-style systems, repo assistants, internal copilots, and tool-using applications need.

The helper should be equivalent to ordinary Go code:

1. receive `[]Message`
2. trim or summarize history if configured
3. call `Client.Loop` with the configured tools
4. return `LoopResult`

This block should have batteries, but it should invite customization. Users should be able to swap the client, tools, context manager, summarizer, hooks, model, prompts, and storage strategy without changing the underlying data model.

It should feel like a reusable component that can live inside a CLI command, HTTP handler, worker, test, or larger agent system.

It should not own:

- persistence
- background memory
- deployment
- scheduling
- graph routing
- global tool registration
- autonomous background work

The caller should still decide when a step runs, where history is stored, which tools are available, and how results are handled.

In short:

> An assembled agent primitive, not an application runner.

## Context management belongs in the practical path

Context management should not be treated as an obscure production feature.

Any useful tool-using agent eventually creates context pressure. Tool results pile up, conversations grow, and models have finite context windows.

The primitive `Loop` should remain simple: it should send the history it is given.

But the recommended practical agent pattern should include explicit context management.

The right shape is:

- the application owns the history
- the application configures a context manager
- the context manager returns a new `[]Message`
- the original history is not mutated
- summarization only happens if the user provides or explicitly enables a summarizer
- model calls for summarization are visible and configurable
- tool-use/result integrity is preserved

"Summarize context" should usually not be a model-callable tool. The model should not normally decide when memory is compacted. The application should decide based on budget, request boundaries, or policy.

The summarizer itself may use an LLM internally, but that should be caller-owned and explicit.

## Tools, subagents, and skills

A **tool** is a concrete capability the model may request during a loop.

Examples:

- read a file
- list a directory
- grep a workspace
- get the current time
- calculate
- fetch a URL, if explicitly enabled

A tool should be bounded, inspectable, and explicitly registered.

A **subagent** is a user-owned LLM workflow invoked by application code.

Examples:

- summarize a large context segment
- review a diff
- classify an issue
- investigate a failing test with read-only tools

A subagent does not need to be a magical core abstraction. In most cases it can be a normal Go function that calls `Ask`, `Loop`, or a basic-agent helper with specialized prompts and tools.

A **skill** is an inspectable bundle of instructions, tools, examples, and metadata.

A skill is not an autonomous agent. It should not own the loop or call the model by itself. It should expose ordinary pieces that the caller can inspect and compose.

## Agent-legible by design

`gocode` should be easy for both humans and coding agents to understand.

A coding agent should be able to inspect nearby names, types, and examples and make useful changes without reverse-engineering a hidden runtime.

That implies:

- obvious names
- local configuration
- explicit data flow
- ordinary Go functions
- no hidden global state
- no implicit registration
- no framework-owned lifecycle
- examples that match real usage
- convenience helpers that reveal their underlying primitives

A good test for any API is:

> Could a coding agent understand this from the names, types, and nearby examples without reading a long framework manual?

If yes, it probably fits.

## Relationship to ADK-style systems

ADK-style systems can make complex agent applications easier by giving users a full application model: agents, runners, sessions, memory services, artifact services, callbacks, events, and deployment patterns.

That can be useful.

But it also means simple tasks often require the same conceptual setup as complex tasks.

`gocode` should optimize for a different experience:

- one model call is one model call
- one tool loop is one tool loop
- a practical assistant is a thin assembly of visible parts
- production features are opt-in
- hard things are composed from the same pieces as easy things

The point is not to match ADK feature-for-feature.

The point is to preserve a smoother complexity curve.

## Product principles

### 1. Start tiny

The smallest useful program should stay small.

Users should be able to call a model without understanding tools, sessions, context managers, skills, MCP, or observability.

### 2. Add power progressively

Each new capability should be adoptable independently.

A user should be able to add:

- a tool without a session
- streaming without a framework runtime
- context trimming without persistence
- persistence without a runner
- MCP without a global registry
- skills without autonomous agents

### 3. Keep primitives visible

Every higher-level helper should expose or clearly map to the lower-level primitives.

Documentation should show the desugared version of important helpers.

### 4. Make the common path short

If many users need the same glue, the library should probably provide a helper.

Boilerplate is not a virtue.

Explicitness should mean visible and understandable, not repetitive and tedious.

### 5. Avoid owning the application

`gocode` should not own:

- the process
- the server
- the scheduler
- the session lifecycle
- the persistence policy
- the deployment model
- the tool registry
- the memory policy

Those belong to the application.

### 6. Prefer boring, reliable pieces

The library should prioritize reliability, inspectability, and Go-native ergonomics over novelty.

The winning move is not more agent vocabulary.

The winning move is:

> The boring, correct Go library for real LLM apps.

## Roadmap implication

The roadmap should prioritize features that improve the practical path without compromising the primitive path.

Near-term priorities should include:

1. safe pre-built tools
2. toolsets and dispatch helpers
3. explicit context management
4. a basic, extensible agent block
5. recipe documentation
6. a compelling repo explainer example
7. MCP as a transparent tool adapter
8. skills as inspectable bundles
9. boring sessions and hooks

This ordering keeps the project focused on the real developer experience:

- simple things stay simple
- useful agents require less glue
- advanced systems remain explicit and composable

## North star

`gocode` should scale from a tiny function call to a serious production agent without changing what the program fundamentally is:

ordinary Go code, visible data flow, explicit control.

The shortest version:

> Easy things easy. Hard things possible. Nothing hidden.