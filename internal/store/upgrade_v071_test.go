package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPublishedV070UpgradePreservesAllRowsAndRestarts(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite3", sqliteURL(filepath.Join(dir, "state.sqlite"), "_journal_mode=WAL"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schemaV4); err != nil {
		t.Fatal(err)
	}
	const uid = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const vendor = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const sqlFixture = `
 UPDATE directory_state SET seeded=1;
 INSERT INTO vendors VALUES('` + vendor + `','acme','Acme','示例','','','',1,7,NULL);
 INSERT INTO applications VALUES('` + uid + `','` + vendor + `','files','Files','文件','','','', 'general-http','https://new.example',120,'["https://new.example"]','ordered',0,9,2,NULL);
 INSERT INTO application_sources VALUES('` + uid + `',1,'general-http','https://old.example',100,'["https://old.example"]','ordered');
 INSERT INTO application_sources VALUES('` + uid + `',2,'general-http','https://new.example',200,'["https://new.example"]','ordered');
 INSERT INTO settings VALUES('global','','site',3,'{"name":"keep me"}');
 INSERT INTO metric_counters VALUES('app','app/` + uid + `','counters.download_errors',5,100);
 INSERT INTO metric_samples VALUES('app','app/` + uid + `','events.recent_total',100,100,'boot',8,NULL,60);
 INSERT INTO metric_hours VALUES('app','app/` + uid + `','counters.reuse_requests',0,1,2,1.5,2,2,1,1,120);
 INSERT INTO events VALUES(1,100,'app/` + uid + `',NULL,NULL,NULL,'download','UPSTREAM','Keep event',503);
 INSERT INTO http_cache_generations(id,storage_id,path,sha256,size_bytes,fetched_at_s,validated_at_s,fresh_until_s,headers_json,is_current) VALUES('cccccccccccccccccccccccccccccccc','app/` + uid + `-e1','keep.bin','dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',8,100,100,200,'{}',1);
 `
	if _, err = db.Exec(sqlFixture); err != nil {
		t.Fatal(err)
	}
	tables := []string{"vendors", "directory_state", "settings", "metric_counters", "metric_samples", "metric_hours", "events", "http_cache_generations"}
	snapshot := func(db *sql.DB) map[string][]string {
		out := map[string][]string{}
		for _, table := range tables {
			rows, e := db.Query("SELECT * FROM " + table)
			if e != nil {
				t.Fatal(e)
			}
			cols, _ := rows.Columns()
			for rows.Next() {
				values := make([]any, len(cols))
				ptr := make([]any, len(cols))
				for i := range values {
					ptr[i] = &values[i]
				}
				if e = rows.Scan(ptr...); e != nil {
					t.Fatal(e)
				}
				out[table] = append(out[table], fmt.Sprint(values))
			}
			if e = rows.Err(); e != nil {
				t.Fatal(e)
			}
			rows.Close()
		}
		return out
	}
	before := snapshot(db)
	// Keep committed data in WAL: preflight must accept the exact published schema
	// without losing pending data when the upgrade opens the authoritative view.
	if _, err = db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO events VALUES(2,101,NULL,NULL,NULL,NULL,'download','WAL','Committed WAL',500)`); err != nil {
		t.Fatal(err)
	}
	before = snapshot(db)
	if err = Preflight(dir); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot(upgraded.DB); !reflect.DeepEqual(before, got) {
		t.Fatal("unrelated rows changed", before, got)
	}
	a, err := upgraded.Application("acme/files")
	if err != nil || a.UID != uid || a.Revision != 9 || a.SourceEpoch != 2 || a.Provider != "http-cache" || a.Enabled {
		t.Fatal(a, err)
	}
	sources, err := upgraded.Sources()
	if err != nil || len(sources) != 2 {
		t.Fatal(sources, err)
	}
	for _, s := range sources {
		if s.Provider != "http-cache" || s.AppUID != uid {
			t.Fatal(s)
		}
	}
	if err = upgraded.SeedDirectory(nil, nil); err != nil {
		t.Fatal(err)
	}
	if err = checkSchema(upgraded.DB); err != nil {
		t.Fatal(err)
	}
	upgraded.DB.Close()
	db.Close()
	upgraded, err = Open(dir)
	if err != nil {
		t.Fatal("restart", err)
	}
	defer upgraded.DB.Close()
	if got := snapshot(upgraded.DB); !reflect.DeepEqual(before, got) {
		t.Fatal("restart changed rows")
	}
}

func TestDirectoryPagesFindSixthAppAndClampBoundaries(t *testing.T) {
	s := openTest(t)
	v, err := s.CreateVendor(directoryVendor("acme"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 8; i++ {
		input := directoryApplication(fmt.Sprintf("tool-%d", i))
		input.Name = LocalizedText{En: fmt.Sprintf("Tool %d", i), ZhCN: fmt.Sprintf("工具%d", i)}
		if _, err = s.CreateApplication(v.ID, input); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.DirectoryPage(1, 1, "", "current")
	if err != nil || first.Total != 1 || len(first.Items) != 1 || len(first.Items[0].Apps) != 5 || first.Items[0].AppTotal != 8 {
		t.Fatal(first, err)
	}
	for _, q := range []string{"tool-6", "工具6", "TOOL 6", "acme/tool-6"} {
		page, err := s.DirectoryPage(99, 1, q, "current")
		if err != nil || page.Page != 1 || len(page.Items) != 1 || page.Items[0].Apps[0].ID != "tool-6" {
			t.Fatal(q, page, err)
		}
	}
	apps, err := s.ApplicationPage("acme", 99, 3, "", "current")
	if err != nil || apps.Page != 3 || apps.Total != 8 || len(apps.Items) != 2 {
		t.Fatal(apps, err)
	}
	empty, err := s.ApplicationPage("acme", 1, 3, "nonexistent", "current")
	if err != nil || empty.Page != 1 || empty.TotalPages != 1 || len(empty.Items) != 0 {
		t.Fatal(empty, err)
	}
	a, _ := s.Application("acme/tool-6")
	change := applicationChanges(a)
	change.Enabled = false
	if _, err = s.UpdateApplication(a.Key, a.Revision, change); err != nil {
		t.Fatal(err)
	}
	disabled, err := s.ApplicationPage("acme", 1, 3, "", "disabled")
	if err != nil || disabled.Total != 1 || disabled.Items[0].ID != "tool-6" {
		t.Fatal(disabled, err)
	}
}
