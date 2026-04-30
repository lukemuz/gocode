# Recipe 01-minimal: smallest tool-using agent

Baseline recipe: how short can a useful tool-using agent be in `gocode`
using primitives alone? No streaming, no middleware, no context manager,
no `Agent` block. Just `Client` + tools + `Loop`.

The full file is 56 lines including doc comment, imports, and arg
parsing. The agent assembly itself is ~12 lines of meaningful Go.

## Run

```bash
export ANTHROPIC_API_KEY=sk-ant-...
go run ./examples/recipes/01-minimal "What time is it, and what is 17 * 23?"
```

## Why this recipe exists

Written as a baseline check on the vision's "easy things easy" promise.
Recipe `01-agent-with-tools` shows what a *production-shaped* agent
looks like (retries, streaming, middleware, context management); this
recipe shows what a *minimal* agent looks like with the same library.

If you're new to `gocode`, read this one first. Read the production
recipe second to see what each layer adds.

## Library features exercised

- `gocode.NewAnthropicClientFromEnv`
- `gocode.Client.Loop`
- `gocode.Join`, `gocode.Toolset`
- `gocode.NewUserMessage`, `gocode.TextContent`
- Built-ins: `tools/clock`, `tools/math`
