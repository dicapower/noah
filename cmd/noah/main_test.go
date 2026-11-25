package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type result struct {
	code     int
	out, err string
}

func runCLI(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	var o, e bytes.Buffer
	code := run(args, strings.NewReader(stdin), &o, &e)
	return result{code, o.String(), e.String()}
}

func env(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	proj := filepath.Join(dir, "my-proj")
	os.MkdirAll(filepath.Join(proj, ".git"), 0o755)
	home := filepath.Join(dir, "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("NOAH_DB", filepath.Join(dir, "db", "noah.db"))
	t.Setenv("NOAH_SPACE", "")
	t.Setenv("NOAH_SECRETS", "")
	wd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(wd) })
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	return proj
}

func TestSaveSearchForgetFlow(t *testing.T) {
	env(t)
	r := runCLI(t, "", "save", "--kind", "decision", "--topic", "db/choice", "We chose SQLite for local storage")
	if r.code != 0 || !strings.Contains(r.out, "saved #1 in my-proj") {
		t.Fatalf("save: %+v", r)
	}
	r = runCLI(t, "", "save", "--topic", "db/choice", "We chose SQLite with WAL for local storage")
	if r.code != 0 || !strings.Contains(r.out, "updated (revision 2)") {
		t.Fatalf("update: %+v", r)
	}
	r = runCLI(t, "", "search", "what did we choose for local storage?")
	if r.code != 0 || !strings.Contains(r.out, "#1 [decision · user") {
		t.Fatalf("search: %+v", r)
	}
	r = runCLI(t, "", "search", "--json", "storage")
	if r.code != 0 || !strings.HasPrefix(r.out, "[") {
		t.Fatalf("json search: %+v", r)
	}
	r = runCLI(t, "", "get", "#1")
	if !strings.Contains(r.out, "with WAL") || !strings.Contains(r.out, "revision 2") {
		t.Fatalf("get: %+v", r)
	}
	if r = runCLI(t, "", "forget", "1"); r.code != 0 {
		t.Fatalf("forget: %+v", r)
	}
	if r = runCLI(t, "", "get", "1"); r.code != 1 || !strings.Contains(r.err, "not found") {
		t.Fatalf("get after forget: %+v", r)
	}
}

func TestSaveFromStdinAndSecretWarning(t *testing.T) {
	env(t)
	key := "AKIA" + "ABCDEFGHIJKLMNOP"
	r := runCLI(t, "key is "+key+"\nmore", "save", "-")
	if r.code != 0 || !strings.Contains(r.err, "redacted") || strings.Contains(r.out, key) {
		t.Fatalf("stdin save: %+v", r)
	}
	t.Setenv("NOAH_SECRETS", "block")
	r = runCLI(t, "key is "+key, "save", "-")
	if r.code != 1 || !strings.Contains(r.err, "secret") {
		t.Fatalf("block policy: %+v", r)
	}
}

func TestDoctorAndSpaces(t *testing.T) {
	env(t)
	runCLI(t, "", "save", "something worth keeping")
	r := runCLI(t, "", "doctor")
	if r.code != 0 || !strings.Contains(r.out, "[ok  ] search index matches the data") {
		t.Fatalf("doctor: %+v", r)
	}
	r = runCLI(t, "", "spaces")
	if !strings.Contains(r.out, "* my-proj") {
		t.Fatalf("spaces: %+v", r)
	}
	if r = runCLI(t, "", "space"); strings.TrimSpace(r.out) != "my-proj" {
		t.Fatalf("space: %+v", r)
	}
}

func TestHookSessionStartIsQuietAndValid(t *testing.T) {
	proj := env(t)
	r0 := runCLI(t, cwdJSON(proj, ""), "hook", "session-start")
	if r0.code != 0 || !strings.Contains(r0.out, "mem_save") || strings.Contains(r0.out, "<noah-memory") {
		t.Fatalf("empty store must still print the protocol, without a memory block: %+v", r0)
	}
	if r := runCLI(t, cwdJSON(proj, "compact"), "hook", "session-start"); !strings.Contains(r.out, "compacted") {
		t.Fatalf("compact source must say so: %+v", r)
	}
	runCLI(t, "", "save", "Remember the deploy checklist")
	r := runCLI(t, cwdJSON(proj, ""), "hook", "session-start")
	if r.code != 0 || !strings.Contains(r.out, `"hookEventName":"SessionStart"`) || !strings.Contains(r.out, "deploy checklist") {
		t.Fatalf("hook output: %+v", r)
	}
	// a broken database must not fail the agent session
	blocker := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOAH_DB", filepath.Join(blocker, "noah.db")) // parent is a file: cannot be created, on any OS
	if r := runCLI(t, `{}`, "hook", "session-start"); r.code != 0 || r.out != "" {
		t.Fatalf("hook must swallow errors: %+v", r)
	}
}

