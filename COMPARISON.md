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
parent's dispatch map *is* the routing mechanism. Sketch:

```go
// A subagent is just a tool whose body runs its own Loop.
researchTool, researchFn, _ := agent.NewTypedTool[input](
    "research", "Delegate a research task.", schema,
    func(ctx context.Context, in input) (string, error) {
        r, err := cheap.Loop(ctx, researchSystem,
            []agent.Message{agent.NewUserMessage(in.Task)},
            researchTools, 8)
        return r.FinalText(), err
    },
)

orchestrator := agent.Assistant{
    Client: smart, // different model from the specialists
    System: "Route research to research, drafting to write.",
    Tools:  agent.Toolset{Bindings: []agent.ToolBinding{
        {Tool: researchTool, Func: researchFn},
        {Tool: writeTool,    Func: writeFn},
    }},
    MaxIter: 6,
}
result, err := orchestrator.Step(ctx, history)
```

The orchestrator's LLM emits a `tool_use` for `research`; the dispatch map
runs the function; the function runs its own `Loop` with its own tools and
prompt; the result string returns to the orchestrator as a tool result.
Recursion is automatic. Parallel subagent calls in one turn run concurrently
because `runTools` already does that.

### What you give up vs what you get

You give up **cross-agent shared state**: each subagent sees only its task
input, and shared state must be passed through the input schema explicitly.

You get **token efficiency** (the subagent's N tool calls collapse to one
tool result in the parent's history), **different models per role**
(orchestrator on a smarter `*Client`, specialists on `client.WithModel(...)`),
**recursion and parallelism without new APIs**, and **no new vocabulary** —
"subagent" is a word, not a type.

A working end-to-end version lives in
[`examples/recipes/04-router-subagents`](examples/recipes/04-router-subagents).

## Worked comparison: persistent chat

The claim this comparison tests is *you own the data, not a `SessionService`*.
A persistent chat is the smallest real workload that exercises the boundary
between conversation state and the framework that mutates it.

### ADK shape (sketch)

ADK fronts persistence with a `SessionService`. The runner reads a session by
id, mutates it as the agent runs, and writes events back through the service.
Switching backends means switching service implementations.

```python
# Pseudocode — illustrative ADK shape
session_service = DatabaseSessionService(db_url=os.environ["DB_URL"])
# or InMemorySessionService(), or VertexAiSessionService(...)

runner = Runner(agent=chat_agent, session_service=session_service)

# First turn: the service creates the session.
session = session_service.create_session(
    app_name="support", user_id="u-123", session_id="s-abc",
    state={"tier": "pro"},
)

# Each turn: the runner reads the session, appends events, persists them.
for event in runner.run(
    user_id="u-123", session_id="s-abc",
    new_message=types.Content(role="user", parts=[types.Part(text=user_input)]),
):
    if event.is_final_response():
        print(event.content.parts[0].text)

# To inspect history, you ask the service:
session = session_service.get_session(
    app_name="support", user_id="u-123", session_id="s-abc",
)
for ev in session.events:
    ...
```

What's happening: the session lives behind the service. The runner reads,
mutates, and writes it; your code observes events as they stream out. State
deltas are applied via `EventActions.state_delta` rather than by you assigning
to a struct. Swapping `InMemorySessionService` for `DatabaseSessionService` or
`VertexAiSessionService` changes durability and scope guarantees but the
mutation point stays inside the runner.

### `gocode` shape

In `gocode`, a session is plain data, persistence is a five-method `Store`
interface, and intra-turn activity is captured by an optional `Recorder`.
The whole turn is read-modify-write:

```go
store, _ := agent.NewFileStore("./sessions")

sess, _ := agent.Load(ctx, store, sessionID) // open-or-create
client, _ := agent.New(agent.Config{
    Provider: provider, Model: agent.ModelHaiku,
    Recorder: agent.RecorderToSession(sess), // appends to sess.Events
})
assistant := agent.Assistant{Client: client, System: "...", MaxIter: 8}

sess.History = append(sess.History, agent.NewUserMessage(userInput))
result, err := assistant.Step(ctx, sess.History)
if err != nil {
    return err // sess is unchanged; retry is just calling again
}
sess.History = result.Messages
agent.Save(ctx, store, sess) // History + Events persist together
```

`Session` is `{ID, History, State, Events}`. `Load` returns a fresh session
on first use. `Save` is `Update`-or-`Create`. The store sees a deep copy
both ways, so the stored and in-memory copies never alias. Errors leave
`sess` untouched — "retry the turn" means exactly what it says.

The `Recorder` interface has one method, `Record(ctx, Event)`. Built-in
implementations (`MemoryRecorder`, `JSONLRecorder`, `MultiRecorder`,
`RecorderToSession`) cover audit, file-tail, fan-out, and round-trip. Per-tool
input/output, retry attempts with computed backoff, and per-iteration model
usage all show up as events.

To swap backends, write a `Store` (`Create`, `Get`, `Update`, `Delete`,
`List`). A Postgres-backed store is a thin wrapper over `pgx`; tests use
`NewMemoryStore()` and need no fixtures.

### What you give up vs what you get

You give up **built-in scope semantics for state** (ADK's `app:` / `user:` /
`temp:` prefixes that route to different storage scopes — in `gocode` you
keep per-session data in `Session.State` and per-user/app data in your
existing application database) and a **managed Vertex session backend**.

You get **no hidden mutation** (the session changes exactly where your code
assigns to it), **trivial backend swap** (five-method interface, no event
protocol), **failure atomicity by default** (nothing is written until `Save`,
so a failed `Step` cannot corrupt the session), **plain JSON on disk** (one
document per session, `cat` / `jq` / diff / hand-edit), and **composability
with the rest of your Go service** — `Session.State` is
`map[string]json.RawMessage`, and `Session.Events` is `[]Event`, both
accessible to your HTTP handler, background worker, and audit logger
without a service in between.

A working version of this pattern lives in
[`examples/recipes/05-persistent-chat`](examples/recipes/05-persistent-chat).

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
2. *You own the data* — persistent-chat recipe (above)
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
