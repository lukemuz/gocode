# gocode CLI

A fast, economical CLI coding agent built on the gocode toolkit. Inspired by Claude Code; written in Go; opinionated about cost and parallelism out of the box.

## Install / run

You'll need an Anthropic API key in your environment for any of these:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

### Option A — install once, run anywhere (recommended)

```bash
go install github.com/lukemuz/gocode/cmd/gocode@latest
```

This drops a `gocode` binary in `$(go env GOBIN)` (or `$(go env GOPATH)/bin` if `GOBIN` is unset). Make sure that directory is on your `PATH`:

```bash
echo $PATH | tr ':' '\n' | grep -q "$(go env GOPATH)/bin" || \
  echo 'add $(go env GOPATH)/bin to your PATH'
```

Then from any directory you want the agent to work in:

```bash
cd ~/your-project
gocode -log auto
```

To re-install after pulling new commits, run `go install ...` again.

### Option B — build a local binary in this repo

From the gocode checkout:

```bash
go build -o bin/gocode ./cmd/gocode
./bin/gocode -dir ~/your-project -log auto
```

Or symlink it into your `PATH`:

```bash
sudo ln -s "$(pwd)/bin/gocode" /usr/local/bin/gocode
```

### Option C — `go run` (development only)

Useful while editing gocode itself. From the repo root:

```bash
go run ./cmd/gocode -dir ~/your-project
```

Slow start every time (re-compiles), but no binary to manage.

### First-run sanity check

```bash
gocode -dir . -log auto
```

You should see:
```
gocode  model=claude-sonnet-4-6  bash=restricted  subagents=on  dir=/abs/path
        explore=claude-haiku-4-5-20251001  plan=claude-opus-4-7
        log=/home/you/.config/gocode/sessions/2026-04-30T14-22-13.jsonl
type a request, or /help for commands. ctrl-c to interrupt, ctrl-d to exit.
> 
```

If you get `anthropic provider: ANTHROPIC_API_KEY environment variable is not set`, you missed the `export` step.

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
| `list_directory`, `Glob`, `Grep`, `read_file`, `file_info` | Read-only filesystem inspection (workspace package) | no |
| `str_replace_based_edit_tool` | Anthropic's trained text editor: view / create / str_replace / insert | yes |
| `bash` | Anthropic's trained bash, sandboxed by `-bash` mode | yes (in standard/unrestricted modes) |
| `todo_write`, `todo_read` | Planning checklist; replace-whole-list semantics | no |
| `batch` | Run several read-only tool calls concurrently in one turn | no |
| `web_search` | Anthropic-hosted web search (server-executed) | no |
| `web_fetch` | Anthropic-hosted URL fetch (server-executed) | no |
| `now` | Current time | no |
| `explore(task)` | Delegate inspection to a Haiku-backed subagent | no |
| `plan(task)` | Delegate hard reasoning to an Opus-backed subagent | no |

`Glob` and `Grep` use the same names Claude Code uses, so the model recognises them immediately. `web_search` and `web_fetch` are server-executed by Anthropic — no handler runs locally; results stream back inline. Disable them with `-no-web` if you want fully offline runs.

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
| `-no-web` | false | Disable the Anthropic-hosted `web_search` and `web_fetch` tools |
| `-bash` | `restricted` | `restricted` \| `standard` \| `unrestricted` |
| `-yes` | false | Auto-approve every confirmation prompt |
| `-max-iter` | 30 | Max model calls per user turn |
| `-log` | (off) | JSONL session log path. Use `-log auto` to write under `~/.config/gocode/sessions/<timestamp>.jsonl`, or pass an explicit path. |

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
| `/log` | Print the active JSONL log path (if any) |

## Session logging

`-log auto` (or `-log <path>`) writes a JSON Lines trace of the entire session to disk. Every model request, response, retry, tool call (start and end with input + output), and turn boundary is recorded — for the main agent and both subagents. The file is append-only, safe to read while the session is running.

It's the right thing to enable when something feels off and we want to look at what actually happened together. `jq -c '.type' session.jsonl | sort | uniq -c` is a good first pass; pipe specific events to `jq -c 'select(.type == "tool_call_end") | {tool: .tool_name, bytes: (.tool_output | length), error: .is_error}'` for tool-level inspection.

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
