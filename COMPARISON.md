# `gocode` vs ADK

This document is a fair, code-first comparison between `gocode` and Google's
Agent Development Kit (ADK). It is not a marketing piece. Where ADK is the
better fit, this document says so.

For the philosophy that motivates `gocode`, see [`VISION.md`](VISION.md). For
the recipes that match the patterns below, see [`RECIPES.md`](RECIPES.md).

## The short version

ADK provides a full agent application model: agents, runners, sessions,
state, events, callbacks, artifact services, memory services, and deployment
targets. It is comprehensive and opinionated.

`gocode` provides a small set of primitives — `Client`, `Provider`,
`Message`, `Tool`, `Loop`, `Assistant`, `Toolset`, `ContextManager`, `Session`
— and lets you compose them with ordinary Go.

The tradeoff:

| Concern | ADK | `gocode` |
|---|---|---|
| Time to first model call | medium (set up agent + runner) | low (one function call) |
| Time to a multi-tool agent | low (declarative) | low (assemble a Toolset) |
| Time to a router with specialists | low (sub-agent transfer) | low (subagents-are-tools recipe) |
| Time to streaming + retries | low (built-in) | low (`StreamBuffer` + `RetryConfig.OnRetry`) |
| Time to persistent sessions | low (`SessionService`) | low (`Store` interface, `FileStore`) |
| Cross-session memory + vector search | medium (built-in `MemoryService`) | not in core (separate package) |
| Trajectory eval | yes (built-in) | offline helpers planned |
| Live audio/video streaming | yes (Gemini Live) | no |
| Managed deployment | yes (Vertex Agent Engine) | no |
| Lines of code per agent | higher (framework boilerplate) | lower (ordinary Go) |
| Lock-in to a specific application model | high | low |

If you need live audio, hosted deployment, or vector-store-backed memory out
of the box, ADK is the better fit. For everything else, `gocode` aims to be
smaller, more legible, and easier to debug — and to compose with the rest of
your Go code without dragging a runtime in with it.

## When to pick which

**Pick ADK when:**

- You're already on Google Cloud and want Vertex Agent Engine deployment.
- You need bidirectional live audio/video streaming.
- You want a built-in vector-backed memory service without writing one.
- Your team prefers a declarative agent configuration model with strong defaults.
- You need ADK's eval dashboards for non-engineer stakeholders.

**Pick `gocode` when:**

- Your service is in Go and you want the agent code to feel like Go.
- You want to debug agents by reading code, not by understanding a runtime.
- You want explicit ownership of history, persistence, and the tool loop.
- You want the simple cases to be simple, not framework-shaped.
- You want to compose agents with your existing services using ordinary Go primitives.
- You don't want a Python runtime adjacent to your Go service.

## Worked comparison: a router with specialists

This is the most common multi-agent pattern: one orchestrator decides which
specialist should handle a request, the specialist does the work, the
orchestrator returns the result.

### ADK shape (sketch)

ADK models this with sub-agents and an `LlmAgent` that can transfer control:

```python
# Pseudocode — illustrative ADK shape
researcher = LlmAgent(
    name="researcher",
    model="gemini-2.0-flash",
    instruction="You are a research specialist...",
    tools=[search_tool, read_url_tool],
)

writer = LlmAgent(
    name="writer",
    model="gemini-2.0-flash",
    instruction="You are a writer...",
    tools=[],
)

orchestrator = LlmAgent(
    name="orchestrator",
    model="gemini-2.0-pro",
    instruction="Route research to researcher, drafting to writer.",
    sub_agents=[researcher, writer],
)

runner = Runner(agent=orchestrator, session_service=...)
session = session_service.create_session(...)
events = runner.run(session_id=session.id, new_message=...)
```

What's happening: the orchestrator's LLM emits a transfer-to-agent action,
the runner routes the conversation to the named sub-agent, the sub-agent runs
its own loop with its own tools, and control returns to the orchestrator.
Events for the entire trajectory live in one session.

