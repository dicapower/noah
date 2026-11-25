// Package importer brings memories from other tools into noah.
package importer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dicapower/noah/internal/memory"
	"github.com/dicapower/noah/internal/store"
)

// Options controls an Engram import.
type Options struct {
	Trust   memory.Trust // trust given to imported memories (default agent)
	DryRun  bool
	Project string // import only this Engram project (after normalising); empty = all
	Log     io.Writer
}

// Report counts what happened to each Engram observation.
type Report struct {
	Read, Imported, Updated, Duplicate int
	SkippedDeleted, SkippedSuperseded  int
	SkippedSecret, SkippedInvalid      int
	Redacted                           int
	Spaces                             map[string]int
}

var (
	badKind  = regexp.MustCompile(`[^a-z0-9_-]+`)
	badTopic = regexp.MustCompile(`[^a-z0-9/_.:-]+`)
)

// Engram imports observations from an Engram SQLite database.
//
// The database is copied (with its -wal and -shm files) to a temporary
// directory first, so the original is never opened and a running Engram is
// not disturbed. Soft-deleted and superseded observations are skipped.
// Observations become memories in a space named after their Engram project;
// topic_key, type, pinned and original dates are kept.
func Engram(ctx context.Context, st *store.Store, dbPath string, opt Options) (Report, error) {
	rep := Report{Spaces: map[string]int{}}
	if opt.Trust == "" {
		opt.Trust = memory.TrustAgent
	}
	tmp, err := os.MkdirTemp("", "noah-engram-*")
	if err != nil {
		return rep, err
	}
	defer os.RemoveAll(tmp)
	copied := filepath.Join(tmp, "engram.db")
	if err := copyFile(dbPath, copied); err != nil {
		return rep, fmt.Errorf("reading %s: %w", dbPath, err)
	}
	for _, ext := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + ext); err == nil {
			if err := copyFile(dbPath+ext, copied+ext); err != nil {
				return rep, err
			}
		}
	}
	db, err := store.OpenReadOnlyDB(copied)
	if err != nil {
		return rep, err
	}
	defer db.Close()
	// A copy with a WAL needs a writable open to be recovered; fall back below if ro fails.
	rows, err := db.QueryContext(ctx, `SELECT COALESCE(project,''), type, title, content,
		COALESCE(topic_key,''), pinned, created_at, updated_at, deleted_at, superseded_by_sync_id
		FROM observations ORDER BY updated_at, id`)
	if err != nil {
		return rep, fmt.Errorf("this does not look like an Engram database: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var project, typ, title, content, topic, created, updated string
		var pinned bool
		var deleted, superseded *string
		if err := rows.Scan(&project, &typ, &title, &content, &topic, &pinned, &created, &updated, &deleted, &superseded); err != nil {
			return rep, err
		}
		rep.Read++
		if deleted != nil && *deleted != "" {
			rep.SkippedDeleted++
			continue
		}
		if superseded != nil && *superseded != "" {
			rep.SkippedSuperseded++
			continue
		}
		space := memory.Unscoped
		if strings.TrimSpace(project) != "" {
			if sp, err := memory.NormalizeSpace(project); err == nil {
				space = sp
			}
		}
		if opt.Project != "" {
			want, _ := memory.NormalizeSpace(opt.Project)
			if space != want {
				continue
			}
		}
		in := memory.Input{
			Space: space, Title: strings.TrimSpace(title), Content: content,
			Kind: kindOf(typ), Topic: topicOf(topic), Trust: opt.Trust, Pinned: pinned,
			CreatedAt: parseTime(created), UpdatedAt: parseTime(updated),
		}
		if opt.DryRun {
			if _, err := in.Clean(); err != nil {
				rep.SkippedInvalid++
				continue
			}
			rep.Imported++
			rep.Spaces[space]++
			continue
		}
		res, err := st.Save(ctx, in)
		switch {
		case errors.Is(err, store.ErrSecret):
			rep.SkippedSecret++
			continue
		case errors.Is(err, store.ErrConflict):
			rep.Duplicate++
			continue
		case err != nil:
			rep.SkippedInvalid++
			if opt.Log != nil {
				fmt.Fprintf(opt.Log, "skipped %q: %v\n", memory.Sanitize(title, 60), err)
			}
			continue
		}
		rep.Redacted += res.Redactions
		switch {
		case res.Duplicate:
			rep.Duplicate++
		case res.Updated:
			rep.Updated++
		default:
			rep.Imported++
			rep.Spaces[space]++
		}
	}
	return rep, rows.Err()
}

func kindOf(t string) string {
	k := badKind.ReplaceAllString(strings.ToLower(strings.TrimSpace(t)), "-")
	k = strings.Trim(k, "-_")
	if k == "" || k[0] < 'a' || k[0] > 'z' {
		return ""
	}
	if len(k) > 32 {
		k = k[:32]
	}
	return k
}

func topicOf(t string) string {
	t = badTopic.ReplaceAllString(strings.ToLower(strings.TrimSpace(t)), "-")
	t = strings.Trim(t, "-/_.:")
	if len(t) > memory.MaxTopic {
		t = t[:memory.MaxTopic]
	}
	return t
}

func parseTime(s string) time.Time {
	for _, l := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(l, s, time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
