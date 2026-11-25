// Command noah is persistent memory for AI coding agents.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is set at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const usage = `noah — persistent memory for AI coding agents

Usage: noah <command> [flags]

Agent integration
  mcp                  run the MCP server on stdio (what your agent launches)
  setup [agent]        print how to connect Claude Code or Cursor
  hook session-start   Claude Code SessionStart hook: inject recent memories

Memories
  save [text|-]        save a memory (text, or - / piped stdin)
  search <question>    search memories
  context              show pinned and recent memories
  get <id>             show one memory
  forget <id>          delete a memory
  spaces               list project spaces
  space                print the space for the current directory

Maintenance
  doctor               check the database and look for stored secrets
  export               write memories as JSON lines
  version              print the version

Environment
  NOAH_DB        database file           (default ~/.noah/noah.db)
  NOAH_SPACE     force the project space (default: from .noah file, git root or folder)
  NOAH_SECRETS   redact | block | off    (default redact)

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
	case "doctor":
		return c.doctor(rest)
	case "export":
		err = c.export(rest)
	case "version", "--version", "-v":
		fmt.Fprintln(out, "noah", version)
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
