package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSameVersionSourceSchemaRejectedWithoutWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.sqlite")
	db, err := sql.Open("sqlite3", sqliteURL(path, "_journal_mode=WAL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// This is an earlier development snapshot carrying the same version marker.
	// It has all current tables but predates immutable multi-source metadata.
	oldSchema := strings.ReplaceAll(schema, "  base_urls_json TEXT NOT NULL,\n", "")
	oldSchema = strings.ReplaceAll(oldSchema, "  source_strategy TEXT NOT NULL CHECK(source_strategy IN ('','ordered','round_robin','random')),\n", "")
	if oldSchema == schema {
		t.Fatal("fixture did not alter the source schema")
	}
	if _, err = db.Exec(oldSchema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO settings(scope,app_id,key,revision,payload) VALUES('app','vendor/app','channel_ttl',1,'{"seconds":37}')`); err != nil {
		t.Fatal(err)
	}
	var version int
	if err = db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil || version != SchemaVersion {
		t.Fatal("same-version fixture is invalid", version, err)
	}
	before := snapshotFiles(t, dir)
	if len(before["state.sqlite-wal"]) == 0 {
		t.Fatal("fixture must retain pending committed WAL data")
	}
	mtimes := map[string]time.Time{}
	for name := range before {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		mtimes[name] = info.ModTime()
	}
	if err = Preflight(dir); !errors.Is(err, ErrFreshDirectory) {
		t.Fatal("preflight accepted older same-version source schema", err)
	}
	opened, err := Open(dir)
	if opened != nil {
		opened.DB.Close()
	}
	if !errors.Is(err, ErrFreshDirectory) {
		t.Fatal("Open accepted older same-version source schema", err)
	}
	if after := snapshotFiles(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatal("refusal modified database bytes or created/deleted sidecars")
	}
	for name, mtime := range mtimes {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.ModTime().Equal(mtime) {
			t.Fatal("refusal wrote source file", name, err)
		}
	}
}

func TestHTTPMaintenancePagesUseSourceRowRangeIndex(t *testing.T) {
	s := openTest(t)
	for _, query := range []struct {
		name string
		sql  string
		args []any
	}{
		{"frozen page", `SELECT row_no,id,storage_id,path,sha256,size_bytes FROM http_cache_generations WHERE storage_id=? AND is_current=1 AND row_no>? AND row_no<=? ORDER BY row_no LIMIT ?`, []any{"app/test-e1", 1000, 1000000, 1000}},
		{"high water", `SELECT COALESCE(MAX(row_no),0) FROM http_cache_generations WHERE storage_id=? AND is_current=1`, []any{"app/test-e1"}},
	} {
		t.Run(query.name, func(t *testing.T) {
			rows, err := s.DB.Query("EXPLAIN QUERY PLAN "+query.sql, query.args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var plan strings.Builder
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				plan.WriteString(detail + "\n")
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(plan.String(), "http_cache_scan") || strings.Contains(plan.String(), "TEMP B-TREE") {
				t.Fatalf("maintenance query scans/sorts instead of paging source rows: %s", plan.String())
			}
			if query.name == "frozen page" && !strings.Contains(plan.String(), "row_no>? AND row_no<?") {
				t.Fatalf("row boundaries are not constrained by the index: %s", plan.String())
			}
		})
	}
}
