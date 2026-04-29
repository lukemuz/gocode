# Recipe 02: repo-explainer

A practical tool that answers questions about a code repository. The first
recipe likely to be useful as a tool in its own right.

## What this shows

Builds on recipe 01 by adding the three things 01 deliberately omitted:

- **Persistent sessions** via `agent.FileStore` — repeated invocations with
  the same `-session` ID continue the conversation across runs
- **Summarization with a cheaper model** — the `ContextManager.Summarizer`
  is wired to `cheap.Ask` (Haiku) so older turns compress when the smart
  model's (Sonnet) context budget is exceeded
- **Multi-turn investigation** — the assistant repeatedly calls workspace
  tools, accumulates findings, and answers from grounded context

The summarizer is plain Go that calls `Ask` on a separate `Client`. There
is no hidden model invocation: deleting the `Summarizer` field reverts to
lossy drop-only trimming. That visibility is the point.

## Run

```bash
export ANTHROPIC_API_KEY=sk-ant-...

# One-shot
go run ./examples/recipes/02-repo-explainer -repo . \
    "What does this project do, and where is the agent loop implemented?"

# Persistent — reusing the same session ID continues the conversation
go run ./examples/recipes/02-repo-explainer -repo . -session demo \
    "What does this project do?"
go run ./examples/recipes/02-repo-explainer -repo . -session demo \
    "How is streaming implemented?"
```

Sessions are stored under `~/.repo-explainer/<id>.json`.

## Library features exercised

- `agent.FileStore`, `agent.Session`, `agent.Save`, `agent.ErrSessionNotFound`
- `agent.ContextManager` with a real `Summarizer`
- `agent.Assistant.StepStream`
- `agent.NewStreamBuffer` paired with `RetryConfig.OnRetry`
- `agent.MustJoin`, `Toolset.Wrap` with three middlewares
- `agent.WithLogging`, `WithTimeout`, `WithResultLimit`
- Built-ins: `agent/tools/clock`, `agent/tools/workspace` (read-only)
- Two `*Client` values for cost-tiering: Sonnet for the loop, Haiku for
  summarization

## Notes on what was awkward

Honest things this recipe surfaced that may inform future ergonomic work:

- **Rendering history for the summarizer** is ~25 lines of plumbing
  (`renderForSummary` plus `abbreviate`). A library helper —
  `agent.RenderForSummary(messages) string` — would remove that, but it's
  a minor convenience and was deliberately left out of the library to see
  whether it actually recurs.
- **Two-client construction** for cost-tiering is verbose because `Client`
  has no derive-from method. A `client.With(Model: ModelHaiku)` shortcut
  would help, but again — recurrence first, API change later.
- **Session bootstrap** (`loadOrCreateSession`) is ~20 lines for what is
  conceptually "open or create." The `Save` helper covers the write side
  cleanly; the read side has no equivalent. Watch for this.
