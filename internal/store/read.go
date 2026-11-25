package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/dicapower/noah/internal/memory"
)

const cols = `m.id, m.space, m.title, m.content, m.kind, COALESCE(m.topic,''), m.tags, m.trust,
	m.pinned, m.revision, m.redactions, m.created_at, m.updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanMemory(sc scanner, extra ...any) (memory.Memory, error) {
	var m memory.Memory
	var tags, trust string
	var pinned int
	dest := append([]any{&m.ID, &m.Space, &m.Title, &m.Content, &m.Kind, &m.Topic, &tags, &trust,
		&pinned, &m.Revision, &m.Redactions, &m.CreatedAt, &m.UpdatedAt}, extra...)
	if err := sc.Scan(dest...); err != nil {
		return m, err
	}
	m.Trust = memory.Trust(trust)
	m.Pinned = pinned != 0
	if tags != "" {
		m.Tags = strings.Fields(tags)
	}
	return m, nil
}

type queryer interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func getTx(ctx context.Context, q queryer, id int64) (memory.Memory, error) {
	m, err := scanMemory(q.QueryRowContext(ctx, `SELECT `+cols+` FROM memories m WHERE m.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// Get returns one memory. An empty space means any space (administrative use).
func (s *Store) Get(ctx context.Context, space string, id int64) (memory.Memory, error) {
	m, err := getTx(ctx, s.r, id)
	if err != nil {
		return m, err
	}
	if space != "" && m.Space != space {
		return memory.Memory{}, ErrNotFound // do not reveal that it exists elsewhere
	}
	return m, nil
}

// Forget permanently deletes a memory (including its search index entry).
// An empty space means any space.
func (s *Store) Forget(ctx context.Context, space string, id int64) error {
	q, args := `DELETE FROM memories WHERE id=?`, []any{id}
	if space != "" {
		q, args = q+` AND space=?`, append(args, space)
	}
	res, err := s.w.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Candidates returns up to limit FTS matches ordered by BM25. An empty space
// searches every space; an empty kind matches every kind.
func (s *Store) Candidates(ctx context.Context, space, kind, match string, limit int) ([]memory.Scored, error) {
	if match == "" {
		return nil, nil
	}
	q := `SELECT ` + cols + `, bm25(memories_fts, 5.0, 1.0, 3.0) AS score
		FROM memories_fts JOIN memories m ON m.id = memories_fts.rowid
		WHERE memories_fts MATCH ?`
	args := []any{match}
	if space != "" {
		q += ` AND m.space = ?`
		args = append(args, space)
	}
	if kind != "" {
		q += ` AND m.kind = ?`
		args = append(args, kind)
	}
	q += ` ORDER BY score LIMIT ?`
	args = append(args, limit)
	rows, err := s.r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []memory.Scored
	for rows.Next() {
		var bm float64
		m, err := scanMemory(rows, &bm)
		if err != nil {
			return nil, err
		}
		out = append(out, memory.Scored{Memory: m, BM25: bm})
	}
	return out, rows.Err()
}

// Recent returns pinned memories first, then the most recently updated.
func (s *Store) Recent(ctx context.Context, space string, limit int) ([]memory.Memory, error) {
	return s.list(ctx, space, "", limit, 0)
}

// List pages through memories, newest first (pinned first).
func (s *Store) List(ctx context.Context, space, kind string, limit, offset int) ([]memory.Memory, error) {
	return s.list(ctx, space, kind, limit, offset)
}

func (s *Store) list(ctx context.Context, space, kind string, limit, offset int) ([]memory.Memory, error) {
	q := `SELECT ` + cols + ` FROM memories m WHERE 1=1`
	var args []any
	if space != "" {
		q += ` AND m.space=?`
		args = append(args, space)
	}
	if kind != "" {
		q += ` AND m.kind=?`
		args = append(args, kind)
	}
	q += ` ORDER BY m.pinned DESC, m.updated_at DESC, m.id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []memory.Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SpaceInfo summarises one space.
type SpaceInfo struct {
	Name    string `json:"name"`
	Count   int    `json:"count"`
	Updated int64  `json:"updated_at"`
}

// Spaces lists every space with its memory count.
func (s *Store) Spaces(ctx context.Context) ([]SpaceInfo, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT space, COUNT(*), MAX(updated_at) FROM memories GROUP BY space ORDER BY MAX(updated_at) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpaceInfo
	for rows.Next() {
		var si SpaceInfo
		if err := rows.Scan(&si.Name, &si.Count, &si.Updated); err != nil {
			return nil, err
		}
		out = append(out, si)
	}
	return out, rows.Err()
}

// Each calls fn for every memory in space (all spaces when empty), oldest first.
// It stops at the first error fn returns.
func (s *Store) Each(ctx context.Context, space string, fn func(memory.Memory) error) error {
	q, args := `SELECT `+cols+` FROM memories m`, []any{}
	if space != "" {
		q, args = q+` WHERE m.space=?`, append(args, space)
	}
	rows, err := s.r.QueryContext(ctx, q+` ORDER BY m.id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return err
		}
		if err := fn(m); err != nil {
			return err
		}
	}
	return rows.Err()
}
