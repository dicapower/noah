package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dicapower/noah/internal/memory"
)

// setup prints how to connect an agent. It never edits the user's agent
// configuration: that is the user's file.
func (c *cli) setup(args []string) error {
	agent := "claude-code"
	if len(args) > 0 {
		agent = strings.ToLower(args[0])
	}
	bin, _ := os.Executable()
	if bin == "" {
		bin = "noah"
	}
	switch agent {
	case "claude-code", "claude":
		fmt.Fprintf(c.out, `Claude Code

1) Register the MCP server (once):

   claude mcp add noah --scope user -- %s mcp

2) Optional: load recent memories at the start of every session. Add to ~/.claude/settings.json:

   {
     "hooks": {
       "SessionStart": [
         { "hooks": [ { "type": "command", "command": "%s hook session-start" } ] }
       ]
     }
   }
`, bin, bin)
	case "cursor":
		cfg, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{"noah": map[string]any{"command": bin, "args": []string{"mcp"}}}}, "   ", "  ")
		fmt.Fprintf(c.out, "Cursor\n\nAdd to ~/.cursor/mcp.json (or .cursor/mcp.json in a project):\n\n   %s\n", cfg)
	default:
		fmt.Fprintf(c.err, "unknown agent %q; use claude-code or cursor. Any MCP client works with the command: %s mcp\n", agent, bin)
		return errUsage
	}
	return nil
}

// hook implements agent hooks. It always returns 0 and keeps stdout clean so
// that a problem with memory can never break or pollute an agent session.
func (c *cli) hook(args []string) int {
	if len(args) != 1 || args[0] != "session-start" {
		fmt.Fprintln(c.err, "usage: noah hook session-start")
		return 0
	}
	var payload struct {
		Cwd string `json:"cwd"`
	}
	if b, err := io.ReadAll(io.LimitReader(c.in, 1<<20)); err == nil {
		_ = json.Unmarshal(b, &payload)
	}
	cwd := payload.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(c.err, "noah:", err)
		return 0
	}
	defer st.Close()
	space := memory.ResolveSpace(cwd)
	ms, err := st.Recent(context.Background(), space, 12)
	if err != nil {
		fmt.Fprintln(c.err, "noah:", err)
		return 0
	}
	text := memory.RenderContext(space, ms, 4000)
	if text == "" {
		return 0
	}
	_ = json.NewEncoder(c.out).Encode(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     "SessionStart",
		"additionalContext": text,
	}})
	return 0
}
