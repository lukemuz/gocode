# Vision

`gocode` is a Go library for building LLM-powered software that scales from one function call to serious agent systems without forcing the serious-agent shape onto the first function call.

The short version:

> Easy things easy. Hard things possible. Nothing hidden.

The core promise:

> You own the data. You own the tools. You own the loop.

## Why this exists

Many agent frameworks optimize for the fully-loaded case. Once you accept their application model, hard things can become easier: agents, runners, sessions, callbacks, artifacts, memory services, graph runtimes, and deployment patterns all have a place.

The tradeoff is that simple things often pay the same conceptual setup cost as complex things.

`gocode` should provide a smoother complexity curve:

| Task size | `gocode` experience |
|---|---|
| Simple task | Tiny setup |
| Medium task | Ergonomic assembly |
| Hard task | Explicit composition |

A one-off model call should not require an agent object. A useful tool-using assistant should not require rewriting the same glue forever. A production system should not require fighting hidden framework ownership.

## What the promise means

In practice:

- conversation history is plain `[]Message` data
- tools are normal Go functions
- providers implement small interfaces
- model calls are explicit
- loops are visible and understandable
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

## Layers of complexity

The library should scale in layers:

1. **Primitives** — `Client`, `Provider`, `Message`, `Tool`, `ToolFunc`, `Ask`, `Loop`, streaming, retries, typed errors.
2. **Assembly helpers** — typed tools, schema helpers, toolsets, context managers, assistant steps, provider setup helpers, middleware, hooks.
3. **Recipes** — practical patterns such as basic assistants, repo explainers, HTTP/SSE chat, persistence, testing, and tool use.
4. **Advanced composition** — MCP, evaluation, replay, multi-step workflows, and user-owned orchestration.

Each layer should be useful on its own. No layer should force users to adopt concepts from a later layer before they need them.

## Good convenience vs bad abstraction

`gocode` is not allergic to convenience. It is allergic to loss of control.

A good abstraction:

- removes repetitive glue
- has obvious behavior
- exposes the primitives underneath
- is easy to bypass
- composes with user code
- fails visibly
- keeps data inspectable
- can be explained in one sentence
- can be rewritten by a user in ordinary Go

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

The enemy is not the word "framework." The enemy is forcing every user into the same application model.

## The practical assistant pattern

The explicit primitives are the foundation, but a practical library also needs a blessed middle path.

A useful assistant often needs:

- a client
- a system prompt
- a toolset
- a context budget
- optional summarization
- max loop iterations
- optional hooks
- caller-owned history

Developers should not need to hand-wire that every time.

So `gocode` provides an assembled assistant primitive: trim context if configured, call `Loop`, and return updated messages.

That helper should remain equivalent to ordinary Go code:

1. receive `[]Message`
2. trim or summarize history if configured
3. call `Client.Loop` with configured tools
4. return `LoopResult`

It should not own:

- persistence
- background memory
- deployment
- scheduling
- graph routing
- global tool registration
- autonomous background work

In short:

> An assembled agent primitive, not an application runner.

## Context management is practical, not exotic

Any useful tool-using agent eventually creates context pressure. Tool results pile up, conversations grow, and models have finite windows.

The primitive `Loop` should remain simple: it sends the history it is given.

The practical path should include explicit context management:

- the application owns the history
- the application configures the context manager
- the context manager returns a new `[]Message`
- the original history is not mutated
- summarization happens only when explicitly configured
- model calls for summarization are visible and caller-owned
- tool-use/tool-result integrity is preserved

The model should not normally decide when memory is compacted. The application should decide based on budget, request boundaries, or policy.

## Tools, MCP, and subagents

A **tool** is a concrete capability the model may request during a loop: read a file, list a directory, get the current time, calculate, or call a bounded external adapter.

A tool should be bounded, inspectable, and explicitly registered.

**MCP** is an adapter path: remote MCP tools become ordinary `ToolBinding` values. Users choose the server, inspect the tools, and pass selected tools into the loop.

A **subagent** is a user-owned LLM workflow invoked by application code. In most cases it can be a normal Go function that calls `Ask`, `Loop`, or an assistant step with specialized prompts and tools.

## Agent-legible by design

`gocode` should be easy for humans and coding agents to understand.

That implies:

- obvious names
- local configuration
- explicit data flow
- ordinary Go functions
- no hidden global state
- no implicit registration
- no framework-owned lifecycle
- examples that match real usage
- helpers that reveal their underlying primitives

A good API test:

> Could a coding agent understand this from the names, types, and nearby examples without reading a long framework manual?

If yes, it probably fits.

## Relationship to ADK-style systems

ADK-style systems can be powerful. They offer a full application model: agents, runners, sessions, memory services, artifact services, callbacks, events, and deployment patterns.

`gocode` optimizes for a different experience:

- one model call is one model call
- one tool loop is one tool loop
- a practical assistant is a thin assembly of visible parts
- production features are opt-in
- hard things are composed from the same pieces as easy things

The point is not feature parity. The point is a smoother complexity curve and clearer ownership.

## Product principles

1. **Start tiny.** The smallest useful program should stay small.
2. **Add power progressively.** Each capability should be adoptable independently.
3. **Keep primitives visible.** Every helper should expose or clearly map to lower-level pieces.
4. **Make the common path short.** Boilerplate is not a virtue.
5. **Avoid owning the application.** The process, server, scheduler, session lifecycle, persistence policy, deployment model, tool registry, and memory policy belong to the application.
6. **Prefer boring, reliable pieces.** The winning move is not more agent vocabulary; it is the boring, correct Go library for real LLM apps.

## Roadmap implication

Prioritize features that improve the practical path without compromising the primitive path:

1. streaming retry helpers and recipes
2. practical recipe docs
3. a repo explainer example
5. boring sessions and durable tool execution
6. observability, testing, and service examples

The design north star remains:

> Ordinary Go code, visible data flow, explicit control.
