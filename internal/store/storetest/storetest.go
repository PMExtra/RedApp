// Package storetest gives tests outside internal/store direct SQL access to a
// data directory's database, for fault injection (triggers, held locks) and
// fixtures that have no store API. Only _test.go files may import it.
package storetest

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// Open returns a connection pool to the database of the data directory dir,
// separate from any open Store, that is closed when the test ends. Writes
// commit like any other writer; TEMP objects are visible only to this pool.
func Open(t testing.TB, dir string) *sql.DB {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(dir, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: path}).String()+"?mode=rw&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}
