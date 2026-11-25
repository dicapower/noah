package importer

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/dicapower/noah/internal/memory"
	"github.com/dicapower/noah/internal/secrets"
	"github.com/dicapower/noah/internal/store"
	_ "modernc.org/sqlite"
)

func fakeEngram(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "engram.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE observations (id INTEGER PRIMARY KEY, session_id TEXT, type TEXT, title TEXT, content TEXT,
		project TEXT, topic_key TEXT, created_at TEXT, updated_at TEXT, deleted_at TEXT, pinned BOOLEAN DEFAULT 0, superseded_by_sync_id TEXT);
	INSERT INTO observations (type,title,content,project,topic_key,created_at,updated_at,pinned) VALUES
	 ('decision','Use SQLite','We chose SQLite with FTS5 for storage','My Project','Architecture/DB','2025-01-02 10:00:00','2025-01-03 10:00:00',1),
	 ('Bug Fix','Fix login','Login failed because the token expired','My Project','','2025-02-01 09:00:00','2025-02-01 09:00:00',0),
	 ('note','Deleted','gone','My Project','','2025-02-01 09:00:00','2025-02-01 09:00:00',0),
	 ('note','Old','old version','My Project','','2025-02-01 09:00:00','2025-02-01 09:00:00',0),
	 ('note','Key','aws key AKIAIOSFODNN7EXAMPLE and more text','Other','','2025-02-01 09:00:00','2025-02-01 09:00:00',0),
	 ('note','Fix login again','Login failed because the token expired','My Project','','2025-02-02 09:00:00','2025-02-02 09:00:00',0);
	UPDATE observations SET deleted_at='2025-03-01 00:00:00' WHERE title='Deleted';
	UPDATE observations SET superseded_by_sync_id='x' WHERE title='Old';`)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEngramImport(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "noah.db"), store.Options{SecretPolicy: secrets.Redact})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	src := fakeEngram(t)

	dry, err := Engram(ctx, st, src, Options{DryRun: true})
	if err != nil || dry.Imported != 5-1 {
		t.Fatalf("dry run: %+v %v", dry, err)
	}
	if sp, _ := st.Spaces(ctx); len(sp) != 0 {
		t.Fatalf("dry run wrote data: %v", sp)
	}

	rep, err := Engram(ctx, st, src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Read != 6 || rep.SkippedDeleted != 1 || rep.SkippedSuperseded != 1 || rep.Duplicate != 1 || rep.Imported != 3 || rep.Redacted != 1 {
		t.Fatalf("report: %+v", rep)
	}
	ms, err := st.List(ctx, "my-project", "", 10, 0)
	if err != nil || len(ms) != 2 {
		t.Fatalf("list: %v %v", ms, err)
	}
	var d *memory.Memory
	for i := range ms {
		if ms[i].Topic == "architecture/db" {
			d = &ms[i]
		}
	}
	if d == nil || !d.Pinned || d.Kind != "decision" || d.Trust != memory.TrustAgent || time.UnixMilli(d.UpdatedAt).UTC().Year() != 2025 {
		t.Fatalf("imported decision wrong: %+v", d)
	}

	// Running it again must not duplicate anything.
	again, err := Engram(ctx, st, src, Options{})
	if err != nil || again.Imported != 0 {
		t.Fatalf("second run: %+v %v", again, err)
	}
}
