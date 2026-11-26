# Changelog

All notable changes to noah. Versions follow [Semantic Versioning](https://semver.org); while noah is below 1.0 the storage format and commands may still change, and any schema change migrates your database automatically on first run.

## Unreleased

### Added
- **Search by relative date.** A question such as "what did I buy two weeks ago?" or "qué compré hace dos semanas" (also yesterday, last week/month/year, ayer, el mes pasado) is turned into a date window; notes created then rank first and recency stops favouring the newest note. The date words no longer count as search terms. LongMemEval-S temporal-reasoning hit@5 went from 94.5% to 97.6%; `TestRecall` is unchanged.
- **LongMemEval-S benchmark** (`TestLongMemEval`, skipped unless `NOAH_LONGMEMEVAL` points at the dataset). noah finds the answer session in the top 5 for 98.1% of the 470 scored questions (97.0% with one memory per exchange); a random ranking scores 18.9%. Weakest category: preferences, 86.7%, where the question shares no words with the answer. Method and commands in the README and `docs/ARCHITECTURE.md`.

- Logos of Rubio (Rubio-IA) and Noah (noah), plus the shared social preview image and Roma. See `assets/BRAND.md`.

## v0.4.2

### Added
- **Search by relative date.** A question such as "what did I buy two weeks ago?" or "qué compré hace dos semanas" (also yesterday, last week/month/year, ayer, el mes pasado) is turned into a date window; notes created then rank first and recency stops favouring the newest note. The date words no longer count as search terms. LongMemEval-S temporal-reasoning hit@5 went from 94.5% to 97.6%; `TestRecall` is unchanged.
- **Easy install and upgrade.** `install.sh` (one line, macOS and Linux, verifies the checksum, never uses `sudo`, same line upgrades), a Homebrew formula (`Formula/noah.rb`) and a Scoop manifest (`bucket/noah.json`). The release workflow regenerates the formula and manifest from each release's checksums, only for the latest release. `scripts/packaging.sh` does the generating.

- **Windows one-line installer.** `install.ps1` (PowerShell, no admin rights, verifies the checksum, same line upgrades). CI installs from the latest release on macOS, Linux and Windows.

### Changed
- README (English and Spanish) opens with how noah and [Rubio-IA](https://github.com/dicapower/rubio-ia) fit together and a three-step quick start for macOS, Linux and Windows.
- The release workflow has a single trigger, runs are serialized per tag, and it checks the published files against `SHA256SUMS`. Before, creating a release started two builds that could upload mismatched files.
- `noah version` now reports the right version also when noah is built without `-ldflags` (a plain `go build` in a tagged checkout, or `go install ...@vX.Y.Z`); before it printed `dev`.
- README (English and Spanish): step-by-step upgrade (downloaded binary with checksum, source, `go install`) and a table with the flags of `save`, `search`, `context`, `stale` and `export`.

## v0.4.1

### Fixed
- **Too many `⚠` flags.** In v0.4.0 any note containing a link was flagged, which on real data meant almost every recent note about a PR or ticket, and those notes were left out of the automatic context. Links are no longer flagged, and "without asking" no longer counts as an override (it mostly appears in protective rules such as "never delete X without asking"). "always send" no longer counts either (it flagged a note saying a service should "always send" a field). Schema v4 and v5 recompute the flags of existing notes on first run.

### Changed
- README (English and Spanish) now has an upgrade section and a note on `⚠` false alarms; `docs/ARCHITECTURE.md` records the detector tuning; `CLAUDE.md` gains a rule for changing the detector.

## v0.4.0

### Added
- **Search by relative date.** A question such as "what did I buy two weeks ago?" or "qué compré hace dos semanas" (also yesterday, last week/month/year, ayer, el mes pasado) is turned into a date window; notes created then rank first and recency stops favouring the newest note. The date words no longer count as search terms. LongMemEval-S temporal-reasoning hit@5 went from 94.5% to 97.6%; `TestRecall` is unchanged.
- **Token budgets.** `mem_search` and `mem_context` take `max_tokens` (default 1,500); the session-start hook is capped by `NOAH_START_TOKENS` (default 1,500). The best hit is always returned.
- **Smarter start-up context.** Pinned notes first, then decisions, preferences and conventions (however old), and recent notes only as filler.
- **Defence against memory poisoning.** Notes containing instructions aimed at the AI, download-and-run commands, links or hidden characters are flagged (`⚠`). A flagged note written by an agent stays searchable, with a warning, but is never injected automatically. Invisible and direction-changing characters are stripped from everything shown to the agent.
- `noah stale` lists notes nobody has used for months and notes flagged as suspicious. noah now records when a memory was last handed to an agent.
- Regression tests for search quality (`TestRecall`, run in CI also at 10,000 memories) and for the token cost of the MCP tool definitions.
- `CLAUDE.md` with commands and the rules contributors must keep.

### Changed
- **Search always widens to "any term".** Before, a question whose terms all matched at least 8 notes never looked further, so the note that held the rare terms but lacked one common word of the question ("configuramos", "producción") was never a candidate. Coverage is also weighted by the rarity of each term among the candidates, whole-word matches count more than prefix matches, and the title has its own weight. On a synthetic benchmark, partial questions answered in the top 5 went from 14/20 to 20/20 at 10,000 memories (19/20 to 20/20 at 1,000); full questions stay 20/20. Cost: median search about 51 ms to 60 ms at 10,000 memories (process start included).
- Schema v3 (adds `last_used_at` and `flags`; existing notes are flagged on first run).

## v0.3.0

### Added
- **Search by relative date.** A question such as "what did I buy two weeks ago?" or "qué compré hace dos semanas" (also yesterday, last week/month/year, ayer, el mes pasado) is turned into a date window; notes created then rank first and recency stops favouring the newest note. The date words no longer count as search terms. LongMemEval-S temporal-reasoning hit@5 went from 94.5% to 97.6%; `TestRecall` is unchanged.
- **Global memory.** The reserved space `global` holds knowledge that applies to every project. Project searches, `noah context`, `mem_context` and the session-start hook include it automatically.
- `noah move <id> <space>` to promote a memory to `global` or move it between projects (CLI only: agents cannot reorganise your memories). A clash in the target space is reported, never overwritten.
- Search results name the space whenever they span more than one.

### Changed
- **Memory ids are never reused.** Deleting the newest memory used to free its number for the next one. The database migrates to schema v2 on first run; memories and the search index are kept.

### Fixed
- **Windows:** noah could not open its database because the drive letter of the path was read as a host name. CI now passes on Windows, macOS and Linux.

## v0.2.0

### Added
- **Search by relative date.** A question such as "what did I buy two weeks ago?" or "qué compré hace dos semanas" (also yesterday, last week/month/year, ayer, el mes pasado) is turned into a date window; notes created then rank first and recency stops favouring the newest note. The date words no longer count as search terms. LongMemEval-S temporal-reasoning hit@5 went from 94.5% to 97.6%; `TestRecall` is unchanged.
- `noah import engram` to bring an existing Engram database over (dry run, secrets redacted, original dates kept).
- Auto-capture hooks for Claude Code: `hook session-start` injects the save protocol and recent memories (also after compaction); `hook stop` optionally nudges the agent to save after long work.
- Relevance cutoff: weak matches are dropped instead of padding the results.
- Apache-2.0 license, English and Spanish README, CI on Linux, macOS and Windows.

## v0.1.0

- First release: Go + SQLite (FTS5) memory server over MCP stdio, project spaces, trust levels, secret redaction, topic upserts, `noah doctor`, export.
