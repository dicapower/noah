// Command noah is persistent memory for AI coding agents.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

// version is set at build time: -ldflags "-X main.version=v0.1.0".
// Without it (a plain `go build` or `go install ...@vX.Y.Z`) Go's own build
// information is used, so the version is still right after an upgrade.
var version = "dev"

// currentVersion returns the version to print.
func currentVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

const usage = `noah — persistent memory for AI coding agents

Usage: noah <command> [flags]

Agent integration
  mcp                  run the MCP server on stdio (what your agent launches)
  setup [agent]        print how to connect Claude Code or Cursor
  hook session-start   Claude Code hook: memory protocol + recent memories
  hook stop            Claude Code hook: ask once to save after substantial work

Memories
  save [text|-]        save a memory (text, or - / piped stdin)
  search <question>    search memories
  context              show pinned and recent memories
  get <id>             show one memory
  forget <id>          delete a memory
  move <id> <space>    move a memory to another project (global = every project)
  spaces               list project spaces
  space                print the space for the current directory

Maintenance
  stale                list notes nobody has used for months and notes flagged as suspicious
  doctor               check the database and look for stored secrets
  export               write memories as JSON lines
  import engram [db]   copy your memories from Engram (default ~/.engram/engram.db)
  version              print the version

Environment
  NOAH_DB        database file           (default ~/.noah/noah.db)
  NOAH_SPACE     force the project space (default: from .noah file, git root or folder)
  NOAH_SECRETS   redact | block | off    (default redact)
  NOAH_START_TOKENS  size cap of the session-start context (default 1500)

Run "noah <command> -h" for the flags of a command.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, in io.Reader, out, errw io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(errw, usage)
		return 2
	}
	c := &cli{in: in, out: out, err: errw}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "mcp":
		err = c.mcp(rest)
	case "setup":
		err = c.setup(rest)
	case "hook":
		return c.hook(rest) // hooks never fail the agent session
	case "save":
		err = c.save(rest)
	case "search":
		err = c.search(rest)
	case "context":
		err = c.context(rest)
	case "get":
		err = c.get(rest)
	case "forget":
		err = c.forget(rest)
	case "spaces":
		err = c.spaces(rest)
	case "space":
		err = c.space(rest)
	case "stale":
		err = c.stale(rest)
	case "doctor":
		return c.doctor(rest)
	case "export":
		err = c.export(rest)
	case "move":
		err = c.move(rest)
	case "import":
		err = c.importCmd(rest)
	case "version", "--version", "-v":
		fmt.Fprintln(out, "noah", currentVersion())
	case "help", "--help", "-h":
		fmt.Fprint(out, usage)
	default:
		fmt.Fprintf(errw, "noah: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
	if err != nil {
		if err == errUsage {
			return 2
		}
		fmt.Fprintln(errw, "noah:", err)
		return 1
	}
	return 0
}
