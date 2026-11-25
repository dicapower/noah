package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/dicapower/noah/internal/memory"
	"github.com/dicapower/noah/internal/secrets"
)

// SaveResult describes what Save did.
type SaveResult struct {
	Memory     memory.Memory `json:"memory"`
	Created    bool          `json:"created"`
	Updated    bool          `json:"updated"`   // an existing topic was revised
	Duplicate  bool          `json:"duplicate"` // identical content already existed
	Redactions int           `json:"redactions"`
}

// Save validates, scans for secrets, de-duplicates and stores a memory.
//
//   - Same space and topic: the existing memory is revised in place.
//   - Same space and identical content: nothing new is stored.
//   - An agent cannot overwrite a memory whose trust is "user".
func (s *Store) Save(ctx context.Context, in memory.Input) (SaveResult, error) {
	in, err := in.Clean()
	if err != nil {
		return SaveResult{}, err
	}
	title, f1, err := secrets.Apply(in.Title, s.policy)
	if err != nil {
		return SaveResult{}, fmt.Errorf("%w: %v", ErrSecret, err)
	}
	content, f2, err := secrets.Apply(in.Content, s.policy)
	if err != nil {
		return SaveResult{}, fmt.Errorf("%w: %v", ErrSecret, err)
	}
	res := SaveResult{Redactions: len(f1) + len(f2)}
	newTitle := title // what a brand-new memory is titled
	if newTitle == "" {
		newTitle = memory.DeriveTitle(content) // derived from the already-redacted content
	}
	newKind := in.Kind
	if newKind == "" {
		newKind = memory.DefaultKind
	}
	hash := memory.Hash(content)
	tags := strings.Join(in.Tags, " ")
	now := s.now().UnixMilli()
	pinned := b2i(in.Pinned)

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	var id int64
	if in.Topic != "" {
		var oldTrust string
		err := tx.QueryRowContext(ctx, `SELECT id, trust FROM memories WHERE space=? AND topic=?`, in.Space, in.Topic).Scan(&id, &oldTrust)
		switch {
		case err == nil:
			if memory.Trust(oldTrust) == memory.TrustUser && in.Trust != memory.TrustUser {
				return res, ErrProtected
			}
			var other int64
			if e := tx.QueryRowContext(ctx, `SELECT id FROM memories WHERE space=? AND hash=? AND id<>?`, in.Space, hash, id).Scan(&other); e == nil {
				return res, fmt.Errorf("%w (memory #%d)", ErrConflict, other)
			}
			// Revising a topic keeps the title, kind and tags the caller did not restate.
			_, err = tx.ExecContext(ctx, `UPDATE memories SET
				title=COALESCE(NULLIF(?,''), title), content=?, kind=COALESCE(NULLIF(?,''), kind),
				tags=CASE WHEN ?='' THEN tags ELSE ? END, trust=?, pinned=pinned OR ?,
				hash=?, revision=revision+1, redactions=?, updated_at=? WHERE id=?`,
				title, content, in.Kind, tags, tags, string(in.Trust), pinned, hash, res.Redactions, now, id)
			if err != nil {
				return res, err
			}
			res.Updated = true
		case errors.Is(err, sql.ErrNoRows):
			id = 0
		default:
			return res, err
		}
	}
	if id == 0 {
		var oldTrust string
		err := tx.QueryRowContext(ctx, `SELECT id, trust FROM memories WHERE space=? AND hash=?`, in.Space, hash).Scan(&id, &oldTrust)
		switch {
		case err == nil:
			trust := oldTrust
			if in.Trust == memory.TrustUser {
				trust = string(memory.TrustUser) // a human confirming a note raises its trust
			}
			if _, err := tx.ExecContext(ctx, `UPDATE memories SET updated_at=?, pinned=pinned OR ?, trust=? WHERE id=?`, now, pinned, trust, id); err != nil {
				return res, err
			}
			res.Duplicate = true
		case errors.Is(err, sql.ErrNoRows):
			r, err := tx.ExecContext(ctx, `INSERT INTO memories
				(space,title,content,kind,topic,tags,trust,pinned,hash,redactions,created_at,updated_at)
				VALUES (?,?,?,?,NULLIF(?,''),?,?,?,?,?,?,?)`,
				in.Space, newTitle, content, newKind, in.Topic, tags, string(in.Trust), pinned, hash, res.Redactions, now, now)
			if err != nil {
				return res, err
			}
			if id, err = r.LastInsertId(); err != nil {
				return res, err
			}
			res.Created = true
		default:
			return res, err
		}
	}
	m, err := getTx(ctx, tx, id)
	if err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	res.Memory = m
	return res, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
