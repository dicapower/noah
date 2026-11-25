package store

import (
	"context"
	"database/sql"
	"fmt"
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
