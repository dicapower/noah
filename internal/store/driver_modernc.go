package store

import (
	"net/url"

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
	u := url.URL{Scheme: "file", Path: path, RawQuery: q.Encode()}
	return u.String()
}
