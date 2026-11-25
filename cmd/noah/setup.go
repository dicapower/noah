package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dicapower/noah/internal/hook"
	"github.com/dicapower/noah/internal/store"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

2) Recommended: load the memory protocol and recent memories whenever a session starts,
   resumes, is cleared or is compacted. Add to ~/.claude/settings.json:

   {
     "hooks": {
       "SessionStart": [
         { "hooks": [ { "type": "command", "command": "%s hook session-start" } ] }
       ]
     }
   }

3) Optional: after substantial work with nothing saved, ask the agent once per session to
   save what it learned. Add inside "hooks" next to SessionStart:

       "Stop": [
         { "hooks": [ { "type": "command", "command": "%s hook stop" } ] }
       ]
`, bin, bin, bin)
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
	if len(args) != 1 || (args[0] != "session-start" && args[0] != "stop") {
		fmt.Fprintln(c.err, "usage: noah hook session-start | stop")
		return 0
	}
	raw, _ := io.ReadAll(io.LimitReader(c.in, 1<<20))
	if args[0] == "stop" {
		return c.hookStop(raw)
	}
	var payload struct {
		Cwd    string `json:"cwd"`
		Source string `json:"source"`
	}
	_ = json.Unmarshal(raw, &payload)
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
	glob, _ := st.Recent(context.Background(), memory.Global, 6)
	_ = json.NewEncoder(c.out).Encode(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     "SessionStart",
		"additionalContext": hook.StartContext(payload.Source, space, ms, glob, 4000),
	}})
	return 0
}

// hookStop asks the agent, once per session and only after substantial work
// with nothing saved to noah, to save what it learned before it stops.
func (c *cli) hookStop(raw []byte) int {
	var in hook.StopInput
	if json.Unmarshal(raw, &in) != nil {
		return 0
	}
	dbPath, err := store.DefaultPath()
	if err != nil {
		return 0
	}
	min := hook.DefaultMinTools
	if v, err := strconv.Atoi(os.Getenv("NOAH_STOP_MIN_TOOLS")); err == nil && v > 0 {
		min = v
	}
	reason := hook.StopReason(in, filepath.Join(filepath.Dir(dbPath), "hooks"), min, time.Now())
	if reason == "" {
		return 0
	}
	_ = json.NewEncoder(c.out).Encode(map[string]any{"decision": "block", "reason": reason})
	return 0
}
