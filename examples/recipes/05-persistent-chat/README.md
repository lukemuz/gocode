# Recipe 05: persistent chat with an event log

Demonstrates `gocode`'s position that **you own the data, not a
`SessionService`**. Session state is plain Go data, persistence is a five-method
interface, and a `Recorder` captures intermediate turn activity into
`Session.Events` alongside the model-facing `History`.

## What this shows

- **Open-or-create** with `agent.Load` — first call creates, later calls load.
- **Read-modify-write** turn loop:
  ```go
  sess.History = append(sess.History, agent.NewUserMessage(input))
  result, err := assistant.Step(ctx, sess.History)
  // failed turn → sess.History unchanged → next attempt starts from the same place
  sess.History = result.Messages
  agent.Save(ctx, store, sess)
  ```
- **`FileStore`** as one Store implementation; the same code works against
  `NewMemoryStore()` for tests and would work against a Postgres or Redis
  store you write yourself in ~80 lines.
- **`agent.RecorderToSession(sess)`** — a `Recorder` that appends every
  model request/response, retry, and tool call to `sess.Events`. After
  `Save`, the full intra-turn activity log is on disk next to `History`.
- **`-dump`** flag formats the recorded events into a per-turn timeline so
  you can see exactly what happened inside each turn, not just the final
  assistant message.

## Run

```bash
export ANTHROPIC_API_KEY=sk-ant-...
go run ./examples/recipes/05-persistent-chat -id alice "what's 17 * 23?"
go run ./examples/recipes/05-persistent-chat -id alice "and minus 100?"
go run ./examples/recipes/05-persistent-chat -id alice -dump
```

The session lives at `$TMPDIR/gocode-chat/alice.json` — it's a plain JSON
document you can `cat`, `jq`, diff, or hand-edit.

## Library features exercised

- `agent.Session`, `agent.Store`, `agent.FileStore`
- `agent.Load`, `agent.Save` (open-or-create / upsert convenience)
- `agent.Recorder` interface + `agent.RecorderToSession`
- `agent.Event` and the `EventType` constants
- `agent.Assistant` driving the turn
- Built-in tools: `agent/tools/math`

## ADK comparison

See [`COMPARISON.md`](../../../COMPARISON.md#worked-comparison-persistent-chat)
for the side-by-side ADK shape — what an `InMemorySessionService` /
`DatabaseSessionService` looks like vs. this read-modify-write loop, and
how ADK's per-event session log compares to `Session.Events` here.

## Notes

- **Failure atomicity by default.** A failed `Step` returns an error and
  leaves `sess` untouched, because nothing was assigned. Retrying the turn
  is just calling `handleTurn` again. This is a property of the code you
  can see, not of a service implementation.
- **Recording is best-effort.** Recorders must be fast and non-blocking;
  errors from `JSONLRecorder.Write` are silently dropped so a flaky log
  destination cannot fail a turn. If you need stronger guarantees, wrap
  your own writer.
- **`Session.Events` does not feed back to the model.** It's an audit
  trail. The model only ever sees `Session.History`. This keeps the
  contract with the LLM unambiguous: history is what the model sees,
  events are what you saw the model do.
