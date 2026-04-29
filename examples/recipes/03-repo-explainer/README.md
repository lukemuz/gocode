# Recipe 02: repo-explainer

A practical tool that answers questions about a code repository. The first
recipe likely to be useful as a tool in its own right.

## What this shows

Builds on recipe 01 by adding the three things 01 deliberately omitted:

- **Persistent sessions** via `gocode.FileStore` — repeated invocations with
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
go run ./examples/recipes/03-repo-explainer -repo . \
    "What does this project do, and where is the agent loop implemented?"

# Persistent — reusing the same session ID continues the conversation
go run ./examples/recipes/03-repo-explainer -repo . -session demo \
    "What does this project do?"
go run ./examples/recipes/03-repo-explainer -repo . -session demo \
    "How is streaming implemented?"
```

Sessions are stored under `~/.repo-explainer/<id>.json`.

## Library features exercised

- `gocode.FileStore`, `gocode.Session`, `gocode.Save`, `gocode.Load`,
  `gocode.ErrSessionNotFound`
- `gocode.ContextManager` with a real `Summarizer`
- `gocode.RenderForSummary` to flatten message history for the summarizer
- `Client.WithModel` for cheap-summarizer cost-tiering
- `gocode.Agent.StepStream`
- `gocode.NewStreamBuffer` paired with `RetryConfig.OnRetry`
- `gocode.MustJoin`, `Toolset.Wrap` with three middlewares
- `gocode.WithLogging`, `WithTimeout`, `WithResultLimit`
- Built-ins: `agent/tools/clock`, `agent/tools/workspace` (read-only)
