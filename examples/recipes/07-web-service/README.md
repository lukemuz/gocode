# Recipe 07: web service template

A minimal HTTP server that fronts a `gocode` `Agent`. Use it as a starting
point: clone the directory, replace the system prompt and toolset with your
own, ship the binary or the Dockerfile.

The whole server is one ~140-line file. The point is that the gocode
pattern stays visible — load session, append user message, `Step`, save —
not that the template is production-finished.

## API

```
POST /chat
content-type: application/json
{ "session_id": "alice", "message": "what is 17 * 23?" }

→ 200 { "session_id": "alice", "reply": "17 * 23 = 391." }

GET /healthz → 200 ok
```

## Run locally

```bash
export ANTHROPIC_API_KEY=sk-ant-...
go run ./examples/recipes/07-web-service

# in another shell
curl -s localhost:8080/chat \
    -H 'content-type: application/json' \
    -d '{"session_id":"alice","message":"what is 17 * 23?"}' | jq
```

Sessions land in `$TMPDIR/gocode-web-sessions/*.json` by default.

## Configuration

| Env var             | Default                       | Notes                       |
|---------------------|-------------------------------|-----------------------------|
| `ANTHROPIC_API_KEY` | required                      | Anthropic API key.          |
| `PORT`              | `8080`                        | Listen port.                |
| `SESSIONS_DIR`      | `$TMPDIR/gocode-web-sessions` | Directory for `FileStore`.  |

## Make it your own

Three things you'll typically change in `main.go`:

1. **`systemPrompt`** — the personality / instructions for your agent.
2. **`gocode.MustJoin(clock.New().Toolset(), math.New().Toolset())`** —
   swap in your own `Toolset`. See `tools/workspace` for sandboxed
   filesystem tools, `mcp` for MCP servers, or build your own with
   `gocode.NewTypedTool` (or `gocode.Tools(...)` / `gocode.Bind(...)`).
3. **`gocode.ModelHaiku`** — bump to `gocode.ModelSonnet` or `ModelOpus`
   for harder tasks; Haiku is the cheap default.

The request shape (`chatRequest` / `chatResponse`) is local to this file —
adjust freely if your client wants a different schema.

## Deploy

### Railway / Render / Fly / Cloud Run (Dockerfile)

The included `Dockerfile` produces a small distroless image. Build context
is the **repo root**:

```bash
docker build -f examples/recipes/07-web-service/Dockerfile -t gocode-web .
docker run --rm -p 8080:8080 -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY gocode-web
```

For persistent sessions on platforms with attached volumes, mount the volume
and set `SESSIONS_DIR=/data`. On stateless platforms, sessions are lost on
restart — fine for short conversations, bad for long ones; back the agent
with a custom `gocode.Store` (Postgres, Redis, DynamoDB) when that matters.

### Bare binary

```bash
CGO_ENABLED=0 go build -o server ./examples/recipes/07-web-service
ANTHROPIC_API_KEY=... ./server
```

## Library features exercised

- `gocode.Agent`, `Agent.Step`
- `stores.NewFileStore`, `gocode.Load`, `gocode.Save`
- `gocode.ContextManager` (so long conversations don't blow the window)
- `gocode.MustJoin` for static toolset composition
- Built-in tools: `tools/clock`, `tools/math`

## Graduating to production

Things this template intentionally leaves out so the gocode pattern stays
front-and-center. Add them when you're ready to ship for real:

- **Streaming.** `Agent.StepStream` delivers token deltas; wrap it with
  Server-Sent Events or websockets if your client wants typewriter output.
- **Per-session locking.** Two concurrent requests for the same
  `session_id` race on `Save`. A `sync.Mutex` keyed by session id fixes it
  in-process; for multi-replica deploys, use a `Store` backed by a database
  with row-level locking, or a session-affinity load balancer.
- **Graceful shutdown.** Wrap `http.Server` with `signal.NotifyContext` and
  call `Shutdown` on SIGTERM so in-flight turns finish cleanly.
- **Auth.** This template trusts anyone who can reach the port. Put it
  behind a reverse proxy or add middleware before exposing it publicly.
- **Rate limiting.** A single client can run up your Anthropic bill. Add a
  token bucket or rely on your platform's rate-limiting.
- **Request hardening.** `http.MaxBytesReader` to cap request size,
  `json.Decoder.DisallowUnknownFields` to reject typos, per-request
  timeouts via `context.WithTimeout`.
