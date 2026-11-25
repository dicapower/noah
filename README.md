# noah

Persistent memory for AI coding agents. One static binary, local SQLite, no daemon, no network.

Agents forget everything when a session ends. `noah` gives Claude Code, Cursor and any other
[MCP](https://modelcontextprotocol.io) client a shared, searchable memory of your decisions,
bug fixes and conventions, kept per project on your own machine.

> **Status: v0.1, pre-release.** The core works and is tested. See [Known limits](docs/ARCHITECTURE.md#known-limits).

## Quick start

```sh
go install github.com/dicapower/noah/cmd/noah@latest   # or download a binary from Releases

noah save --kind decision --topic auth/provider "We use Firebase Auth with custom claims for roles"
noah search "which provider handles login?"
noah context
```

Connect your agent (this prints the exact commands and never edits your config):

```sh
noah setup claude-code
noah setup cursor
```

## What the agent gets

Six MCP tools, kept small because every tool description costs context on every request:

| Tool | Purpose |
|---|---|
| `mem_search` | Natural-language search, returns best matches with snippets |
| `mem_save` | Save a memory; the same `topic` revises it instead of duplicating |
| `mem_get` | Read one memory in full |
| `mem_context` | Pinned and most recent memories of this project |
| `mem_forget` | Delete a memory that is wrong or obsolete |
| `mem_spaces` | List projects and their sizes |

With the optional Claude Code hook (`noah hook session-start`), recent memories are injected at the start of every session.

## How it works

- **Spaces.** Memory belongs to a project: the `NOAH_SPACE` variable, the nearest `.noah` file, the git root, or the folder name. The home directory maps to `_unscoped` so unrelated work never piles up under one project. Searches never cross projects unless asked (`scope: "all"`).
- **Search.** SQLite FTS5 finds candidates (all terms first, any term if there are too few), then a small reranker in Go weighs relevance, term coverage, recency, trust and pinned notes. Accents are ignored, simple inflections match, identifiers like `ENG-123` match exactly.
- **Concurrency.** WAL mode with one writer connection and a separate read pool, so several agents can read and write the same database at once.
- **No sessions to leak.** There is no session start/end bookkeeping that can be left open.

## Security model

Agent memory is an attack surface: anything an agent saves is read back into a future prompt.

- **Secrets are redacted before they are stored** (API keys, tokens, private keys, passwords in URLs and assignments). Set `NOAH_SECRETS=block` to refuse instead, or `off` to disable. `noah doctor` scans existing memories.
- **Trust levels.** Memories written by you (CLI) are `user`; memories written by an agent are `agent`. An agent cannot overwrite, pin or delete a `user` memory.
- **Labelled context.** Injected memories are wrapped in a block that says they are reference data, not instructions, and shows who wrote each one. The closing tag cannot be forged from inside a memory.
- **No listening port.** The agent starts `noah mcp` over stdio. There is no HTTP server for a web page to talk to.
- The database file is created with mode `0600`.

## Commands

```
noah mcp | setup | hook session-start        agent integration
noah save | search | context | get | forget  memories
noah spaces | space                          projects
noah doctor | export | version               maintenance
```

Environment: `NOAH_DB` (default `~/.noah/noah.db`), `NOAH_SPACE`, `NOAH_SECRETS`.

## Performance

On a 2 vCPU machine, 10,000 memories of ~900 characters: about 900 inserts/s, 26 MB, searches in 13-33 ms, and 50 of 50 natural-language questions answered correctly. At 100,000 memories searches take 94-280 ms. Full table, method and what was tried and rejected: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Development

```sh
go test ./...
NOAH_SCALE=1000,10000 go test ./internal/search -run TestScale -v   # scale benchmark
```

Pure Go, no cgo: it cross-compiles to macOS, Linux and Windows with `CGO_ENABLED=0`.

## License

Not yet chosen.
