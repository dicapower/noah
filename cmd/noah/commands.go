package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/dicapower/noah/internal/importer"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/dicapower/noah/internal/mcpserver"
	"github.com/dicapower/noah/internal/memory"
	"github.com/dicapower/noah/internal/search"
	"github.com/dicapower/noah/internal/secrets"
	"github.com/dicapower/noah/internal/store"
)

var errUsage = errors.New("usage")

type cli struct {
	in       io.Reader
	out, err io.Writer
}

func (c *cli) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("noah "+name, flag.ContinueOnError)
	fs.SetOutput(c.err)
	return fs
}

// parse parses flags that may appear before or after positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, errUsage
			}
			return nil, errUsage
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func openStore() (*store.Store, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return nil, err
	}
	return store.Open(path, store.Options{SecretPolicy: secrets.ParsePolicy(os.Getenv("NOAH_SECRETS"))})
}

func currentSpace(override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return memory.NormalizeSpace(override)
	}
	cwd, _ := os.Getwd()
	return memory.ResolveSpace(cwd), nil
}

func (c *cli) mcp(args []string) error {
	fs := c.flags("mcp")
	space := fs.String("space", "", "project space (default: from the current directory)")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	sp, err := currentSpace(*space)
	if err != nil {
		return err
	}
	return server.ServeStdio(mcpserver.New(mcpserver.Config{Store: st, Space: sp, Version: currentVersion()}))
}

func (c *cli) save(args []string) error {
	fs := c.flags("save")
	space := fs.String("space", "", "project space")
	title := fs.String("title", "", "short title")
	kind := fs.String("kind", "", "decision, bugfix, pattern, note, ...")
	topic := fs.String("topic", "", "stable key; saving again with it revises the memory")
	pin := fs.Bool("pin", false, "always include in context")
	var tags multi
	fs.Var(&tags, "tag", "tag (repeatable)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	content := strings.Join(pos, " ")
	if content == "-" || (content == "" && !isTerminal(os.Stdin)) {
		b, err := io.ReadAll(io.LimitReader(c.in, memory.MaxContent+1))
		if err != nil {
			return err
		}
		content = string(b)
	}
	sp, err := currentSpace(*space)
	if err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	res, err := st.Save(context.Background(), memory.Input{
		Space: sp, Title: *title, Content: content, Kind: *kind, Topic: *topic,
		Tags: tags, Trust: memory.TrustUser, Pinned: *pin,
	})
	if err != nil {
		return err
	}
	verb := "saved"
	switch {
	case res.Updated:
		verb = fmt.Sprintf("updated (revision %d)", res.Memory.Revision)
	case res.Duplicate:
		verb = "already saved"
	}
	fmt.Fprintf(c.out, "%s #%d in %s: %s\n", verb, res.Memory.ID, res.Memory.Space, memory.Sanitize(res.Memory.Title, 100))
	if res.Redactions > 0 {
		fmt.Fprintf(c.err, "noah: %d possible secret(s) were redacted before saving\n", res.Redactions)
	}
	return nil
}

func (c *cli) search(args []string) error {
	fs := c.flags("search")
	space := fs.String("space", "", "project space")
	all := fs.Bool("all", false, "search every space")
	kind := fs.String("kind", "", "only this kind")
	limit := fs.Int("limit", 8, "results")
	asJSON := fs.Bool("json", false, "JSON output")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		fmt.Fprintln(c.err, "usage: noah search [flags] <question>")
		return errUsage
	}
	sp, err := currentSpace(*space)
	if err != nil {
		return err
	}
	if *all {
		sp = ""
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	hits, err := search.Run(context.Background(), st, strings.Join(pos, " "), search.Options{Space: sp, Kind: *kind, Limit: *limit, Global: true})
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(c.out).Encode(hits)
	}
	fmt.Fprint(c.out, mcpserver.FormatHits(hits, mcpserver.ShowSpace(hits, sp)))
	return nil
}

func (c *cli) context(args []string) error {
	fs := c.flags("context")
	space := fs.String("space", "", "project space")
	limit := fs.Int("limit", 12, "memories")
	maxTokens := fs.Int("max-tokens", mcpserver.DefaultMaxTokens, "cap the output at about this many tokens")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	sp, err := currentSpace(*space)
	if err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	out, err := mcpserver.BuildContext(context.Background(), st, sp, *limit, *maxTokens)
	if err != nil {
		return err
	}
	if out != "" {
		fmt.Fprint(c.out, out)
	} else {
		fmt.Fprintf(c.out, "No memories saved yet for %q.\n", sp)
	}
	return nil
}