func TestUsageAndUnknown(t *testing.T) {
	env(t)
	if r := runCLI(t, ""); r.code != 2 {
		t.Fatalf("no args => 2, got %+v", r)
	}
	if r := runCLI(t, "", "nope"); r.code != 2 || !strings.Contains(r.err, "unknown command") {
		t.Fatalf("unknown: %+v", r)
	}
	if r := runCLI(t, "", "version"); r.code != 0 || !strings.HasPrefix(r.out, "noah ") {
		t.Fatalf("version: %+v", r)
	}
	if r := runCLI(t, "", "setup", "cursor"); r.code != 0 || !strings.Contains(r.out, `"mcpServers"`) {
		t.Fatalf("setup cursor: %+v", r)
	}
}

func TestHookStopBlocksOnceThenAllows(t *testing.T) {
	env(t)
	dir := t.TempDir()
	tr := filepath.Join(dir, "t.jsonl")
	var b strings.Builder
	for i := 0; i < 12; i++ {
		b.WriteString(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash"}]}}` + "\n")
	}
	if err := os.WriteFile(tr, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	pj, _ := json.Marshal(map[string]string{"session_id": "sess-1", "transcript_path": tr}) // escapes Windows backslashes
	payload := string(pj)
	r := runCLI(t, payload, "hook", "stop")
	if r.code != 0 || !strings.Contains(r.out, `"decision":"block"`) || !strings.Contains(r.out, "mem_save") {
		t.Fatalf("first stop should ask to save: %+v", r)
	}
	if r := runCLI(t, payload, "hook", "stop"); r.code != 0 || r.out != "" {
		t.Fatalf("second stop must stay silent: %+v", r)
	}
	if r := runCLI(t, `not json`, "hook", "stop"); r.code != 0 || r.out != "" {
		t.Fatalf("garbage input must be swallowed: %+v", r)
	}
}

func TestGlobalMemoryAndMove(t *testing.T) {
	proj := env(t)
	_ = proj
	runCLI(t, "", "save", "--space", "global", "--title", "Language", "Always answer in Spanish from Spain")
	if r := runCLI(t, "", "search", "which language do we answer in"); !strings.Contains(r.out, "global") || !strings.Contains(r.out, "Spanish from Spain") {
		t.Fatalf("project search must include global: %+v", r)
	}
	if r := runCLI(t, "", "context"); !strings.Contains(r.out, `space="global"`) {
		t.Fatalf("context must include global: %+v", r)
	}
	runCLI(t, "", "save", "--space", "someproj", "--title", "Hosting", "Everything runs on Dokploy")
	r := runCLI(t, "", "search", "--all", "Dokploy")
	if !strings.Contains(r.out, "someproj") {
		t.Fatalf("setup failed: %+v", r)
	}
	id := strings.TrimPrefix(strings.Fields(strings.SplitN(r.out, "\n", 3)[1])[0], "#")
	if m := runCLI(t, "", "move", id, "global"); m.code != 0 || !strings.Contains(m.out, "now in global") {
		t.Fatalf("move: %+v", m)
	}
	if r := runCLI(t, "", "search", "Dokploy"); !strings.Contains(r.out, "global") {
		t.Fatalf("moved memory must show up as global: %+v", r)
	}
	if m := runCLI(t, "", "move", "x"); m.code != 2 {
		t.Fatalf("bad usage must return 2: %+v", m)
	}
}

// cwdJSON builds a hook payload; json.Marshal escapes the backslashes of Windows paths.
func cwdJSON(cwd, source string) string {
	m := map[string]string{"cwd": cwd}
	if source != "" {
		m["source"] = source
	}
	b, _ := json.Marshal(m)
	return string(b)
}
