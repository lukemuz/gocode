# Changelog

All notable changes to this project will be documented in this file.

The format is loosely based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## v0.1.1 — 2026-05-08

### Added

- `providers/openrouter` now supports OpenRouter's hosted server-side tools.
  `openrouter.WebSearch(opts)` constructs a `gocode.ProviderTool` for the
  `openrouter:web_search` hosted tool; the model decides when to invoke and
  OpenRouter executes the search server-side.
- `openrouter.ProviderTag` constant ("openrouter") for tagging hosted tools;
  the OpenRouter provider now rejects `ProviderTool` entries tagged for
  other providers at request build time.
- Citation surfacing on OpenAI-compatible `Call`: when a backend (e.g.
  OpenRouter web search) attaches `annotations` to the assistant message,
  each entry is emitted as an opaque `gocode.ContentBlock` (Type set from
  the annotation's `type`, full JSON in `Raw`). Streaming citations are
  not yet surfaced.

### Changed

- `openai.CompatibleCall` and `openai.CompatibleStream` gained a final
  `allowProviderTools bool` parameter. Stock OpenAI passes `false` (Chat
  Completions does not host tools); OpenRouter passes `true`. External
  callers of these helpers will need to add the new argument.
- `toOpenAITools` now returns `[]json.RawMessage` so opaque hosted-tool
  entries can be spliced alongside function tools.

## v0.1.0 — 2026-05-08

First tagged release. The library and CLI ship together under a single
module version.

### Library

- `Ask` / `AskStream` for one-shot model calls with usage reporting.
- `Loop` / `LoopStream` for tool-using loops over plain `[]Message` history.
- `Extract[T]` for typed structured output, with optional intermediate tools
  and a `Validate` hook (built on `ToolMetadata.Terminal`).
- `Agent` block: a thin composition of client, system prompt, toolset,
  context manager, iteration limit, and hooks. `Step` and `StepStream`.
- `Parallel` for fanning out independent `Ask`/`Loop` calls.
- `Toolset`, `ToolBinding`, `Tools`, `Bind`, `Join`, `MustJoin`. Typed tools
  via `NewTypedTool`. Schema helpers (`Object`, `String`, `Number`,
  `Array`, `Required`, …).
- Middleware: `WithTimeout`, `WithResultLimit`, `WithLogging`,
  `WithPanicRecovery`, `WithConfirmation`.
- `ContextManager` for explicit history trimming with tool-use/result
  integrity preservation; optional summarization.
- Prompt-cache markers (`Ephemeral`, `EphemeralExtended`) honored by
  Anthropic and OpenRouter; transparently dropped by OpenAI providers.
- `Provider` interface plus implementations:
  - `providers/anthropic` — Messages API, server-executed and
    provider-defined tools (web search, code execution, bash, text editor).
  - `providers/openai` — Chat Completions and Responses, with hosted
    tools on Responses (web search, file search, code interpreter,
    image generation).
  - `providers/openrouter` — OpenAI-compatible with cache-marker
    translation for Anthropic backends.
- Sessions: plain `Session` data, `MemoryStore` and `FileStore`
  implementations of the five-method `Store` interface.
- Streaming with `StreamBuffer` for retry-aware partial-output handling.
- Typed errors: `APIError`, `ToolError`, `LoopError`,
  `RetryExhaustedError`, `ErrMissingTool`, `ErrMaxIter`,
  `ErrSessionExists`, `ErrSessionNotFound`.
- Configurable retry with exponential backoff and `OnRetry` callback.
- JSONL session recorder for offline replay and debugging.

### Built-in tools

- `tools/clock` — current UTC time.
- `tools/math` — safe calculator.
- `tools/workspace` — sandboxed `list_directory`, `Glob`, `Grep`,
  `read_file`, `file_info`, and (in the read/write build) exact-string
  `edit_file`.
- `tools/bash` — shell tool with `restricted` / `standard` /
  `unrestricted` safety modes, working directory sandboxing.
- `tools/editor` — provider-trained `str_replace_based_edit_tool`
  (view / create / str_replace / insert).
- `tools/todo` — `todo_write` / `todo_read` for in-conversation
  planning checklists.
- `tools/batch` — fan-out tool that runs 2+ independent read-only
  tool calls concurrently in a single turn.
- `tools/web` — native `web_fetch` with HTML-to-text and pagination.
- `tools/subagent` — wrap any `Agent` as a tool callable by another
  agent; iteration history stays out of the parent's context.

### MCP

- `mcp` package adapts Model Context Protocol servers into ordinary
  toolsets via `mcp.Connect` / `Server.Toolset`.

### CLI (`cmd/gocode`)

- Multi-agent CLI coding assistant on OpenRouter:
  - main agent (`x-ai/grok-4.3` by default) with read-only workspace
    tools, bash, editor, todo, batch, and web_fetch.
  - `explore` subagent (`openai/gpt-oss-120b`) for cheap, parallelisable
    repo research and bounded Q&A.
  - `plan` subagent (`x-ai/grok-4.3`) for design, architecture, and
    hard-debugging questions.
- Flags: `-dir`, `-model`, `-explore-model`, `-plan-model`,
  `-no-subagents`, `-no-fetch`, `-bash`, `-yes`, `-max-iter`, `-log`,
  `-version`.
- Env-var fallbacks: `GOCODE_MODEL`, `GOCODE_EXPLORE_MODEL`,
  `GOCODE_PLAN_MODEL`, `GOCODE_SUMMARIZE_MODEL`.
- REPL with `:exit`, `:reset`, `:tokens`, `:help`, plus `/compact`
  for summarising history mid-session.
- Unified-diff preview in edit confirmation prompts.
- Optional JSONL session log (`-log auto` writes under
  `~/.config/gocode/sessions/`).
- Project memory: `AGENTS.md` and `CLAUDE.md` from the workspace and
  user-level (`~/.config/gocode/AGENTS.md`, `~/.claude/CLAUDE.md`) are
  appended to the system prompt.
