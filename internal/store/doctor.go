package store

import (
	"context"
	"os"

	"github.com/dicapower/noah/internal/memory"
	"github.com/dicapower/noah/internal/secrets"
)

// Report is the result of Check.
type Report struct {
	Path           string  `json:"path"`
	SizeBytes      int64   `json:"size_bytes"`
	SchemaVersion  int     `json:"schema_version"`
	JournalMode    string  `json:"journal_mode"`
	Memories       int     `json:"memories"`
	Spaces         int     `json:"spaces"`
	Unscoped       int     `json:"unscoped_memories"`
	IntegrityOK    bool    `json:"integrity_ok"`
	IntegrityMsg   string  `json:"integrity_msg,omitempty"`
	IndexOK        bool    `json:"index_ok"`
	IndexMsg       string  `json:"index_msg,omitempty"`
	WithSecrets    int     `json:"memories_with_possible_secrets"`
	SecretMemories []int64 `json:"secret_memory_ids,omitempty"`
}

// Check inspects the database: file health, search index consistency, and
// whether any stored memory still looks like it contains a credential.
func (s *Store) Check(ctx context.Context) (Report, error) {
	r := Report{Path: s.path}
	if fi, err := os.Stat(s.path); err == nil {
		r.SizeBytes = fi.Size()
	}
	_ = s.r.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&r.SchemaVersion)
	_ = s.r.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&r.JournalMode)
	_ = s.r.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(DISTINCT space), COALESCE(SUM(space=?),0) FROM memories`, memory.Unscoped).Scan(&r.Memories, &r.Spaces, &r.Unscoped)
	if err := s.r.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&r.IntegrityMsg); err != nil {
		return r, err
	}
	r.IntegrityOK = r.IntegrityMsg == "ok"
	if r.IntegrityOK {
		r.IntegrityMsg = ""
	}
	// FTS5's own consistency check compares the index with the content table.
	if _, err := s.w.ExecContext(ctx, `INSERT INTO memories_fts(memories_fts, rank) VALUES('integrity-check', 1)`); err != nil {
		r.IndexMsg = err.Error()
	} else {
		r.IndexOK = true
	}
	err := s.Each(ctx, "", func(m memory.Memory) error {
		if len(secrets.Scan(m.Title+"\n"+m.Content)) > 0 {
			r.WithSecrets++
			if len(r.SecretMemories) < 50 {
				r.SecretMemories = append(r.SecretMemories, m.ID)
			}
		}
		return nil
	})
	return r, err
}
