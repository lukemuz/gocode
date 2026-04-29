# research — deep research agent

A small CLI that answers a research question by:

1. **Planning** — an LLM decomposes the question into focused sub-questions.
2. **Investigating** — workers run in parallel, each searching the web via the
   [Brave Search MCP server](https://github.com/modelcontextprotocol/servers/tree/main/src/brave-search)
   and emitting a summary with citations.
3. **Synthesizing** — a final LLM call stitches the notes into a cited report.

There is no graph runtime. The pipeline is a planner function, `agent.Parallel`,
and a synthesizer function. Cost-tier per phase by passing different models.

## Setup

```bash
export ANTHROPIC_API_KEY=sk-ant-...
export BRAVE_API_KEY=BSA...     # https://api.search.brave.com
```

The Brave Search MCP server is launched on demand via `npx`, so Node.js must
be on PATH.

## Run

```bash
go run ./cmd/research "What are the practical differences between QUIC and HTTP/2?"
```

Common flags:

| flag             | default        | purpose                                      |
|------------------|----------------|----------------------------------------------|
| `-max-subtasks`  | 5              | cap planner output                           |
| `-concurrency`   | 3              | semaphore on in-flight workers               |
| `-worker-iter`   | 12             | tool-use iteration cap per worker            |
| `-planner-model` | sonnet         | model for planner                            |
| `-worker-model`  | haiku          | model for workers                            |
| `-synth-model`   | sonnet         | model for synthesizer                        |
| `-out FILE`      | stdout         | write report body to FILE                    |
| `-json`          | false          | emit full `Report` JSON instead of just body |
| `-quiet`         | false          | suppress progress logs                       |

## Library use

```go
import "github.com/lukemuz/gocode/research"

cfg := research.Config{
    Planner:        plannerClient,
    Worker:         workerClient,
    Synthesizer:    synthClient,
    SearchTools:    braveToolset,
    MaxSubtasks:    5,
    MaxConcurrency: 3,
}
report, err := research.Run(ctx, cfg, "your question")
```

`research.Decompose`, `research.Investigate`, and `research.Synthesize` are all
exported so you can run any phase independently.