func idArg(fs *flag.FlagSet, args []string, errw io.Writer, name string) (int64, error) {
	pos, err := parse(fs, args)
	if err != nil {
		return 0, err
	}
	if len(pos) != 1 {
		fmt.Fprintf(errw, "usage: noah %s <id>\n", name)
		return 0, errUsage
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(pos[0], "#"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a memory id", pos[0])
	}
	return id, nil
}

func (c *cli) get(args []string) error {
	fs := c.flags("get")
	space := fs.String("space", "", "restrict to a space (default: any)")
	id, err := idArg(fs, args, c.err, "get")
	if err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	sp := ""
	if *space != "" {
		if sp, err = currentSpace(*space); err != nil {
			return err
		}
	}
	m, err := st.Get(context.Background(), sp, id)
	if err != nil {
		return err
	}
	fmt.Fprint(c.out, mcpserver.FormatMemory(m))
	return nil
}

func (c *cli) forget(args []string) error {
	fs := c.flags("forget")
	id, err := idArg(fs, args, c.err, "forget")
	if err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Forget(context.Background(), "", id); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "deleted #%d\n", id)
	return nil
}

func (c *cli) spaces(args []string) error {
	if _, err := parse(c.flags("spaces"), args); err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	list, err := st.Spaces(context.Background())
	if err != nil {
		return err
	}
	cur, _ := currentSpace("")
	for _, s := range list {
		mark := " "
		if s.Name == cur {
			mark = "*"
		}
		fmt.Fprintf(c.out, "%s %-30s %5d memories  updated %s\n", mark, s.Name, s.Count, time.UnixMilli(s.Updated).UTC().Format("2006-01-02"))
	}
	if len(list) == 0 {
		fmt.Fprintln(c.out, "no memories yet")
	}
	return nil
}

func (c *cli) space(args []string) error {
	if _, err := parse(c.flags("space"), args); err != nil {
		return err
	}
	sp, err := currentSpace("")
	if err != nil {
		return err
	}
	fmt.Fprintln(c.out, sp)
	return nil
}

