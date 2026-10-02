package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestVersionMigrationPreservesHistoryAndIsolation(t *testing.T) {
	dir := t.TempDir()
	db, e := sql.Open("sqlite3", filepath.Join(dir, "state.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE TABLE schema_version(version INTEGER NOT NULL);INSERT INTO schema_version VALUES(1);CREATE TABLE versions(version TEXT PRIMARY KEY,first_seen TEXT NOT NULL);INSERT INTO versions VALUES('2.1.285','2026-09-01T12:00:00Z');CREATE TABLE records(kind TEXT,id TEXT,body BLOB,PRIMARY KEY(kind,id));INSERT INTO records VALUES('current','old-object','{"Generation":"unchanged"}');`)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		s, e := Open(dir)
		if e != nil {
			t.Fatal(e)
		}
		before, e := s.Versions()
		if e != nil || before["2.1.285"] != "2026-09-01T12:00:00Z" {
			t.Fatal(before, e)
		}
		if e = s.SeenFor("claude-code", "2.1.285"); e != nil {
			t.Fatal(e)
		}
		other, _ := s.VersionsFor("claude-code")
		if len(other) != 1 || other["2.1.285"] == before["2.1.285"] {
			t.Fatal("history not isolated")
		}
		if e = s.Seen("2.1.285"); e != nil {
			t.Fatal(e)
		}
		after, _ := s.Versions()
		if after["2.1.285"] != before["2.1.285"] {
			t.Fatal("first_seen rewritten")
		}
		var resource map[string]string
		if e = s.Get("current", "old-object", &resource); e != nil || resource["Generation"] != "unchanged" {
			t.Fatal("cache identity changed", e)
		}
		s.DB.Close()
	}
}
func TestFailedVersionMigrationRollsBack(t *testing.T) {
	dir := t.TempDir()
	db, e := sql.Open("sqlite3", filepath.Join(dir, "state.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE TABLE schema_version(version INTEGER NOT NULL);INSERT INTO schema_version VALUES(1);CREATE TABLE versions(version TEXT PRIMARY KEY,first_seen TEXT);INSERT INTO versions VALUES('2.1.285',NULL);`)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	if s, e := Open(dir); e == nil {
		s.DB.Close()
		t.Fatal("invalid legacy state silently migrated")
	}
	db, _ = sql.Open("sqlite3", filepath.Join(dir, "state.sqlite"))
	defer db.Close()
	var version, count int
	if e = db.QueryRow("SELECT version FROM schema_version").Scan(&version); e != nil || version != 1 {
		t.Fatal("migration version committed", e)
	}
	if e = db.QueryRow("SELECT count(*) FROM versions WHERE first_seen IS NULL").Scan(&count); e != nil || count != 1 {
		t.Fatal("legacy data changed", e)
	}
}

func TestMetadataHistoryCommitIsAtomicAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Fail after the metadata write, at the history insertion boundary.
	if _, err = s.DB.Exec(`CREATE TRIGGER reject_history BEFORE INSERT ON versions BEGIN SELECT RAISE(FAIL,'injected history failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err = s.PutVersion("trusted", "2.1.285", "new metadata", "claude-code", "2.1.285"); err == nil {
		t.Fatal("history fault ignored")
	}
	s.DB.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	var value string
	if err = s.Get("trusted", "2.1.285", &value); err != sql.ErrNoRows {
		t.Fatal("partial metadata survived restart", value, err)
	}
	if _, err = s.DB.Exec("DROP TRIGGER reject_history"); err != nil {
		t.Fatal(err)
	}
	if err = s.PutVersion("trusted", "2.1.285", "verified", "claude-code", "2.1.285"); err != nil {
		t.Fatal(err)
	}
	before, _ := s.VersionsFor("claude-code")
	if err = s.PutVersion("trusted", "2.1.285", "verified again", "claude-code", "2.1.285"); err != nil {
		t.Fatal(err)
	}
	after, _ := s.VersionsFor("claude-code")
	if before["2.1.285"] == "" || before["2.1.285"] != after["2.1.285"] {
		t.Fatal("first_seen changed")
	}
	if got, _ := s.VersionsFor("codex"); len(got) != 0 {
		t.Fatal("history leaked across apps")
	}
}
