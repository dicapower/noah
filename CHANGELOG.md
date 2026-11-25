# Changelog

All notable changes to noah. Versions follow [Semantic Versioning](https://semver.org); while noah is below 1.0 the storage format and commands may still change, and any schema change migrates your database automatically on first run.

## v0.3.0

### Added
- **Global memory.** The reserved space `global` holds knowledge that applies to every project. Project searches, `noah context`, `mem_context` and the session-start hook include it automatically.
- `noah move <id> <space>` to promote a memory to `global` or move it between projects (CLI only: agents cannot reorganise your memories). A clash in the target space is reported, never overwritten.
- Search results name the space whenever they span more than one.

### Changed
- **Memory ids are never reused.** Deleting the newest memory used to free its number for the next one. The database migrates to schema v2 on first run; memories and the search index are kept.

## v0.2.0

### Added
- `noah import engram` to bring an existing Engram database over (dry run, secrets redacted, original dates kept).
- Auto-capture hooks for Claude Code: `hook session-start` injects the save protocol and recent memories (also after compaction); `hook stop` optionally nudges the agent to save after long work.
- Relevance cutoff: weak matches are dropped instead of padding the results.
- Apache-2.0 license, English and Spanish README, CI on Linux, macOS and Windows.

## v0.1.0

- First release: Go + SQLite (FTS5) memory server over MCP stdio, project spaces, trust levels, secret redaction, topic upserts, `noah doctor`, export.
