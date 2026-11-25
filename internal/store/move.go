package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/dicapower/noah/internal/memory"
)

// Move changes the space of a memory, for example from a project to Global. It
// fails with ErrConflict when the target space already holds the same topic or
// identical content.
func (s *Store) Move(ctx context.Context, id int64, space string) (memory.Memory, error) {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return memory.Memory{}, err
	}
	defer tx.Rollback()
	var old string
	if err := tx.QueryRowContext(ctx, `SELECT space FROM memories WHERE id=?`, id).Scan(&old); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return memory.Memory{}, ErrNotFound
		}
		return memory.Memory{}, err
	}
	if old != space {
		if _, err := tx.ExecContext(ctx, `UPDATE memories SET space=? WHERE id=?`, space, id); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return memory.Memory{}, ErrConflict
			}
			return memory.Memory{}, err
		}
	}
	m, err := getTx(ctx, tx, id)
	if err != nil {
		return memory.Memory{}, err
	}
	return m, tx.Commit()
}
