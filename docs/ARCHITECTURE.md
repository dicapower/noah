# Architecture decisions

Status: draft v0. Each decision lists the evidence behind it. Numbers come from
`bench/` (synthetic corpus, 2 vCPU, 20k observations of ~900 chars) and should
be read as relative, not absolute.

## Principles

1. **One static binary, no daemon, no network surface by default.** The agent
   launches `noah mcp` over stdio. There is no HTTP server unless you ask for
   a local Unix socket.
2. **Local SQLite is the single source of truth.** No cloud, no sync in the core.
3. **Isolation first.** Memory belongs to a *space* (a repo, a client). Reads
   never cross spaces unless asked.
4. **Memory is untrusted input.** Every entry carries its source and a trust
   level; injected context labels it and never lets low-trust entries act as
   instructions.
5. **Nothing sensitive goes in.** Secrets are detected on write.

## Decisions

| Area | Decision | Why |
|---|---|---|
| Language | Go | The hot path is C SQLite either way, so Go vs Rust changes little for performance. Go gives trivial cross-compilation, a single binary and a larger pool of contributors for agent tooling. |
| SQLite driver | `database/sql` over two build-tagged drivers: pure-Go `modernc.org/sqlite` (default, `go install` works anywhere) and cgo `mattn/go-sqlite3` for release binaries | Measured: cgo is ~2.2x faster on search (15 ms vs 34 ms p50) and ~1.4x on inserts. Pure Go keeps contribution and cross-builds simple. Same code, same tests. |
| Tokenizer | FTS5 `unicode61 remove_diacritics 2` with query-time prefixes, **not** `trigram` | Measured against trigram: index 3x smaller (30.6 MB vs 89.3 MB), inserts ~4x faster, search ~2.7x faster. Prefix matching covers simple inflections; embeddings cover synonyms. |
| Retrieval | Two phase: AND query first, OR only if fewer than k hits; top-N (50) candidates from FTS, rerank in Go | Ranking inside SQL over every match makes cost scale with match count. Reranking N candidates keeps it bounded. |
| Semantic search | Optional embeddings in their own table, scored only against the FTS candidates | Keeps rows small, avoids brute force over the whole database, and works with no model installed. Embedder is an interface (Ollama endpoint first). |
| Concurrency | One writer connection (`BEGIN IMMEDIATE`, busy timeout) plus a separate read pool, WAL, `synchronous=NORMAL` | Measured under continuous reads: a single shared connection starved the writer (30 writes in 4 s vs 6,116 with split pools). |
| Hooks | Subcommands of the same binary (`noah hook session-start`), no bash, jq or curl | Removes per-prompt process forks and platform quirks. |
| Containers | Not used for the core. Optional Dockerfile only for CI or a future team server | The agent launches the tool per session over stdio and needs host paths for project detection; a container adds startup latency and volume friction for no isolation benefit. |
| Distribution | GitHub Releases (native runners), Homebrew, Scoop, `go install` | |

## Code layout

```
cmd/noah            thin CLI: flags, wiring, no logic
internal/memory      domain: Observation, Space, Trust, policies (no I/O)
internal/store       SQLite adapter behind the memory.Store interface
internal/search      query building, candidate retrieval, reranking
internal/secrets     secret detection and write policy
internal/embed       Embedder interface and adapters
internal/mcpserver   MCP tools adapter (stdio)
internal/hooks       agent hook handlers
```

Patterns used on purpose: ports and adapters (store, embedder, scanner behind
interfaces so they are testable and swappable), a save pipeline
(validate → scan → dedupe → persist → index), strategy for rankers and secret
policies, functional options for configuration. Patterns avoided: global
state, ORMs, reflection-heavy DI.

## Known limits

- Benchmarks use a synthetic, lexically dense corpus; recall on real memories
  must be measured separately.
- Pure-Go builds are slower than cgo builds.
- No multi-machine sync. Use Git for sharing exports.