func (c *cli) export(args []string) error {
	fs := c.flags("export")
	space := fs.String("space", "", "only this space (default: all)")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	sp := ""
	if *space != "" {
		var err error
		if sp, err = currentSpace(*space); err != nil {
			return err
		}
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	enc := json.NewEncoder(c.out)
	return st.Each(context.Background(), sp, func(m memory.Memory) error { return enc.Encode(m) })
}

func (c *cli) doctor(args []string) int {
	if _, err := parse(c.flags("doctor"), args); err != nil {
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(c.err, "noah:", err)
		return 1
	}
	defer st.Close()
	r, err := st.Check(context.Background())
	if err != nil {
		fmt.Fprintln(c.err, "noah:", err)
		return 1
	}
	problems := 0
	line := func(ok bool, format string, a ...any) {
		mark := "ok  "
		if !ok {
			mark = "FAIL"
			problems++
		}
		fmt.Fprintf(c.out, "[%s] %s\n", mark, fmt.Sprintf(format, a...))
	}
	fmt.Fprintf(c.out, "database  %s (%.1f MB, schema v%d, %s)\n", r.Path, float64(r.SizeBytes)/1e6, r.SchemaVersion, r.JournalMode)
	fmt.Fprintf(c.out, "contents  %d memories in %d spaces\n\n", r.Memories, r.Spaces)
	line(r.IntegrityOK, "database integrity %s", r.IntegrityMsg)
	line(r.IndexOK, "search index matches the data %s", r.IndexMsg)
	line(r.JournalMode == "wal", "WAL mode (readers do not block the writer)")
	line(r.WithSecrets == 0, "no stored memory looks like it contains a credential (%d found: ids %v)", r.WithSecrets, r.SecretMemories)
	if r.Unscoped > 0 {
		fmt.Fprintf(c.out, "[info] %d memories are in %q: they were saved outside any project\n", r.Unscoped, memory.Unscoped)
	}
	if problems > 0 {
		return 1
	}
	return 0
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func (c *cli) importCmd(args []string) error {
	fs := c.flags("import")
	dry := fs.Bool("dry-run", false, "show what would be imported without writing")
	project := fs.String("project", "", "import only this Engram project")
	trust := fs.String("trust", "agent", "trust for imported memories: agent or user")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 || pos[0] != "engram" || len(pos) > 2 {
		fmt.Fprintln(c.err, "usage: noah import engram [path/to/engram.db] [--dry-run] [--project name] [--trust agent|user]")
		return errUsage
	}
	var tr memory.Trust
	switch *trust {
	case "agent":
		tr = memory.TrustAgent
	case "user":
		tr = memory.TrustUser
	default:
		return fmt.Errorf("--trust must be agent or user")
	}
	path := ""
	if len(pos) == 2 {
		path = pos[1]
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path = filepath.Join(home, ".engram", "engram.db")
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	rep, err := importer.Engram(context.Background(), st, path, importer.Options{Trust: tr, DryRun: *dry, Project: *project, Log: c.err})
	if err != nil {
		return err
	}
	prefix := ""
	if *dry {
		prefix = "(dry run, nothing written) "
	}
	fmt.Fprintf(c.out, "%sread %d observations: imported %d, revised %d, already present %d\n",
		prefix, rep.Read, rep.Imported, rep.Updated, rep.Duplicate)
	fmt.Fprintf(c.out, "skipped: %d deleted, %d superseded, %d blocked by secret policy, %d invalid\n",
		rep.SkippedDeleted, rep.SkippedSuperseded, rep.SkippedSecret, rep.SkippedInvalid)
	if rep.Redacted > 0 {
		fmt.Fprintf(c.out, "%d possible secret(s) were redacted while importing\n", rep.Redacted)
	}
	names := make([]string, 0, len(rep.Spaces))
	for k := range rep.Spaces {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Fprintf(c.out, "  %-32s %d\n", k, rep.Spaces[k])
	}
	return nil
}

func (c *cli) move(args []string) error {
	fs := c.flags("move")
	pos, err := parse(fs, args)
	if err != nil || len(pos) != 2 {
		fmt.Fprintln(c.err, "usage: noah move <id> <space>   (use \"global\" for knowledge that applies to every project)")
		return errUsage
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(pos[0], "#"), 10, 64)
	if err != nil {
		return fmt.Errorf("%q is not a memory id", pos[0])
	}
	sp, err := memory.NormalizeSpace(pos[1])
	if err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	m, err := st.Move(context.Background(), id, sp)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "#%d is now in %s\n", m.ID, m.Space)
	return nil
}

// stale lists what deserves a human review: memories nobody has used for a
// long time (candidates to delete or rewrite) and memories flagged as
// suspicious (never replayed into sessions automatically, but still stored).
func (c *cli) stale(args []string) error {
	fs := c.flags("stale")
	space := fs.String("space", "", "only this space (default: all)")
	days := fs.Int("days", 180, "not used or revised for this many days")
	limit := fs.Int("limit", 30, "most entries per list")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	sp := ""
	if *space != "" {
		var err error
		if sp, err = currentSpace(*space); err != nil {
			return err
		}
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	old, err := st.Stale(ctx, sp, time.Now().AddDate(0, 0, -*days), *limit)
	if err != nil {
		return err
	}
	flagged, err := st.Flagged(ctx, sp, *limit)
	if err != nil {
		return err
	}
	line := func(m memory.Memory) {
		seen := m.UpdatedAt
		if m.LastUsedAt > seen {
			seen = m.LastUsedAt
		}
		fmt.Fprintf(c.out, "  #%-5d %-24s %-10s %-8s last seen %s  %s\n", m.ID, m.Space, m.Kind, m.Trust,
			time.UnixMilli(seen).UTC().Format("2006-01-02"), memory.Sanitize(m.Title, 70))
	}
	fmt.Fprintf(c.out, "Not used or revised in %d days (%d shown; pinned notes are never listed):\n", *days, len(old))
	for _, m := range old {
		line(m)
	}
	if len(old) == 0 {
		fmt.Fprintln(c.out, "  none")
	}
	fmt.Fprintf(c.out, "\nFlagged as suspicious (%d shown; they are not injected automatically unless you wrote them):\n", len(flagged))
	for _, m := range flagged {
		line(m)
		fmt.Fprintf(c.out, "         ⚠ %s\n", strings.Join(m.Flags, ", "))
	}
	if len(flagged) == 0 {
		fmt.Fprintln(c.out, "  none")
	}
	fmt.Fprintln(c.out, "\nReview with: noah get <id>   ·   remove with: noah forget <id>")
	return nil
}
