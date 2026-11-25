package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dicapower/noah/internal/memory"
)

// migrations are applied in order; the index+1 is stored in PRAGMA user_version.
// Never edit an applied migration: append a new one.
var migrations = []string{
	`
CREATE TABLE memories (
	id         INTEGER PRIMARY KEY,
	space      TEXT    NOT NULL,
	title      TEXT    NOT NULL,
	content    TEXT    NOT NULL,
	kind       TEXT    NOT NULL DEFAULT 'note',
	topic      TEXT,
	tags       TEXT    NOT NULL DEFAULT '',
	trust      TEXT    NOT NULL DEFAULT 'agent',
	pinned     INTEGER NOT NULL DEFAULT 0,
	hash       TEXT    NOT NULL,
	revision   INTEGER NOT NULL DEFAULT 1,
	redactions INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_memories_topic ON memories(space, topic) WHERE topic IS NOT NULL;
CREATE UNIQUE INDEX idx_memories_hash  ON memories(space, hash);
CREATE INDEX idx_memories_recent ON memories(space, pinned DESC, updated_at DESC);
CREATE INDEX idx_memories_space_id ON memories(space, id);

CREATE VIRTUAL TABLE memories_fts USING fts5(
	title, content, tags,
	content='memories', content_rowid='id',
	tokenize='unicode61 remove_diacritics 2',
	prefix='2 3 4'
);
CREATE TRIGGER memories_ai AFTER INSERT ON memories BEGIN
	INSERT INTO memories_fts(rowid, title, content, tags) VALUES (new.id, new.title, new.content, new.tags);
END;
CREATE TRIGGER memories_ad AFTER DELETE ON memories BEGIN
	INSERT INTO memories_fts(memories_fts, rowid, title, content, tags) VALUES ('delete', old.id, old.title, old.content, old.tags);
END;
CREATE TRIGGER memories_au AFTER UPDATE ON memories BEGIN
	INSERT INTO memories_fts(memories_fts, rowid, title, content, tags) VALUES ('delete', old.id, old.title, old.content, old.tags);
	INSERT INTO memories_fts(rowid, title, content, tags) VALUES (new.id, new.title, new.content, new.tags);
END;
`,
	// v2: ids are never reused. Without AUTOINCREMENT SQLite hands out the
	// number of a deleted last row again, so an id saved elsewhere could end up
	// pointing at a different memory. Rows keep their ids, so the FTS index
	// stays valid.
	`
CREATE TABLE memories_new (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	space      TEXT    NOT NULL,
	title      TEXT    NOT NULL,
	content    TEXT    NOT NULL,
	kind       TEXT    NOT NULL DEFAULT 'note',
	topic      TEXT,
	tags       TEXT    NOT NULL DEFAULT '',
	trust      TEXT    NOT NULL DEFAULT 'agent',
	pinned     INTEGER NOT NULL DEFAULT 0,
	hash       TEXT    NOT NULL,
	revision   INTEGER NOT NULL DEFAULT 1,
	redactions INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
INSERT INTO memories_new (id, space, title, content, kind, topic, tags, trust, pinned, hash, revision, redactions, created_at, updated_at)
	SELECT id, space, title, content, kind, topic, tags, trust, pinned, hash, revision, redactions, created_at, updated_at FROM memories;
DROP TABLE memories;
ALTER TABLE memories_new RENAME TO memories;
CREATE UNIQUE INDEX idx_memories_topic ON memories(space, topic) WHERE topic IS NOT NULL;
CREATE UNIQUE INDEX idx_memories_hash  ON memories(space, hash);
CREATE INDEX idx_memories_recent ON memories(space, pinned DESC, updated_at DESC);
CREATE INDEX idx_memories_space_id ON memories(space, id);
CREATE TRIGGER memories_ai AFTER INSERT ON memories BEGIN
	INSERT INTO memories_fts(rowid, title, content, tags) VALUES (new.id, new.title, new.content, new.tags);
END;
CREATE TRIGGER memories_ad AFTER DELETE ON memories BEGIN
	INSERT INTO memories_fts(memories_fts, rowid, title, content, tags) VALUES ('delete', old.id, old.title, old.content, old.tags);
END;
CREATE TRIGGER memories_au AFTER UPDATE ON memories BEGIN
	INSERT INTO memories_fts(memories_fts, rowid, title, content, tags) VALUES ('delete', old.id, old.title, old.content, old.tags);
	INSERT INTO memories_fts(rowid, title, content, tags) VALUES (new.id, new.title, new.content, new.tags);
END;
`,
	// v3: when a memory was last handed to an agent (for review of stale notes)
	// and the flags raised by memory.Suspicious. Existing rows are re-flagged on
	// the first open after upgrading (see Store.reflag).
	`
ALTER TABLE memories ADD COLUMN last_used_at INTEGER;
ALTER TABLE memories ADD COLUMN flags TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_memories_used ON memories(space, last_used_at);
`,
	// v4: the url flag and the "without asking" pattern were dropped because
	// they flagged ordinary notes; flags are recomputed for every memory.
	`UPDATE memories SET flags = '';`,
}

func backfillFlags(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, title, content FROM memories`)
	if err != nil {
		return err
	}
	flags := map[int64]string{}
	for rows.Next() {
		var id int64
		var title, content string
		if err := rows.Scan(&id, &title, &content); err != nil {
			rows.Close()
			return err
		}
		if f := memory.Suspicious(title, content); len(f) > 0 {
			flags[id] = strings.Join(f, " ")
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for id, f := range flags {
		if _, err := tx.ExecContext(ctx, `UPDATE memories SET flags=? WHERE id=?`, f, id); err != nil {
			return err
		}
	}
	return nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	var v int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if v > len(migrations) {
		return fmt.Errorf("database schema v%d is newer than this noah (v%d); upgrade noah", v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if i == 2 || i == 3 { // v3 adds flags and v4 redefines them: recompute for existing memories
			if err := backfillFlags(ctx, tx); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %d: %w", i+1, err)
			}
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
