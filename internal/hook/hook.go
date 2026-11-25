// Package hook holds the logic behind the agent hooks (Claude Code today).
//
// Hooks must never break or pollute an agent session, so everything here is
// best effort and returns an empty result on any problem.
package hook

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dicapower/noah/internal/memory"
)

// Protocol is noah's own instruction to the agent. It is trusted text written
// by noah, kept outside the <noah-memory> block that holds untrusted notes.
const Protocol = `noah is this project's persistent memory (tools mem_search and mem_save).
Search it before non-trivial work. Save with mem_save when you make a decision (include why), fix a bug (include the root cause), learn a convention or gotcha, or finish a significant piece of work. Use a stable "topic" so a recurring fact is updated, not duplicated. Facts that apply to every project (how the user works, shared infrastructure, general conventions) go in space "global". Never save secrets or routine chatter.`

const afterCompact = `The conversation was just compacted. If something important from before is missing and not listed below, search noah before continuing.`

// StartContext builds the SessionStart context: the protocol, then the recent
// memories of the space. source is Claude Code's SessionStart source
// (startup, resume, clear, compact). It is never empty.
func StartContext(source, space string, ms, global []memory.Memory, maxChars int) string {
	var b strings.Builder
	b.WriteString(Protocol)
	b.WriteString("\n")
	if source == "compact" {
		b.WriteString(afterCompact)
		b.WriteString("\n")
	}
	if len(ms) > 0 {
		b.WriteString("\n")
		b.WriteString(memory.RenderContext(space, ms, maxChars))
	}
	if len(global) > 0 && space != memory.Global {
		b.WriteString("\n")
		b.WriteString(memory.RenderContext(memory.Global, global, maxChars/2))
	}
	return b.String()
}

// StopInput is the part of Claude Code's Stop hook payload noah uses.
type StopInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	StopHookActive bool   `json:"stop_hook_active"`
}

// DefaultMinTools is how many tool calls since the last noah save count as
// "substantial work".
const DefaultMinTools = 10

// StopReason decides whether to ask the agent to save before it stops. It
// returns the reason text, or "" to let the agent stop. It nudges at most once
// per session, only after substantial work with nothing saved to noah since.
// stateDir keeps one small marker file per nudged session.
func StopReason(in StopInput, stateDir string, minTools int, now time.Time) string {
	if in.StopHookActive || in.TranscriptPath == "" || !safeID.MatchString(in.SessionID) {
		return ""
	}
	if minTools <= 0 {
		minTools = DefaultMinTools
	}
	marker := filepath.Join(stateDir, in.SessionID)
	if _, err := os.Stat(marker); err == nil {
		return ""
	}
	tools, err := toolsSinceSave(in.TranscriptPath)
	if err != nil || tools < minTools {
		return ""
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return ""
	}
	if err := os.WriteFile(marker, []byte(now.UTC().Format(time.RFC3339)), 0o600); err != nil {
		return "" // cannot remember we asked: better silent than looping
	}
	prune(stateDir, now)
	return "noah: before you finish, save any durable knowledge from this session with mem_save " +
		"(decisions and why, root causes of bugs, conventions, gotchas). " +
		"If there is nothing worth keeping, just answer \"nothing to save\"."
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// toolsSinceSave counts tool calls in a Claude Code transcript (JSON lines)
// that happened after the last noah mem_save. Calls to other mem_* tools do
// not count as work.
func toolsSinceSave(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	n := 0
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			n = countLine(line, n)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return n, nil
			}
			return n, err
		}
	}
}

func countLine(line []byte, n int) int {
	var rec struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &rec) != nil || len(rec.Message.Content) == 0 || rec.Message.Content[0] != '[' {
		return n
	}
	var blocks []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(rec.Message.Content, &blocks) != nil {
		return n
	}
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		switch {
		case isNoahSave(b.Name):
			n = 0
		case strings.Contains(b.Name, "mem_"):
			// reading or managing memory is not work
		default:
			n++
		}
	}
	return n
}

func isNoahSave(name string) bool {
	return strings.Contains(name, "noah") && strings.HasSuffix(name, "mem_save")
}

// prune removes markers older than 30 days.
func prune(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > 30*24*time.Hour {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
