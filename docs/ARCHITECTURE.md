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
| SQLite driver | Pure-Go `modernc.org/sqlite` only, `CGO_ENABLED=0` everywhere | `go install` works anywhere and cross-builds are trivial. An earlier measurement found cgo `mattn/go-sqlite3` about 2.2x faster on search (15 ms vs 34 ms p50) and 1.4x on inserts; that gain was judged not worth losing single-binary cross-compilation, so no cgo driver exists. |
| Dates | `query.Time` parses "N days/weeks/months/years ago", yesterday, last week/month/year, calendar dates ("March 3rd", "3 de marzo", ISO) and "last <weekday>" (English and Spanish) into an age window, removes the phrase from the text match, and the reranker blends a creation-date fit into relevance (35%) while switching recency off | Agents ask "what did we decide last week?" and words alone cannot answer it. It adds no schema, tool or dependency. Measured on LongMemEval-S: temporal-reasoning hit@5 0.945 to 0.976. Dates use creation time, not update time, because the question asks when it happened. |
| Tokenizer | FTS5 `unicode61 remove_diacritics 2` with query-time prefixes, **not** `trigram`; prefix index for 2-4 letter prefixes | Measured against trigram: index 3x smaller (30.6 MB vs 89.3 MB), inserts ~4x faster, search ~2.7x faster. Prefix matching covers simple inflections; embeddings (planned) cover synonyms. The 2-4 letter prefix index halves short-term search (62 to 33 ms at 10k) for +40% size and -20% insert rate; short terms (`api`, `sql`, `jwt`) are common in code. |
| Identifiers | Words containing digits are never trimmed or prefix-matched | Found by the recall benchmark: `zorblax1` also matched `zorblax10..19`. `ENG-12` must not match `ENG-123`. |
| Hooks (Claude Code) | `SessionStart` injects a trusted protocol plus recent memories on startup, resume, clear and compact; optional `Stop` asks once per session to save after 10+ tool calls with no noah save | Claude Code cannot inject context from `PreCompact`, so SessionStart with source `compact` is what restores memory after compaction. The Stop nudge reads the transcript locally, is capped at one per session and honours `stop_hook_active` to avoid loops. |
| Global space | Reserved space `global`, searched with every project search and injected in the session context (max 6 recent) | Knowledge that is true across projects had no home; a per-project store forced either duplication or losing it. Moving a memory is a CLI-only operation (`noah move`), so an agent cannot reshuffle what the user organised. |
| Sessions | None | Session start/end bookkeeping left 70 of 116 sessions open in a real install. Memories belong to a space, not a session; `updated_at` is enough. |
| Trust | Agents (MCP) write with trust `agent`; only the CLI writes `user`. Agents cannot overwrite, pin or delete `user` memories | Memory poisoning is the main attack on agent memory; a poisoned note must not be able to rewrite what the human wrote. |
| Retrieval | AND query first, then (since v0.4) always an OR query when the question has more than one term; top-N (50) candidates from FTS, rerank in Go | Ranking inside SQL over every match makes cost scale with match count. Reranking N candidates keeps it bounded. The OR phase used to run only when AND found fewer than 8 hits, which hid the note holding the rare terms of a question whenever the common words matched many others. |
| Semantic search | Optional embeddings in their own table, scored only against the FTS candidates | Keeps rows small, avoids brute force over the whole database, and works with no model installed. Embedder is an interface (Ollama endpoint first). |
| Concurrency | One writer connection (`BEGIN IMMEDIATE`, busy timeout) plus a separate read pool, WAL, `synchronous=NORMAL` | Measured under continuous reads: a single shared connection starved the writer (30 writes in 4 s vs 6,116 with split pools). |
| Hooks | Subcommands of the same binary (`noah hook session-start`), no bash, jq or curl | Removes per-prompt process forks and platform quirks. |
| Containers | Not used for the core. Optional Dockerfile only for CI or a future team server | The agent launches the tool per session over stdio and needs host paths for project detection; a container adds startup latency and volume friction for no isolation benefit. |
| Distribution | GitHub Releases (5 platforms, `CGO_ENABLED=0`, `SHA256SUMS`); `install.sh` (macOS/Linux, verifies the checksum, no sudo, same line upgrades); Homebrew formula and Scoop manifest generated into the main repo by the release workflow; `go install` | Easy install and upgrade matter as much as features. The tap lives in the main repo (`brew tap dicapower/noah <url>`, `scoop bucket add noah <url>`), so there is no second repository and no personal access token to leak or expire; the workflow commits with the default token and only for the latest release. No `noah upgrade`: the binary makes no network connections. |

## Code layout

```
cmd/noah             CLI, `noah mcp`, `noah hook session-start`, `noah setup`
internal/memory      domain: Memory, Trust, space resolution, validation, context rendering (no I/O)
internal/store       SQLite adapter: migrations, save pipeline, reads, doctor
internal/search      two-phase retrieval and reranking over a Source interface
internal/query       natural-language question to FTS5 query (stopwords, diacritics, stems) and relative-date parsing (`Time`)
internal/secrets     secret detection and write policy
internal/mcpserver   MCP tools adapter (stdio), six tools
```
Not built yet: `internal/embed` (optional embeddings).

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

### Tried and rejected

- **Sorting only ids, then loading rows** (to avoid sorting wide rows): no gain. FTS5 spends its time scoring each match, not sorting.
- **`detail=column`**: 6x slower BM25 here, rejected.
- **Min-max normalising BM25 across candidates**: with few candidates a tiny score gap became the whole 0..1 range and drowned recency, trust and pinned. Scores are now relative to the best match.
- **`ORDER BY rank`**: slower than explicit `bm25()` with column weights.

