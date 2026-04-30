# gocode CLI

A fast, economical CLI coding agent built on the gocode toolkit. Inspired by Claude Code; written in Go; opinionated about cost and parallelism out of the box.

## Install / run

```bash
export ANTHROPIC_API_KEY=sk-ant-...
go run ./cmd/gocode -dir .
```

Or build a binary:

```bash
go build -o bin/gocode ./cmd/gocode
./bin/gocode -dir .
```

## What's running

```
main agent (Sonnet by default)
  ├── direct tools  workspace + bash + str_replace_based_edit_tool
  │                 + todo + clock + batch
  ├── explore       subagent on Haiku — read-only fs + restricted bash + batch
  └── plan          subagent on Opus — read-only fs only, no shell, no edits
```

Why three agents? Cost tiering and context isolation. The main Sonnet decides what work to do; cheap inspection happens on Haiku and never enters the main context (only the subagent's final summary returns); hard reasoning escalates to Opus on demand.

## Tools available to the main agent

| Tool | What it does | Confirmation |
|---|---|---|
| `list_directory`, `find_files`, `search_text`, `read_file`, `file_info` | Read-only filesystem inspection (workspace package) | no |
| `str_replace_based_edit_tool` | Anthropic's trained text editor: view / create / str_replace / insert | yes |
| `bash` | Anthropic's trained bash, sandboxed by `-bash` mode | yes (in standard/unrestricted modes) |
| `todo_write`, `todo_read` | Planning checklist; replace-whole-list semantics | no |
| `batch` | Run several read-only tool calls concurrently in one turn | no |
| `now` | Current time | no |
| `explore(task)` | Delegate inspection to a Haiku-backed subagent | no |
| `plan(task)` | Delegate hard reasoning to an Opus-backed subagent | no |

The `explore` and `plan` subagents are themselves agents with their own toolsets — the main agent can ask them anything within their scope.

## Context economy

Two things make this fast and cheap:

1. **Stable cacheable prefix.** System prompt + project memory + tool definitions are marked as cache breakpoints. After the first turn of a session, those tokens cost ~10% of normal. Cache hit rate shows up in `/tokens`.

2. **Subagent context isolation.** When `explore` runs a 30-file investigation, all that searching and reading happens in the subagent's loop and dies with it. Only the textual summary returns to the main agent.

When context fills up, run `/compact` (see below) — Haiku summarises older turns so you can keep going without starting over.

## Project memory

On startup gocode loads, in order, and concatenates:

1. `<workspace>/AGENTS.md`        — vendor-neutral convention
2. `<workspace>/CLAUDE.md`        — Claude Code's flavour, picked up for compatibility
3. `~/.config/gocode/AGENTS.md`   — your personal gocode preferences
4. `~/.claude/CLAUDE.md`          — your existing Claude Code memory, reused

Anything found is appended to the system prompt under "## Project memory" and becomes part of the cached prefix. View what's loaded with `/memory`.

A good `AGENTS.md` is short and concrete: project conventions, how to run tests, the language/style the project prefers. The shorter and more stable it is, the better the cache works.

## Flags

| Flag | Default | Description |
|---|---|---|
| `-dir` | `.` | Working directory the agent is sandboxed to |
| `-model` | `claude-sonnet-4-6` | Main-agent model |
| `-explore-model` | `claude-haiku-4-5-20251001` | Model for the explore subagent |
| `-plan-model` | `claude-opus-4-7` | Model for the plan subagent |
| `-no-subagents` | false | Disable the explore and plan tools |
| `-bash` | `restricted` | `restricted` \| `standard` \| `unrestricted` |
| `-yes` | false | Auto-approve every confirmation prompt |
| `-max-iter` | 30 | Max model calls per user turn |

### Bash safety modes

- **`restricted`** (default, no confirmation): only commands whose first token is on a curated allowlist (`ls`, `cat`, `grep`, `rg`, `find`, `git status/diff/log`, `go`, `wc`, `head`, `tail`, etc.). Shell metacharacters (`;`, `&`, `|`, `\``, `$`, `<`, `>`) are rejected — the model can't smuggle in a second command.
- **`standard`** (confirmation required): open shell with a deny-list for obviously dangerous patterns (`rm -rf /`, `sudo`, `curl|sh`, `dd of=/dev/`, fork bombs, writes to `/etc`).
- **`unrestricted`** (confirmation required): only the timeout (30s) and output cap (64 KiB) apply.

You can pair any mode with `-yes` to auto-approve, e.g. for headless runs.

## Slash commands

Both `/cmd` and `:cmd` are accepted.

| Command | What it does |
|---|---|
| `/help` | Show all commands |
| `/exit`, `/quit` | Leave the REPL |
| `/reset`, `/clear` | Drop conversation history |
| `/compact [instructions]` | Summarise older turns; keeps the last 4 user turns verbatim. Optional instructions steer the summary (e.g. `/compact focus on the auth refactor`). |
| `/tokens` | Print accumulated input/output/cache tokens and cache hit rate |
| `/memory` | Print the loaded project memory |
| `/tools` | List available tools (with `[confirm]` flag) |
| `/model <id>` | Switch the main-agent model mid-session (subagent models unchanged) |

## Recipes

**Quick code question:**
```
> what's the difference between Loop and StepStream?
```
Sonnet answers directly using its read-only tools.

**Investigation:**
```
> find every place we read environment variables and summarise the patterns
```
Sonnet should delegate this to `explore`. The Haiku subagent fans out greps via `batch`, reads candidate files, and returns a summary. Cheap.

**Refactor:**
```
> rename ToolFunc to ToolHandler everywhere, update tests, leave a deprecation alias
```
Sonnet plans via `todo_write`, uses `str_replace_based_edit_tool` for edits, runs `go test ./...` via `bash` (you'll be prompted to approve).

**Architecture decision:**
```
> we want to add a per-session sqlite cache for file shas. design it.
```
Sonnet should call `plan` to escalate to Opus for the design, then summarise back.

## Things to watch the first time you run it

- **Did the main agent delegate to `explore`?** If it does inspection inline instead of calling `explore`, the system prompt may need tuning. Watch the stderr `[tool ...]` lines.
- **Is the cache working?** After 2-3 turns, `/tokens` should show a non-trivial cache hit rate. If it's zero, something is breaking the prefix.
- **Confirmation prompts.** Edits and shell (in non-restricted modes) prompt on stderr. `-yes` skips this if you trust the run.

## What's not here yet

- SHA-based file-content dedup (re-reads cost full tokens today)
- Haiku-tier tool-result compression for oversized outputs
- Auto-compaction at a context-window threshold
- Persistent codebase index across sessions
- Speculative inspection while the model is drafting
- OAuth / Claude subscription auth (API key only)

These are next on the list — see the project ROADMAP if it grows.
