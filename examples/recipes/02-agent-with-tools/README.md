# Recipe 02: agent with tools

The entry-point recipe. Smallest possible "I'm building a real thing"
example: an `Agent` with a curated toolset, middleware, context
management, and streamed output.

## What this shows

- `luft.Agent` as the assembly point — a thin block that ties together
  client, system prompt, toolset, context manager, and iteration cap
- Built-in tools composed with `luft.Join`: clock + math + read-only
  workspace
- Three middlewares applied in one `Wrap` call: timeout, result limit,
  structured logging via `*slog.Logger`
- `ContextManager` configured (no-op for short inputs, but ready for long
  conversations)
- Streaming with `StreamBuffer` wired to `RetryConfig.OnRetry` so partial
  output is cleared cleanly when a retry happens mid-stream

What it deliberately omits: subagents (recipe 04), persistence (recipe 05),
parallel pipelines (recipe 06). Each later recipe layers one
dimension on top of this base.

## Run

```bash
export ANTHROPIC_API_KEY=sk-ant-...
go run ./examples/recipes/02-agent-with-tools "What time is it, and what is 17 * 23?"
go run ./examples/recipes/02-agent-with-tools -dir . "How many .go files are in this directory tree?"
```

## Library features exercised

- `luft.New`, `luft.Config`, `luft.RetryConfig`
- `luft.Agent`, `Agent.StepStream`
- `luft.Join`, `Toolset.Wrap`
- `luft.WithTimeout`, `luft.WithResultLimit`, `luft.WithLogging`
- `luft.ContextManager`
- `luft.NewStreamBuffer` (retry-aware streaming)
- Built-ins: `tools/clock`, `tools/math`, `tools/workspace`
