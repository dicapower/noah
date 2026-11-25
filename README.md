<p align="center">
  <img src="assets/logo.svg" alt="noah" width="140">
</p>

<h1 align="center">noah</h1>

<p align="center"><b>Persistent memory for AI coding agents.</b><br>
One file, runs locally, no account, no cloud, no background service.</p>

<p align="center"><a href="README.es.md">Leer en español</a> · <a href="docs/ARCHITECTURE.md">Architecture</a> · License: Apache-2.0</p>

---

## Why

Coding agents (Claude Code, Cursor, …) forget everything when a session ends. `noah` gives them a
searchable notebook that lives on your machine: decisions you made, bugs you fixed, conventions
you follow. The agent saves things as it works and finds them again in the next session, in the
right project, without you pasting context.

- **Private.** Everything stays in one SQLite file on your computer. Nothing is sent anywhere.
- **Simple.** One binary. No server to run, no port to open, nothing to keep alive.
- **Fast.** Searches take milliseconds; 10,000 memories fit in about 26 MB.
- **Safe by design.** Secrets are redacted before they are stored, and a memory an agent writes can never override one you wrote.
- **Per project.** Memories belong to the project you are in and never leak into another.

> **Status: v0.4, pre-1.0.** It works and is tested, but it is young; the storage format and commands may still change (databases migrate automatically). Known limits are listed [at the end](#known-limits).

## Install

Pick one. All of them install the same single binary (`noah`), with no other dependencies.

**macOS and Linux, one line** (also used to upgrade):

```sh
curl -fsSL https://raw.githubusercontent.com/dicapower/noah/main/install.sh | sh
```

It downloads the latest release, verifies its checksum, and installs next to your existing `noah` (or in `/usr/local/bin`, or `~/.local/bin` if that is not writable). It never uses `sudo`; if the folder is not in your `PATH` it prints the line to add.

**Homebrew** (macOS and Linux):

```sh
brew tap dicapower/noah https://github.com/dicapower/noah
brew install dicapower/noah/noah
```

**Windows** ([Scoop](https://scoop.sh)):

```powershell
scoop bucket add noah https://github.com/dicapower/noah
scoop install noah
```

**From source** (needs [Go 1.26+](https://go.dev/dl/)):

```sh
git clone https://github.com/dicapower/noah.git
cd noah
go build -o noah ./cmd/noah
sudo mv noah /usr/local/bin/        # or any folder in your PATH
```

Or download a ready-made archive from the [Releases page](https://github.com/dicapower/noah/releases) (Linux, macOS and Windows; each release lists `SHA256SUMS`). Check it with `noah version`. See [CHANGELOG.md](CHANGELOG.md).

It is pure Go with no C dependencies, so the same code builds on macOS, Linux and Windows.

### Upgrading

| You installed with | Upgrade with |
|---|---|
| the install script | run the same `curl … \| sh` line again |
| Homebrew | `brew update && brew upgrade dicapower/noah/noah` |
| Scoop | `scoop update noah` |
| a downloaded archive | unpack the new one over the old `noah` (on Windows close your agent first) and check it with `shasum -a 256 -c SHA256SUMS --ignore-missing` |
| source | `git pull && go build -o noah ./cmd/noah`, then replace the old binary |

Then:

1. **Restart your agent** so it launches the new binary. A running agent keeps using the old one until it restarts, whatever you installed.
2. Run `noah version` and `noah doctor`. The database migrates itself the first time the new binary opens it; migrations only add, and a database from a newer noah is refused rather than damaged.
3. Skim the [CHANGELOG](CHANGELOG.md) for behaviour changes. If you want a copy first, `noah export > ~/noah-backup.jsonl` (cheap, and the only step you can skip).

noah never updates itself: it makes no network connections by design. Nothing to reinstall in your agent's hooks, which call the same `noah` binary.

## Connect your agent (2 minutes)

`noah setup` prints the exact commands for your machine. It never edits your configuration.

### Claude Code

```sh
claude mcp add noah --scope user -- noah mcp
```

Recommended: add the hooks below to `~/.claude/settings.json`. They make saving a habit instead of a hope:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "noah hook session-start" } ] }
    ],
    "Stop": [
      { "hooks": [ { "type": "command", "command": "noah hook stop" } ] }
    ]
  }
}
```

- **`SessionStart`** runs when a session starts, resumes, is cleared or is compacted. It tells the agent when to save and loads the project's recent memories, so nothing is lost after a compaction. It works even in a project with no memories yet.
- **`Stop`** (optional) runs when the agent finishes. If it did substantial work (10+ tool calls) and saved nothing to noah since, it asks the agent, **once per session**, to save what it learned first. It reads the session log locally; no AI is involved. Change the threshold with `NOAH_STOP_MIN_TOOLS`.

### Cursor

Add to `~/.cursor/mcp.json` (or `.cursor/mcp.json` inside a project):

```json
{ "mcpServers": { "noah": { "command": "noah", "args": ["mcp"] } } }
```

### Any other MCP client

Run `noah mcp` as a stdio MCP server.

Then just work. Ask your agent things like *"remember that we use Firebase Auth for login"* or
*"what did we decide about the database?"*. The agent decides when to save and search using the
tools below.

## Moving from Engram

Bring your existing memories with one command. Your Engram database is only read, from a temporary copy.

```sh
noah import engram --dry-run     # preview: counts per project, nothing is written
noah import engram               # do it (default file: ~/.engram/engram.db)
```

Each Engram project becomes a noah project. Topic keys, types, pinned flags and original dates are kept; deleted and superseded entries are skipped; secrets found inside are redacted. Running it twice does not duplicate anything. Imported memories are marked as written by an agent; add `--trust user` if you want them protected from agent edits.

## Using it from the terminal

```sh
noah save --kind decision --topic auth/provider "We use Firebase Auth with custom claims for roles"
noah search "which provider handles login?"
noah search --all "docker deploy"        # across every project
noah context                              # pinned + most recent memories of this project
noah get 42
noah forget 42
```

`--topic` is a stable name: saving again with the same topic updates the memory instead of creating a duplicate. `--pin` keeps a memory always in context.

## What the agent gets

Six tools, kept deliberately few because every tool description costs context on every request (about 1,000 tokens for all six, enforced by a test). `mem_search` and `mem_context` accept `max_tokens` so an answer never floods the window.

| Tool | What it does |
|---|---|
| `mem_search` | Natural-language search; returns the best matches with a snippet |
| `mem_save` | Save a memory; the same `topic` revises it |
| `mem_get` | Read one memory in full |
| `mem_context` | Pinned notes, then decisions and conventions, then recent ones, within a token budget |
| `mem_forget` | Delete a memory that is wrong or obsolete |
| `mem_spaces` | List projects and their sizes |

## Global memory

Some knowledge applies to every project: how you like to work, your hosting setup, general conventions. Save it in the `global` space and it shows up everywhere: in the session context and in every project search.

```sh
noah save --space global "Client apps are deployed on our Dokploy instance behind Traefik"
noah move 42 global          # promote an existing memory to global
noah move 42 my-project      # or move it to another project
```

Agents can do the same by saving with `space: "global"`. Keep it for things that are true across projects; project-specific notes stay in their project.

## How projects work

A memory belongs to a *space* (project), chosen in this order:

1. the `NOAH_SPACE` environment variable,
2. the nearest `.noah` file (put a project name in it to override),
3. the git repository root folder name,
4. the current folder name.

Your home folder and `/` map to `_unscoped`. Searches stay inside the current project unless you ask for all (`--all`, or `scope: "all"` from the agent). Run `noah space` to see which project you are in.

> If your agent starts MCP servers outside the project folder, set `NOAH_SPACE` in its MCP configuration.

## Security

Agent memory is an attack surface: whatever an agent saves is read back into a future prompt.

- **Secrets never reach the database.** API keys, tokens, private keys and passwords in URLs are redacted on save. `NOAH_SECRETS=block` refuses instead; `off` disables. `noah doctor` scans what is already stored.
- **Two trust levels.** What you save from the terminal is `user`; what an agent saves is `agent`. An agent cannot overwrite, pin or delete a `user` memory.
- **Flagged notes.** A note that tells the AI to ignore its rules, runs a download-and-execute command or hides characters gets a `⚠` flag. If an agent wrote it, it is searchable (with a warning) but never injected into a session on its own. `noah stale` lists them for review. This is a heuristic: it makes the common cases visible, not every attack.
- **Labelled context.** Injected memories are wrapped in a block that says they are reference notes, not instructions, and shows who wrote each. A memory cannot close that block from the inside.
- **No network.** The agent launches `noah mcp` over stdio. There is no HTTP server for a web page to reach. The database file is created private (`0600`).

## All commands

```
noah mcp | setup | hook session-start|stop   agent integration
noah save | search | context | get | forget  memories
noah spaces | space | move                   projects
noah import engram                           migrate from Engram
noah doctor | stale | export | version       maintenance
```

`noah <command> -h` shows the flags. The ones you will use most:

| Command | Flags |
|---|---|
| `noah save [text\|-]` | `-title`, `-kind` (decision, bugfix, pattern, note…), `-topic` (stable key: saving again with it revises the note), `-tag` (repeatable), `-pin`, `-space` |
| `noah search <question>` | `-all` (every project), `-kind`, `-limit` (default 8), `-json`, `-space` |
| `noah context` | `-limit` (default 12), `-max-tokens` (default 1500), `-space` |
| `noah stale` | `-days` (default 180), `-limit` (default 30), `-space`. Lists notes nobody has used for months and notes flagged `⚠` |
| `noah export` | `-space`. `noah export > backup.jsonl` writes everything as JSON lines |

`noah get <id>` shows a note in full (including why it is flagged); `noah forget <id>` deletes it; `noah move <id> <space>` moves it.

| Environment variable | Meaning | Default |
|---|---|---|
| `NOAH_DB` | database file | `~/.noah/noah.db` |
| `NOAH_SPACE` | force the project | detected |
| `NOAH_SECRETS` | `redact`, `block` or `off` | `redact` |
| `NOAH_START_TOKENS` | size cap of the context injected at session start | `1500` |

## Troubleshooting

- **`command not found: noah`.** The folder with the binary is not in your `PATH`. The install script prints the line to add; or run `which noah` / `echo $PATH`. Typing `./noah` from the folder that holds it always works. Do not paste commands with a trailing `# comment` into zsh: it does not treat `#` as a comment by default.
- **The agent does not see the tools.** Restart the agent after adding the server and check the path with `which noah`; use the full path in the config if the agent does not inherit your `PATH`.
- **Searches return nothing.** You may be in a different project than the one where you saved. Run `noah space`, `noah spaces`, or search with `--all`.
- **Something looks wrong.** `noah doctor` checks database integrity, the search index and stored secrets.
- **A note I wrote is flagged `⚠`.** Run `noah get <id>` to see why. If it is a false alarm, [open an issue](https://github.com/dicapower/noah/issues) with the sentence that triggered it; the detector is deliberately narrow, and real notes are the best test data. `noah forget <id>` removes a note you no longer want.
- **Start over.** Delete `~/.noah/noah.db` (back it up first with `noah export`).

## Performance

Synthetic memories of ~900 characters on a 2-vCPU machine:

| Memories | Typical search | Inserts/s | Size | Natural-language questions answered |
|---:|---:|---:|---:|---:|
| 10,000 | 13–33 ms | ~900 | 26 MB | 50 / 50 |
| 100,000 | 94–280 ms | ~860 | 259 MB | 50 / 50 |

Method, comparisons and rejected experiments: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Known limits

- **Saving still depends on the agent.** The hooks remind it at the right moments, but no tool can decide by itself what is worth keeping.
- **Matches words, not meaning.** "What database did we choose?" will not find a note that only says "SQLite". Optional embeddings are planned.
- **A word that appears in almost every memory is slow at very large sizes** (about 280 ms at 100,000 memories). Normal queries are much faster.
- **No sync between machines.** Share with `noah export` and Git.
- **The `⚠` detector is a heuristic.** It looks for a short list of known injection phrases, download-and-run commands and hidden characters. It misses paraphrased attacks, and v0.4.0 raised false alarms on real notes before it was narrowed in v0.4.1. Treat it as a smoke alarm, not a guarantee.
- The numbers above come from synthetic data; results on real notes will vary.

## Development

```sh
go vet ./... && go test ./...
NOAH_RECALL_N=10000 go test ./internal/search -run TestRecall -v     # search-quality test
NOAH_SCALE=1000,10000 go test ./internal/search -run TestScale -v    # scale benchmark
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./cmd/noah           # cross-compile
```

## License

Apache License 2.0 — Copyright 2026 DICA POWER OÜ. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
