package httpcache

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// sql opens a second connection to the fixture database. Tests use it only to
// seed many rows in one transaction, to inject faults with triggers and to
// inspect rows; the service under test reaches the database only through the
// store. Triggers created here persist and fire on the store's connection.
func (f *fixture) sql(t *testing.T) *sql.DB {
	t.Helper()
	path := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(f.dir, "state.sqlite"))}).String()
	db, err := sql.Open("sqlite3", path+"?mode=rw&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}
