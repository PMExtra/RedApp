package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/instance"
)

// fileState captures contents and modification times so a refusal can be
// shown to leave every file, including WAL sidecars, untouched.
func fileState(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, body := range snapshotFiles(t, dir) {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		out[name] = fmt.Sprintf("%x@%d", body, info.ModTime().UnixNano())
	}
	return out
}

// rewriteHeader changes the header pragmas of a closed RedApp database and
// leaves a committed row in its WAL. The connection stays open until the test
// ends because closing the last connection would checkpoint and remove the WAL.
func rewriteHeader(t *testing.T, dir string, pragmas string) {
	t.Helper()
	db, err := sql.Open("sqlite3", sqliteURL(filepath.Join(dir, databaseName), "_journal_mode=WAL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(pragmas + `; PRAGMA wal_checkpoint(TRUNCATE); PRAGMA wal_autocheckpoint=0`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO pending_object_deletes(path) VALUES('objects/http/00000000000000000000000000000000.body')`); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRefusesIncompatibleDirectoriesWithoutModification(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, dir string){
		"other schema version": func(t *testing.T, dir string) {
			createDirectory(t, dir)
			rewriteHeader(t, dir, fmt.Sprintf("PRAGMA user_version=%d", SchemaVersion-1))
		},
		"foreign sqlite file with the same version": func(t *testing.T, dir string) {
			createDirectory(t, dir)
			rewriteHeader(t, dir, "PRAGMA application_id=0")
		},
		"files but no database": func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, "objects"), 0700); err != nil {
				t.Fatal(err)
			}
		},
		"database is not a regular file": func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, databaseName), 0700); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			prepare(t, dir)
			before := fileState(t, dir)
			if err := Preflight(dir); !errors.Is(err, ErrIncompatibleDirectory) {
				t.Fatalf("Preflight = %v; want ErrIncompatibleDirectory", err)
			}
			s, err := Open(dir)
			if s != nil {
				s.Close()
			}
			if !errors.Is(err, ErrIncompatibleDirectory) {
				t.Fatalf("Open = %v; want ErrIncompatibleDirectory", err)
			}
			if after := fileState(t, dir); !reflect.DeepEqual(before, after) {
				t.Fatal("refusing the directory modified, created or deleted files")
			}
		})
	}
}

func createDirectory(t *testing.T, dir string) {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCreatesVersionedSchemaInEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, instance.LockName), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Preflight(dir); err != nil {
		t.Fatalf("Preflight on a directory with only the lock file = %v", err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version, app int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != SchemaVersion {
		t.Fatalf("user_version = %d, %v; want %d", version, err, SchemaVersion)
	}
	if err := s.db.QueryRow(`PRAGMA application_id`).Scan(&app); err != nil || app != applicationID {
		t.Fatalf("application_id = %d, %v; want %d", app, err, applicationID)
	}
	if err := Preflight(filepath.Join(dir, "missing")); err != nil {
		t.Fatalf("Preflight on a missing directory = %v", err)
	}
}

// Committed transactions may still be in the WAL after an unclean exit; the
// read-only preflight must accept the directory without touching it.
func TestPreflightAcceptsValidDatabaseWithPendingWAL(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.SaveSiteSettings(1, map[string]string{"title": "pending"}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dir, databaseName+"-wal")); err != nil || info.Size() == 0 {
		t.Fatalf("fixture left no WAL data: %v", err)
	}
	before := fileState(t, dir)
	if err = Preflight(dir); err != nil {
		t.Fatalf("Preflight = %v", err)
	}
	if after := fileState(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatal("Preflight modified the directory")
	}
}

func TestPreflightReportsLiveOwner(t *testing.T) {
	dir := t.TempDir()
	guard, err := instance.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err = Preflight(dir); err == nil || errors.Is(err, ErrIncompatibleDirectory) {
		t.Fatalf("Preflight with a live owner = %v; want an ownership error", err)
	}
}

// Version, metadata and generations belong to one release; deleting the
// version must remove the rows that reference it.
func TestReleaseRowsCascadeFromVersion(t *testing.T) {
	s := openTest(t)
	r := releaseFixture(t, s, storageOf(t, s, "openai/codex"), "1.0.0")
	now := time.Now().UTC()
	if err := s.PutChannel(Channel{AppID: r.AppID, Name: "latest", Version: r.Version, FetchedAt: now, ExpiresAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	g := Generation{ID: "generation", AppID: r.AppID, Version: r.Version, ResourceKey: r.Key, ExpectedSHA256: r.SHA256, Phase: "incomplete", IsCurrent: true, StartedAt: now, SourceFence: fenceOf(t, s, r.AppID)}
	if err := s.CreateGeneration(g); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM app_versions WHERE app_id=?`, r.AppID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"release_metadata", "channels", "resources", "generations"} {
		var count int
		if err := s.db.QueryRow(`SELECT count(*) FROM `+table+` WHERE app_id=?`, r.AppID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s kept %d rows after its version was deleted: %v", table, count, err)
		}
	}
}
