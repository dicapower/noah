package store

import (
	"database/sql"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go SQLite driver: no C toolchain needed to build
)

const driverName = "sqlite"

// dsn builds the connection string. Pragmas are set in the DSN so that every
// connection the pool opens gets them. The writer uses BEGIN IMMEDIATE so that
// concurrent writers queue on the busy timeout instead of failing mid-transaction.
func dsn(path string, writer bool) string {
	q := url.Values{}
	for _, p := range []string{
		"busy_timeout(5000)",
		"journal_mode(WAL)",
		"synchronous(NORMAL)",
		"foreign_keys(ON)",
		"temp_store(MEMORY)",
	} {
		q.Add("_pragma", p)
	}
	if writer {
		q.Set("_txlock", "immediate")
	} else {
		q.Add("_pragma", "query_only(1)")
	}
	u := url.URL{Scheme: "file", Path: uriPath(path), RawQuery: q.Encode()}
	return u.String()
}

// uriPath turns a file system path into the path part of a file: URI. A Windows
// path like C:\Users\me\noah.db becomes /C:/Users/me/noah.db; without this the
// drive letter is read as a host name and the database cannot be opened.
func uriPath(p string) string {
	p = filepath.ToSlash(p)
	if len(p) >= 2 && p[1] == ':' {
		p = "/" + p
	}
	return p
}

// OpenReadOnlyDB opens a foreign SQLite file read-only (used by importers).
func OpenReadOnlyDB(path string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: uriPath(path), RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}
	return sql.Open(driverName, u.String())
}
