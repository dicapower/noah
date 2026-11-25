# noah

Persistent memory for AI coding agents. Go, SQLite (FTS5), MCP over stdio. Apache-2.0, DICA POWER OÜ.

## Commands

- Check everything: `go vet ./... && go test ./...`
- Search-quality test (fixed seed, 2,000 memories): `go test ./internal/search -run TestRecall -v`; larger case: `NOAH_RECALL_N=10000`. Print numbers without asserting: `NOAH_RECALL_REPORT=1`.
- Scale benchmark: `NOAH_SCALE=1000,10000 go test ./internal/search -run TestScale -v`
- Cross-tool benchmark against Engram: `go build -o $BENCH_DIR/noah ./cmd/noah`, put an `engram` binary next to it, then `BENCH_DIR=/tmp/noah-bench python3 bench/bench.py noah 10000`.
- Windows build check: `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./cmd/noah`

## Layout

`cmd/noah` CLI and hooks · `internal/store` SQLite (one writer, read pool, migrations) · `internal/search` FTS candidates + rerank · `internal/query` question → FTS query · `internal/memory` types, rendering, suspicious-content detection · `internal/mcpserver` the six tools · `internal/hook` Claude Code hooks · `internal/importer` Engram import · `internal/secrets` redaction.

## Rules that must not break

- **Schema:** append a migration to `internal/store/migrate.go`; never edit an applied one. Bump nothing else: `PRAGMA user_version` is the length of the list.
- **Tool surface:** six MCP tools, and `TestSchemaTokenCostStaysSmall` caps the cost of their definitions. Adding a tool or a long description costs every user on every request; prefer a parameter on an existing tool.
- **Search quality:** `TestRecall` is a contract. Raise `minPartial` when ranking improves; never lower it to make a change pass.
- **Context is injected unprompted, so it is untrusted input:** memories reach the agent only through `memory.RenderContext` (labelled, closing tag neutralised, invisible characters stripped). Notes flagged by `memory.Suspicious` and not written by the user are never injected automatically (`memory.Injectable`).
- **Budgets:** anything automatic (session start, `mem_context`) has a token cap. Pinned notes first, then decisions/conventions, recency only as filler.
- **Agents never gain trust:** `mem_save` always writes trust `agent`; only the CLI writes `user`.
- Hooks must never fail or pollute a session: exit 0, clean stdout.
- No network access, no telemetry, no cgo (pure-Go SQLite).

## Style

Standard `gofmt`, small packages, errors wrapped with `%w`, tests next to the code. Comments explain why, not what. User-facing text is plain English; Spanish lives in `README.es.md`.
