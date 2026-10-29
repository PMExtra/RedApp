package store

import (
	"database/sql"
	"testing"
)

// Rebuilding the applications table keeps every column, including
// auto_index, and gives existing applications no download choices.
func TestMigration21KeepsApplicationsAndAddsEmptyDownloadChoices(t *testing.T) {
	dir := t.TempDir()
	path := loadSchemaFixture(t, dir, fixturePath(20))
	db, err := sql.Open("sqlite3", sqliteURL(path, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE applications SET auto_index=1`); err != nil {
		t.Fatal(err)
	}
	var epoch int64
	if err = db.QueryRow(`SELECT source_epoch FROM applications`).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var autoIndex, apps int
	var defaultArtifact, hidden string
	if err = s.read.QueryRow(`SELECT count(*),min(auto_index),max(default_artifact),max(hidden_artifacts_json) FROM applications`).Scan(&apps, &autoIndex, &defaultArtifact, &hidden); err != nil {
		t.Fatal(err)
	}
	if apps == 0 || autoIndex != 1 || defaultArtifact != "" || hidden != "[]" {
		t.Fatalf("applications=%d auto_index=%d default=%q hidden=%q", apps, autoIndex, defaultArtifact, hidden)
	}
	// The fixture application is not a JSON release: it keeps its epoch.
	var after int64
	if err = s.read.QueryRow(`SELECT source_epoch FROM applications`).Scan(&after); err != nil || after != epoch {
		t.Fatal("epoch of another provider changed", epoch, after, err)
	}
}
