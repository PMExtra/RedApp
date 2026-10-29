package store

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Project every old column in every old table, including unchanged auth/config,
// counters and cache tables. New columns are checked separately, not ignored
// by projecting only a hand-picked subset of old data.
func oldRows(t *testing.T, path string, queries map[string]string) (map[string]string, map[string][]string) {
	t.Helper()
	db, err := sql.Open("sqlite3", sqliteURL(path, "mode=ro"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if queries == nil {
		queries = map[string]string{}
		tables, err := db.Query(`SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for tables.Next() {
			var name string
			if err = tables.Scan(&name); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
		tables.Close()
		for _, name := range names {
			rows, err := db.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, name)
			if err != nil {
				t.Fatal(err)
			}
			var columns []string
			for rows.Next() {
				var column string
				if err = rows.Scan(&column); err != nil {
					t.Fatal(err)
				}
				columns = append(columns, "quote("+strconv.Quote(column)+")")
			}
			rows.Close()
			queries[name] = "SELECT " + strings.Join(columns, " || ',' || ") + " FROM " + strconv.Quote(name)
		}
	}
	data := map[string][]string{}
	for name, query := range queries {
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var value string
			if err = rows.Scan(&value); err != nil {
				t.Fatal(err)
			}
			data[name] = append(data[name], value)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		sort.Strings(data[name])
	}
	return queries, data
}

// preserved reports whether every old row is still present: later migrations
// may add rows, such as new settings, but never change or drop old ones.
func preserved(before, after map[string][]string) bool {
	for table, rows := range before {
		left := map[string]int{}
		for _, row := range after[table] {
			left[row]++
		}
		for _, row := range rows {
			if left[row] == 0 {
				return false
			}
			left[row]--
		}
	}
	return true
}

func TestMigration15PreservesDataAndObjects(t *testing.T) {
	dir := t.TempDir()
	path := loadSchemaFixture(t, dir, fixturePath(15))
	db, err := sql.Open("sqlite3", sqliteURL(path, "mode=rw&_foreign_keys=on"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
 INSERT INTO admin(id,hash,revision) VALUES(1,X'666978747572652d68617368',7);
 INSERT INTO hosted_files(app_uid,path,id,sha256,size_bytes,created_at_s) SELECT uid,'kept.zip','fixture-hosted',?,4,1 FROM applications;
 INSERT INTO application_tags(app_uid,ordinal,tag,folded) SELECT uid,0,'kept-tag','kept-tag' FROM applications;
 INSERT INTO blobs(app_id,sha256,size_bytes,verified_at_s) SELECT app_id,sha256,4,1 FROM resources;
 INSERT INTO generations(id,app_id,version,resource_key,expected_sha256,blob_sha256,phase,is_current,bytes,total_bytes,source_bytes,resumes,full_retry,started_at_s,finished_at_s,download_ns,verification_ns,app_revision,vendor_revision,checkpoint) SELECT 'kept-generation',app_id,version,resource_key,sha256,sha256,'complete',1,4,4,4,0,0,1,2,1,1,1,1,1 FROM resources;
 INSERT INTO http_cache_generations(id,storage_id,path,sha256,size_bytes,headers_json,fetched_at_s,validated_at_s,fresh_until_s,is_current) SELECT '11111111111111111111111111111111',app_id,'kept.zip',sha256,4,'{}',1,1,2,1 FROM resources;
 INSERT INTO metric_samples(scope,app_id,metric,t_s,observed_at_s,boot,value,delta,duration_s) VALUES('global','','requests',1,1,'fixture',3,1,1);
 `, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	object := filepath.Join(dir, "objects", "fixture", "kept.bin")
	if err = os.MkdirAll(filepath.Dir(object), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(object, []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	queries, before := oldRows(t, path, nil)
	if err = Preflight(dir); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var bad, refresh int
	for _, table := range []string{"applications", "application_sources"} {
		if err = s.read.QueryRow("SELECT count(*) FROM " + table + " WHERE json_release_json != 'null'").Scan(&bad); err != nil || bad != 0 {
			t.Fatal(table, bad, err)
		}
	}
	if err = s.read.QueryRow("SELECT count(*) FROM release_refresh_status").Scan(&refresh); err != nil || refresh != 0 {
		t.Fatal(refresh, err)
	}
	s.Close()
	_, after := oldRows(t, path, queries)
	if !preserved(before, after) {
		t.Fatal("old table rows changed")
	}
	if body, err := os.ReadFile(object); err != nil || string(body) != "kept" {
		t.Fatal("object changed", err)
	}
	saved := backups(t, dir)
	if len(saved) != 1 {
		t.Fatal(saved)
	}
	_, backupData := oldRows(t, saved[0], queries)
	if !reflect.DeepEqual(before, backupData) {
		t.Fatal("backup differs")
	}
	if _, v := headerOf(t, saved[0]); v != 15 {
		t.Fatal(v)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if len(backups(t, dir)) != 1 {
		t.Fatal("restart repeated migration")
	}
}

func TestMigration15FailurePreservesOriginal(t *testing.T) {
	for _, mode := range []string{"after-rebuild", "disk-space", "backup-collision"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := loadSchemaFixture(t, dir, fixturePath(15))
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			plan := productionPlan()
			switch mode {
			case "after-rebuild":
				plan.steps = []migrationStep{{From: 15, Name: "rollback test", Up: func(tx *sql.Tx) error {
					if err := migrate15To16(tx); err != nil {
						return err
					}
					return errors.New("injected before commit")
				}}, {From: 16, Name: "provider scope", Up: migrate16To17}}
			case "disk-space":
				plan.freeSpace = func(string) (uint64, error) { return 0, nil }
			case "backup-collision":
				plan.now = syntheticPlan().now
				name := filepath.Join(dir, "state.sqlite.schema15-20300102T030405Z.backup")
				if err = os.WriteFile(name, []byte("keep backup"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err = plan.migrate(dir, path, discardLog); err == nil {
				t.Fatal("expected failure")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed migration changed original", err)
			}
			if _, v := headerOf(t, path); v != 15 {
				t.Fatal(v)
			}
		})
	}
}