## LongMemEval-S

`TestLongMemEval` (`internal/search/longmemeval_test.go`) runs the public LongMemEval-S retrieval task through the normal path: one empty database per question, its ~48 history sessions saved with `Store.Save` (secret redaction included), the question asked verbatim to `search.Run` with recency measured at the question date. A question is a hit@k when one of the first k results comes from `answer_session_ids`; the 30 abstention questions are skipped. It reports and never asserts, so it cannot gate a change the way `TestRecall` does.

| Granularity | hit@1 | hit@5 | hit@10 | Preferences hit@5 |
|---|---:|---:|---:|---:|
| one memory per session (47 per question) | 0.883 | 0.981 | 0.989 | 0.867 |
| one memory per exchange (248 per question) | 0.864 | 0.970 | 0.985 | 0.800 |

- Controls: a random ranking gives 0.189 at 5 (answer sessions out of ~48, computed from the data). Two of the misses were read by hand and are genuine vocabulary gaps (question "music streaming service", answer session about Spotify; "homegrown ingredients" against basil and mint), not mapping errors.
- Relative dates ("two weeks ago") are parsed by `query.Time` and ranked by when the note was created; before that change session hit@5 was 0.972 and temporal-reasoning 0.945 (now 0.976). One-memory-per-exchange hit@5 did not move.
- Tried and rejected: pseudo-relevance feedback (add the words that the top 3 hits share and the question lacks, search again). It lowered session hit@5 from 0.981 to 0.968 and preferences from 0.867 to 0.767, because on a vague question the first hits are the wrong ones and their words drag the search further away. Preferences need meaning (embeddings) or tags written by the agent at save time, not more words from the same hits.
- Per-exchange hit@1 is lower because several exchanges of one session compete for the top places; hit@5 barely moves.
- Roles are not prefixed to the text, sessions over 64 KB are cut (one case), and all questions are run in one process on 2 vCPUs in about 3 minutes.
- Retrieval only. It says nothing about whether a model answers correctly with the result, and other tools' published numbers use their own harnesses and are not directly comparable.

## Known limits

- **Vocabulary mismatch.** Search matches words and simple inflections. "What database did we choose?" does not find a note that only says "SQLite". Optional embeddings are designed for this and not built yet.
- **Cost grows linearly with the number of memories matching the query**, at about 1.7 microseconds each, because FTS5 scores every match. A term present in most memories at 100k memories costs ~280 ms. Typical queries match far fewer. A document-frequency cut (drop near-universal terms when others exist) is the next optimisation.
- **The synthetic benchmark is lexically dense.** The 50/50 comes from distinctive planted identifiers. LongMemEval-S (above) is closer to real text, but it is chat transcripts, not the short notes an agent writes; recall on real memories still has to be measured.
- Pure-Go SQLite only: searches are about 2x slower than a cgo driver would be in earlier measurements. Accepted on purpose (see Decisions).
- The project space comes from the directory the agent launches `noah mcp` in. Agents that start MCP servers outside the project directory need `NOAH_SPACE` or `--space`.
- No multi-machine sync. Use `noah export` and Git for sharing.

## Context management (v0.4)

What reaches the agent unprompted is paid for in every turn and read as if it were true, so it is budgeted and treated as untrusted.

- **Budgets, not counts.** Automatic context (session start, `mem_context`) and `mem_search` stop at a token budget (default 1,500; `NOAH_START_TOKENS`, `max_tokens`). Tokens are estimated at 3.5 characters each (about ±20%), enough for a budget without shipping a tokenizer. The best hit is always returned.
- **Priority, not recency.** `Store.Context` orders pinned notes, then decisions/preferences/conventions/architecture/gotchas (they stay true), then everything else by recency. Recency alone lets the last chatty note push out a decision from last month.
- **After compaction** the SessionStart hook fires again with source `compact`; noah reinjects the same prioritised context with a one-line notice. A PreCompact hook was considered and left out: Claude Code does not add a PreCompact hook's output to the conversation, so noah could not put anything in front of the model there. The Stop hook already asks the agent to save after long work, which is the moment that matters.
- **Poisoning.** `memory.Suspicious` flags instruction-like text, download-and-run commands and invisible characters at save time (retroactively in migrations v3, v4 and v5). A link alone is not flagged: it is in nearly every note about a PR or ticket. Flagged notes written by an agent or an external source are excluded from automatic injection but stay searchable with a warning; `noah stale` lists them. Invisible and bidirectional characters are stripped wherever memory text is shown. Tuning lesson (v0.4.0 to v0.4.1): the first version also flagged links, "without asking" and "always send"; on a real database of 862 notes it flagged the 30 most recent notes it listed, and every one was a false alarm that hid the note from automatic context. A flag that fires on ordinary notes is worse than none, because it silently removes memory. Narrow patterns, test them against real notes (`noah stale`), and when a pattern changes, append a migration that recomputes flags. The detector is regex-based and will miss a determined attacker; its job is to make the ordinary cases visible and to keep them from being replayed on their own.
- **Review.** `last_used_at` is set whenever a memory is returned to an agent. `noah stale` lists what has not been used or revised for 180 days (pinned notes excluded).
- **Ranking.** Coverage is weighted by inverse document frequency among the FTS candidates and the title has its own weight, so a term present in almost every candidate ("producción") no longer counts as much as a rare one. `TestRecall` guards it.