### `gocode` shape

In `gocode`, a subagent is a `ToolFunc` that happens to call `Loop`. The
parent's dispatch map *is* the routing mechanism.

```go
// Build a researcher subagent and expose it as a tool.
researchTool, researchFn, _ := agent.NewTypedTool(
    "research",
    "Delegate a research task to a specialist with web tools.",
    agent.Object(
        agent.String("task", "What to research", agent.Required()),
    ),
    func(ctx context.Context, in struct{ Task string `json:"task"` }) (string, error) {
        result, err := client.Loop(ctx,
            "You are a research specialist. Be thorough.",
            []agent.Message{agent.NewUserMessage(in.Task)},
            researchTools.Tools(), researchTools.Dispatch(), 8,
        )
        if err != nil {
            return "", err
        }
        return agent.TextContent(result.Messages[len(result.Messages)-1]), nil
    },
)

// Build a writer subagent the same way (omitted).

orchestrator := agent.Assistant{
    Client: client,
    System: "Route research tasks to research, drafting to write.",
    Tools: agent.Toolset{Bindings: []agent.ToolBinding{
        {Tool: researchTool, Func: researchFn},
        {Tool: writeTool,    Func: writeFn},
    }},
    MaxIter: 10,
}

result, err := orchestrator.Step(ctx, history)
```

What's happening: the orchestrator's LLM emits a `tool_use` for `research`,
the dispatch map runs the function, the function spins up its own `Loop` with
its own tools and prompt, and the result string is returned to the
orchestrator as a tool result. Recursion just works; parallel sub-agent calls
in one turn run concurrently because `runTools` already does that.

### What you give up vs what you get

You give up:

- **Cross-agent shared state.** Each subagent sees only its task input.
  When you need shared state, you pass it explicitly through the input schema.
- **A single unified event log spanning parent and child.** The parent sees
  the subagent's output, not its intermediate steps. (When the events
  `Recorder` lands, you'll be able to wire one across both, but explicitly.)

You get:

- **Token efficiency.** The subagent's 30 tool calls collapse into one tool
  result in the parent's history. The parent's context stays clean.
- **Recursion and parallelism for free.** No new APIs.
- **Different models per role.** The orchestrator can use a smarter, more
  expensive model; the subagents can use a cheaper one. This is the same
  pattern as `client` vs `cheaperClient` — just two `*Client` values.
- **No new vocabulary.** "Subagent" is a word, not a type. Nothing new to learn.

A working version of this pattern lives in
[`examples/recipes/04-router-subagents`](examples/recipes/04-router-subagents).

## Worked comparison: persistent chat

*Coming with recipe 05 (`persistent-chat`).* This one earns its place because
"you own the data, not a `SessionService`" is the second-most-common ADK
objection and deserves a side-by-side.

## Worked comparison: trajectory testing

*Coming once trajectory test helpers land.* This one earns its place because
"ordinary Go testing vs. hosted eval dashboards" is a major axis of
differentiation that no amount of recipe code makes obvious on its own.

## Why only three comparisons

Every recipe in [`RECIPES.md`](RECIPES.md) demonstrates the library, but
most of them don't need an ADK comparison — they would just retell the same
point in a different domain. The three comparisons in this document each
prove a distinct philosophical claim:

1. *Subagents are tools* — router recipe (above)
2. *You own the data* — persistent-chat recipe (planned)
3. *Testing is ordinary Go* — trajectory-testing helpers (planned)

If a future feature opens up a fourth axis of meaningful difference, a
comparison gets added. Otherwise this document stays focused.

## What `gocode` will not become

To keep this comparison honest going forward, `gocode` is committed to *not*
adopting the following ADK shapes:

- a `Runner` that owns the loop
- a `SessionService` with hidden mutation
- a vector-backed `MemoryService` in core
- a graph runtime
- a deployment target
- a `SubAgent` or `Skill` type — both are expressible as tools

If a future feature would require any of these, the right answer is a
separate package or a documented recipe — not a core addition.
