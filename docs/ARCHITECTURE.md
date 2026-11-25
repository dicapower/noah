# Architecture decisions

Status: v0.1 implemented (store, search, MCP server, CLI, Claude Code hook, Engram importer).
Each decision lists the evidence behind it. Numbers come from synthetic corpora
of ~900-character memories on a 2 vCPU machine and should be read as relative,
not absolute. `NOAH_SCALE=1000,10000 go test ./internal/search -run TestScale -v`
reproduces the scale table below.

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
| Tokenizer | FTS5 `unicode61 remove_diacritics 2` with query-time prefixes, **not** `trigram`; prefix index for 2-4 letter prefixes | Measured against trigram: index 3x smaller (30.6 MB vs 89.3 MB), inserts ~4x faster, search ~2.7x faster. Prefix matching covers simple inflections; embeddings (planned) cover synonyms. The 2-4 letter prefix index halves short-term search (62 to 33 ms at 10k) for +40% size and -20% insert rate; short terms (`api`, `sql`, `jwt`) are common in code. |
| Identifiers | Words containing digits are never trimmed or prefix-matched | Found by the recall benchmark: `zorblax1` also matched `zorblax10..19`. `ENG-12` must not match `ENG-123`. |
| Sessions | None | Engram-style session start/end bookkeeping left 70 of 116 sessions open in the real install. Memories belong to a space, not a session; `updated_at` is enough. |
| Trust | Agents (MCP) write with trust `agent`; only the CLI writes `user`. Agents cannot overwrite, pin or delete `user` memories | Memory poisoning is the main attack on agent memory; a poisoned note must not be able to rewrite what the human wrote. |
| Retrieval | Two phase: AND query first, OR only if fewer than k hits; top-N (50) candidates from FTS, rerank in Go | Ranking inside SQL over every match makes cost scale with match count. Reranking N candidates keeps it bounded. |
| Semantic search | Optional embeddings in their own table, scored only against the FTS candidates | Keeps rows small, avoids brute force over the whole database, and works with no model installed. Embedder is an interface (Ollama endpoint first). |
| Concurrency | One writer connection (`BEGIN IMMEDIATE`, busy timeout) plus a separate read pool, WAL, `synchronous=NORMAL` | Measured under continuous reads: a single shared connection starved the writer (30 writes in 4 s vs 6,116 with split pools). |
| Hooks | Subcommands of the same binary (`noah hook session-start`), no bash, jq or curl | Removes per-prompt process forks and platform quirks. |
| Containers | Not used for the core. Optional Dockerfile only for CI or a future team server | The agent launches the tool per session over stdio and needs host paths for project detection; a container adds startup latency and volume friction for no isolation benefit. |
| Distribution | GitHub Releases (native runners), Homebrew, Scoop, `go install` | |

## Code layout

```
cmd/noah             CLI, `noah mcp`, `noah hook session-start`, `noah setup`
internal/memory      domain: Memory, Trust, space resolution, validation, context rendering (no I/O)
internal/store       SQLite adapter: migrations, save pipeline, reads, doctor
internal/search      two-phase retrieval and reranking over a Source interface
internal/query       natural-language question to FTS5 query (stopwords, diacritics, stems)
internal/secrets     secret detection and write policy
internal/mcpserver   MCP tools adapter (stdio), six tools
```
Not built yet: `internal/embed` (optional embeddings), the cgo driver build tag.

Patterns used on purpose: ports and adapters (store, embedder, scanner behind
interfaces so they are testable and swappable), a save pipeline
(validate → scan → dedupe → persist → index), strategy for rankers and secret
policies, functional options for configuration. Patterns avoided: global
state, ORMs, reflection-heavy DI.

## Measured results (v0.1)

Synthetic corpus, Zipf-distributed vocabulary, ~900 characters per memory, one
space, 100 runs per query type. "Common term" is the single most frequent word
(present in most memories): a deliberately adversarial case. Recall uses 50
natural-language questions ("how did we handle the zorblaxK issue with
flimmerK?") whose answer is one planted memory.

| Memories | Insert/s | DB size | Common term p50/p95 | 3 terms p50/p95 | Short term p50/p95 | Recall top-1 / top-5 |
|---:|---:|---:|---:|---:|---:|---:|
| 1,000 | 938 | 2.6 MB | 5.0 / 7.5 ms | 4.1 / 5.6 ms | 4.8 / 6.0 ms | 50 / 50 |
| 10,000 | 897 | 26 MB | 30 / 44 ms | 13 / 18 ms | 33 / 45 ms | 50 / 50 |
| 50,000 | 866 | 131 MB | 147 / 187 ms | 53 / 64 ms | 139 / 170 ms | 50 / 50 |
| 100,000 | 863 | 259 MB | 276 / 322 ms | 94 / 122 ms | 278 / 315 ms | 50 / 50 |

Process: `noah mcp` initialises in ~7 ms; MCP tool calls take 0.5-3 ms on a small database.

Against Engram v1 measured in the same environment (different synthetic corpus, so
read as order of magnitude): inserts are about 6x faster at 10k and do not
degrade with size (Engram's `add` p50 went from 1.4 ms at 1k to 6.9 ms at 10k);
3-term search is about 3x faster (13 vs ~38 ms), short-term about 1.7x faster
(33 vs 57 ms), the database is half the size (26 vs 52 MB), and natural-language
questions find the right memory (Engram's AND-only mode returned nothing for 5 of 5).
Common-term search is on par (30 vs 25 ms at 10k). Engram did not finish the 50k load.

### Tried and rejected

- **Sorting only ids, then loading rows** (to avoid sorting wide rows): no gain. FTS5 spends its time scoring each match, not sorting.
- **`detail=column`**: 6x slower BM25 here, rejected.
- **Min-max normalising BM25 across candidates**: with few candidates a tiny score gap became the whole 0..1 range and drowned recency, trust and pinned. Scores are now relative to the best match.
- **`ORDER BY rank`**: slower than explicit `bm25()` with column weights.

## Known limits

- **Vocabulary mismatch.** Search matches words and simple inflections. "What database did we choose?" does not find a note that only says "SQLite". Optional embeddings are designed for this and not built yet.
- **Cost grows linearly with the number of memories matching the query**, at about 1.7 microseconds each, because FTS5 scores every match. A term present in most memories at 100k memories costs ~280 ms. Typical queries match far fewer. A document-frequency cut (drop near-universal terms when others exist) is the next optimisation.
- **Benchmarks are synthetic and lexically dense.** Recall on real memories still has to be measured; the 50/50 comes from distinctive planted identifiers.
- The default build uses the pure-Go SQLite driver; a cgo build tag (about 2x faster searches in earlier measurements) is not wired yet.
- The project space comes from the directory the agent launches `noah mcp` in. Agents that start MCP servers outside the project directory need `NOAH_SPACE` or `--space`.
- No multi-machine sync. Use `noah export` and Git for sharing.
