package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dicapower/noah/internal/memory"
)

func line(names ...string) string {
	var blocks []string
	for _, n := range names {
		blocks = append(blocks, fmt.Sprintf(`{"type":"tool_use","name":%q,"input":{}}`, n))
	}
	return `{"type":"assistant","message":{"role":"assistant","content":[` + strings.Join(blocks, ",") + `]}}` + "\n"
}

func transcript(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.jsonl")
	body := `{"type":"user","message":{"role":"user","content":"hello"}}` + "\n"
	body += strings.Join(lines, "")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func many(n int, name string) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(line(name))
	}
	return b.String()
}

func TestStartContext(t *testing.T) {
	if got := StartContext("startup", "app", nil, nil, 4000); !strings.Contains(got, "mem_save") || strings.Contains(got, "<noah-memory") {
		t.Fatalf("no memories: protocol only, got %q", got)
	}
	ms := []memory.Memory{{ID: 1, Title: "Use SQLite", Content: "Use SQLite for storage", Kind: "decision", Trust: memory.TrustUser, UpdatedAt: time.Now().UnixMilli()}}
	got := StartContext("compact", "app", ms, nil, 4000)
	for _, want := range []string{"mem_save", "compacted", "<noah-memory", "Use SQLite"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if i, j := strings.Index(got, "mem_save"), strings.Index(got, "<noah-memory"); i > j {
		t.Error("trusted protocol must come before the untrusted block")
	}
}

func TestStopNudgesOnceAfterSubstantialWork(t *testing.T) {
	state := t.TempDir()
	tr := transcript(t, many(12, "Bash"))
	in := StopInput{SessionID: "s1", TranscriptPath: tr}
	now := time.Now()
	if StopReason(in, state, 0, now) == "" {
		t.Fatal("12 tool calls and no save: must ask")
	}
	if StopReason(in, state, 0, now) != "" {
		t.Fatal("must ask only once per session")
	}
	if StopReason(StopInput{SessionID: "s2", TranscriptPath: tr}, state, 0, now) == "" {
		t.Fatal("another session is asked independently")
	}
}

func TestStopStaysQuietWhenNotNeeded(t *testing.T) {
	state := t.TempDir()
	now := time.Now()
	cases := map[string]StopInput{
		"little work":       {SessionID: "a", TranscriptPath: transcript(t, many(3, "Bash"))},
		"loop guard":        {SessionID: "b", TranscriptPath: transcript(t, many(20, "Bash")), StopHookActive: true},
		"already saved":     {SessionID: "c", TranscriptPath: transcript(t, many(20, "Bash"), line("mcp__noah__mem_save"), many(2, "Edit"))},
		"bad session id":    {SessionID: "../x", TranscriptPath: transcript(t, many(20, "Bash"))},
		"missing file":      {SessionID: "d", TranscriptPath: "/nonexistent/t.jsonl"},
		"memory reads only": {SessionID: "e", TranscriptPath: transcript(t, many(30, "mcp__noah__mem_search"))},
	}
	for name, in := range cases {
		if r := StopReason(in, state, 0, now); r != "" {
			t.Errorf("%s: want silence, got %q", name, r)
		}
	}
}

func TestStopCountsOnlyWorkAfterTheLastSave(t *testing.T) {
	state := t.TempDir()
	tr := transcript(t, many(15, "Bash"), line("mcp__noah__mem_save"), many(11, "Edit"))
	if StopReason(StopInput{SessionID: "x", TranscriptPath: tr}, state, 0, time.Now()) == "" {
		t.Fatal("11 calls after the last save: must ask")
	}
}

func TestPruneRemovesOldMarkers(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old")
	if err := os.WriteFile(old, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	prune(dir, time.Now())
	if _, err := os.Stat(old); err == nil {
		t.Fatal("old marker should be removed")
	}
}

func TestStartContextIncludesGlobalMemories(t *testing.T) {
	g := []memory.Memory{{ID: 9, Title: "Prefers Spanish", Content: "Answer in Spanish from Spain", Kind: "note", Trust: memory.TrustUser, UpdatedAt: time.Now().UnixMilli()}}
	got := StartContext("startup", "app", nil, g, 4000)
	if !strings.Contains(got, `space="global"`) || !strings.Contains(got, "Spanish from Spain") {
		t.Fatalf("global memories missing: %q", got)
	}
	if strings.Contains(StartContext("startup", memory.Global, nil, g, 4000), `<noah-memory space="global">`) {
		t.Fatal("inside the global space the global block must not be repeated")
	}
}
