# `gocode` vs ADK

A code-first comparison between `gocode` and Google's Agent Development Kit (ADK). Where ADK is the better fit, this document says so.

## The short version

ADK is a full agent application model: agents, runners, sessions, state, events, callbacks, artifact services, memory services, and managed deployment. It is comprehensive and opinionated.

`gocode` is a small set of primitives — `Client`, `Provider`, `Message`, `Tool`, `Loop`, `Assistant`, `Toolset`, `ContextManager`, `Session` — composed with ordinary Go.

| Concern | ADK | `gocode` |
|---|---|---|
| Time to first model call | medium | low |
| Multi-tool agent | declarative | assemble a `Toolset` |
| Streaming + retries | built-in | `StreamBuffer` + `RetryConfig.OnRetry` |
| Persistent sessions | `SessionService` | `Store` interface, `FileStore` |
| Cross-session memory + vector search | built-in | not in core |
| Trajectory eval | built-in | offline helpers planned |
| Live audio/video | yes (Gemini Live) | no |
| Managed deployment | yes (Vertex Agent Engine) | no |
| Application-model lock-in | high | low |

If you need live audio, hosted deployment, or vector-backed memory out of the box, pick ADK. If your service is in Go and you want agent code that reads like Go, pick `gocode`.

## Honest strengths and limits

`gocode` is good at:

- **Legibility.** History is `[]Message`; tools are functions; loops are visible. Reading the code tells you what runs.
- **Explicit ownership.** Persistence, context trimming, retries, and the loop are caller-controlled. No service mutates state behind your back.
- **Composition with existing Go.** No Python runtime adjacent to your service. The same client, store, and toolset slot into HTTP handlers, workers, and tests.
- **Easy testing.** The `Provider` interface is the main seam. Tests run without network calls and assert contracts, not prose.

`gocode` does not try to be:

- a managed deployment story
- a vector-backed memory layer
- a live audio/video runtime
- a hosted eval dashboard
- a cross-language framework

For any of those, ADK (or another framework) is the better fit.

## When to pick which

**Pick ADK when** you want Vertex Agent Engine deployment, bidirectional live audio/video, a built-in vector memory service, declarative agent configuration with strong defaults, or eval dashboards for non-engineer stakeholders.

**Pick `gocode` when** your service is in Go, you want to debug agents by reading code, and you want explicit ownership of history, persistence, and the loop.

## Two worked comparisons

Two patterns each prove a distinct philosophical claim. Other recipes live under `examples/recipes/`; they demonstrate the library but don't need an ADK comparison to make their point.

### 1. Router with specialists — *subagents are tools*

One orchestrator decides which specialist handles a request, the specialist works, the orchestrator returns the result.

**ADK shape (sketch).** ADK models this with sub-agents and an `LlmAgent` that can transfer control. The orchestrator's LLM emits a transfer-to-agent action; the runner routes the conversation to the named sub-agent; control returns to the orchestrator. Events for the whole trajectory live in one session.

```python
# Pseudocode
researcher  = LlmAgent(name="researcher", tools=[search, read_url], ...)
writer      = LlmAgent(name="writer", ...)
orchestrator = LlmAgent(name="orchestrator", sub_agents=[researcher, writer], ...)

runner = Runner(agent=orchestrator, session_service=...)
events = runner.run(session_id=session.id, new_message=...)
```

**`gocode` shape.** A subagent is a `ToolFunc` that happens to call `Loop`. The parent's dispatch map *is* the routing mechanism.

```go
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
    Client: smart, // smarter model than the specialists
    System: "Route research to research, drafting to write.",
    Tools:  agent.Toolset{Bindings: []agent.ToolBinding{
        {Tool: researchTool, Func: researchFn},
        {Tool: writeTool,    Func: writeFn},
    }},
    MaxIter: 6,
}
result, err := orchestrator.Step(ctx, history)
```

You give up **cross-agent shared state** (each subagent sees only its task input; shared state passes through the input schema). You get **token efficiency** (the subagent's N tool calls collapse to one tool result), **different models per role**, **recursion and parallelism without new APIs**, and **no new vocabulary** — "subagent" is a word, not a type.

Working version: [`examples/recipes/04-router-subagents`](examples/recipes/04-router-subagents).

### 2. Persistent chat — *you own the data*

The smallest workload that exercises the boundary between conversation state and the framework that mutates it.

**ADK shape (sketch).** A `SessionService` fronts persistence. The runner reads a session by id, mutates it as the agent runs, and writes events back. State deltas apply via `EventActions.state_delta` rather than direct assignment. Swapping `InMemorySessionService` for `DatabaseSessionService` or `VertexAiSessionService` changes durability, but the mutation point stays inside the runner.

```python
# Pseudocode
session_service = DatabaseSessionService(db_url=os.environ["DB_URL"])
runner = Runner(agent=chat_agent, session_service=session_service)

for event in runner.run(user_id="u-123", session_id="s-abc",
                        new_message=types.Content(...)):
    if event.is_final_response():
        print(event.content.parts[0].text)
```

**`gocode` shape.** A session is plain data; persistence is a five-method `Store`; the whole turn is read-modify-write.

```go
store, _ := agent.NewFileStore("./sessions")

sess, err := store.Get(ctx, sessionID)
if errors.Is(err, agent.ErrSessionNotFound) {
    sess = &agent.Session{ID: sessionID}
}

sess.History = append(sess.History, agent.NewUserMessage(userInput))
result, err := assistant.Step(ctx, sess.History)
if err != nil {
    return err // sess unchanged; retry is just calling again
}
sess.History = result.Messages

if len(sess.History) == 1 {
    err = store.Create(ctx, sess)
} else {
    err = store.Update(ctx, sess)
}
```

You give up **built-in scope semantics for state** (ADK's `app:` / `user:` / `temp:` prefixes — in `gocode` you keep per-session data in `Session.State` and per-user data in your existing database) and a **managed Vertex backend**.

You get **no hidden mutation**, **trivial backend swap** (five-method interface), **failure atomicity** (nothing persists until you save, so a failed `Step` cannot corrupt the session), and **plain JSON on disk** that `cat`, `jq`, and diffs can read.

Working version: [`examples/recipes/05-persistent-chat`](examples/recipes/05-persistent-chat).

## What `gocode` will not become

To keep this comparison honest going forward, the core is committed to *not* adopting:

- a `Runner` that owns the loop
- a `SessionService` with hidden mutation
- a vector-backed `MemoryService` in core
- a graph runtime
- a deployment target
- a `SubAgent` or `Skill` type — both are expressible as tools

If a future feature would require any of these, the right answer is a separate package or a documented recipe — not a core addition.
