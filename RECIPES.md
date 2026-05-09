# Recipes

Runnable patterns under `examples/recipes/`. Each recipe is ordinary Go that ties primitives together to solve one concrete problem: history is `[]Message`, tools are functions, loops are visible, persistence is explicit.

| # | Recipe | What it demonstrates |
|---|---|---|
| 01 | [`01-minimal`](examples/recipes/01-minimal) | Smallest useful program: one `Ask` call |
| 02 | [`02-agent-with-tools`](examples/recipes/02-agent-with-tools) | `Agent` with a curated toolset, middleware, and context management |
| 03 | [`03-repo-explainer`](examples/recipes/03-repo-explainer) | Sandboxed workspace tools, streaming, file-backed sessions |
| 04 | [`04-router-subagents`](examples/recipes/04-router-subagents) | Parent delegates to specialist subagents — *subagents are tools* |
| 05 | [`05-persistent-chat`](examples/recipes/05-persistent-chat) | Long-running conversation with `FileStore` and context trimming |
| 06 | [`06-parallel-pipeline`](examples/recipes/06-parallel-pipeline) | Parallel-then-sequential pipeline with `Parallel` and `Ask` |

Each recipe directory has a `main.go` plus a `README.md` describing what it does, how to run it, and which `luft` features it exercises.
