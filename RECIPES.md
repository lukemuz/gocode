# Recipes

This document names the eight canonical recipes that demonstrate `gocode`
across the most common real-world agent scenarios. Each recipe is a runnable
program under `examples/recipes/` that ties together the library's primitives
to solve one concrete problem.

The goal is legibility, not feature coverage. Each recipe should:

- be 150–300 lines of ordinary Go
- map 1:1 to a scenario a developer would otherwise reach for ADK or LangGraph for
- reference a corresponding section in [`COMPARISON.md`](COMPARISON.md) where
  the equivalent ADK shape is described
- show the data flow plainly: history is `[]Message`, tools are functions,
  loops are visible, persistence is explicit

## The eight scenarios

| # | Recipe | What it demonstrates | Status |
|---|---|---|---|
| 01 | `01-assistant-with-tools` | Single assistant with a curated toolset, middleware, and context management | planned |
| 02 | `02-repo-explainer` | Sandboxed workspace tools, streaming, summarization, file-backed sessions | planned |
| 03 | `03-workflow-agent` | Read-then-act batch workflow over a list of inputs | planned |
| 04 | `04-router-subagents` | Parent agent delegates to specialist subagents — *subagents are tools* | in progress |
| 05 | `05-persistent-chat` | Long-running conversation with `FileStore`, context trimming, and a summarizer | planned |
| 06 | `06-http-sse` | `net/http` handler streaming model output over Server-Sent Events | planned |
| 07 | `07-cli-agent` | Interactive terminal agent in the Claude Code / Aider shape | planned |
| 08 | `08-batch` | Fan-out over many inputs with `Parallel`, with per-input retries | planned |

## Recipe contract

Every recipe directory contains:

- `main.go` — the runnable program
- `README.md` — what it does, how to run it, and which `gocode` features it
  exercises
- a one-line entry in this file's status table

When a recipe lands, update the status column and add a link in
[`COMPARISON.md`](COMPARISON.md) to the corresponding ADK section.

## Ordering rationale

Recipe 04 (`router-subagents`) ships first because it's the most
philosophically load-bearing: it tests whether the "subagents are tools"
position is true in code. If that pattern reads cleanly, the rest of the
recipes are ergonomic exercises. If it doesn't, the library has a real gap to
address before the others are worth building.

The remaining recipes will be ordered by leverage — comparison-document
weight first, then the patterns most commonly searched for in the agent
ecosystem (RAG-shape, persistent chat, HTTP/SSE).
