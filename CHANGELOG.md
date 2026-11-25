# Changelog

All notable changes to noah. Versions follow [Semantic Versioning](https://semver.org); while noah is below 1.0 the storage format and commands may still change, and any schema change migrates your database automatically on first run.

## Unreleased

### Added
- **Token budgets.** `mem_search` and `mem_context` take `max_tokens` (default 1,500); the session-start hook is capped by `NOAH_START_TOKENS` (default 1,500). The best hit is always returned.
- **Smarter start-up context.** Pinned notes first, then decisions, preferences and conventions (however old), and recent notes only as filler.
- **Defence against memory poisoning.** Notes containing instructions aimed at the AI, download-and-run commands, links or hidden characters are flagged (`⚠`). A flagged note written by an agent stays searchable, with a warning, but is never injected automatically. Invisible and direction-changing characters are stripped from everything shown to the agent.
- `noah stale` lists notes nobody has used for months and notes flagged as suspicious. noah now records when a memory was last handed to an agent.
- Regression tests for search quality (`TestRecall`, run in CI also at 10,000 memories) and for the token cost of the MCP tool definitions. `bench/bench.py` is the cross-tool benchmark.
- `CLAUDE.md` with commands and the rules contributors must keep.

### Changed
- **Search always widens to "any term".** Before, a question whose terms all matched at least 8 notes never looked further, so the note that held the rare terms but lacked one common word of the question ("configuramos", "producción") was never a candidate. Coverage is also weighted by the rarity of each term among the candidates, whole-word matches count more than prefix matches, and the title has its own weight. On the cross-tool benchmark, partial questions answered in the top 5 went from 14/20 to 20/20 at 10,000 memories (19/20 to 20/20 at 1,000); full questions stay 20/20. Cost: median search about 51 ms to 60 ms at 10,000 memories (process start included).
- Schema v3 (adds `last_used_at` and `flags`; existing notes are flagged on first run).

## v0.3.0

### Added
- **Global memory.** The reserved space `global` holds knowledge that applies to every project. Project searches, `noah context`, `mem_context` and the session-start hook include it automatically.
- `noah move <id> <space>` to promote a memory to `global` or move it between projects (CLI only: agents cannot reorganise your memories). A clash in the target space is reported, never overwritten.
- Search results name the space whenever they span more than one.

### Changed
- **Memory ids are never reused.** Deleting the newest memory used to free its number for the next one. The database migrates to schema v2 on first run; memories and the search index are kept.

### Fixed
- **Windows:** noah could not open its database because the drive letter of the path was read as a host name. CI now passes on Windows, macOS and Linux.

## v0.2.0

### Added
- `noah import engram` to bring an existing Engram database over (dry run, secrets redacted, original dates kept).
- Auto-capture hooks for Claude Code: `hook session-start` injects the save protocol and recent memories (also after compaction); `hook stop` optionally nudges the agent to save after long work.
- Relevance cutoff: weak matches are dropped instead of padding the results.
- Apache-2.0 license, English and Spanish README, CI on Linux, macOS and Windows.

## v0.1.0

- First release: Go + SQLite (FTS5) memory server over MCP stdio, project spaces, trust levels, secret redaction, topic upserts, `noah doctor`, export.
