// Package store persists memories in SQLite.
//
// There is one writer connection and a separate pool of read-only connections.
// In WAL mode readers never block the writer and the writer never blocks
// readers, which is what keeps searches fast while an agent is saving.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dicapower/noah/internal/secrets"
)

// Errors callers can test for with errors.Is.
var (
	ErrNotFound  = errors.New("memory not found")
	ErrProtected = errors.New("a user-written memory cannot be overwritten by an agent")
	ErrConflict  = errors.New("identical content is already stored under another topic")
	ErrSecret    = errors.New("possible secret detected")
)

// Options configure Open.
type Options struct {
	SecretPolicy secrets.Policy
	Now          func() time.Time
}

// Store is a handle to the database. It is safe for concurrent use.
type Store struct {
	path   string
	w, r   *sql.DB
	policy secrets.Policy
	now    func() time.Time
}

// DefaultPath returns the database path: $NOAH_DB or ~/.noah/noah.db.
func DefaultPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("NOAH_DB")); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find home directory; set NOAH_DB: %w", err)
	}
	return filepath.Join(home, ".noah", "noah.db"), nil
}

// Open opens (creating and migrating if needed) the database at path.
func Open(path string, o Options) (*Store, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.SecretPolicy == "" {
		o.SecretPolicy = secrets.Redact
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	w, err := sql.Open(driverName, dsn(path, true))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := w.PingContext(ctx); err != nil {
		w.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := migrate(ctx, w); err != nil {
		w.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600) // memory can contain private notes
	r, err := sql.Open(driverName, dsn(path, false))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(4)
	return &Store{path: path, w: w, r: r, policy: o.SecretPolicy, now: o.Now}, nil
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// Close closes both connection pools.
func (s *Store) Close() error {
	return errors.Join(s.r.Close(), s.w.Close())
}
