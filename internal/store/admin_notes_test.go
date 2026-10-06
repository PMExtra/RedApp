package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAdminNotesUpgradeV8PreservesDataAndStableIdentity(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(dir, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schemaV8); err != nil {
		t.Fatal(err)
	}
	old := &Store{DB: db}
	v, err := legacyVendor(t, db, VendorInput{ID: "openai", Name: LocalizedText{En: "Custom", ZhCN: "自定义"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO applications(uid,vendor_uid,id,name_en,name_zh_cn,description_en,description_zh_cn,icon,provider,base_url,cache_ttl_seconds,base_urls_json,source_strategy,enabled,revision,source_epoch) VALUES('cccccccccccccccccccccccccccccccc',?,'private','Private','私有','','','','info','',0,'[]','',1,1,1)`, v.UID); err != nil {
		t.Fatal(err)
	}
	a, err := old.Application(v.ID + "/private")
	if err != nil {
		t.Fatal(err)
	}
	instructions, err := old.SaveInstructions(a.Key, 0, LocalizedText{En: "Keep public instructions"})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	gotV, _ := s.Vendor(v.ID)
	gotA, _ := s.Application(a.Key)
	gotI, _ := s.Instructions(a.UID)
	if !reflect.DeepEqual(v, gotV) || !reflect.DeepEqual(a, gotA) || gotI != instructions {
		t.Fatal("upgrade changed existing data", gotV, gotA, gotI)
	}
	if err = checkSchema(s.DB); err != nil {
		t.Fatal(err)
	}
	if checkSchemaDefinition(s.DB, schemaV8, 8) == nil {
		t.Fatal("old program must not accept schema 9")
	}
	for _, item := range []struct{ kind, key string }{{"vendor", v.ID}, {"app", a.Key}} {
		empty, err := s.AdminNotes(item.kind, item.key)
		if err != nil || empty != (AdminNotes{}) {
			t.Fatal(empty, err)
		}
		if _, err = s.SaveAdminNotes(item.kind, item.key, 0, item.kind+" private\n\ttext"); err != nil {
			t.Fatal(err)
		}
	}
	s.DB.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	for _, item := range []struct{ kind, key string }{{"vendor", v.ID}, {"app", a.Key}} {
		got, err := s.AdminNotes(item.kind, item.key)
		if err != nil || got.Text != item.kind+" private\n\ttext" || got.Revision != 1 {
			t.Fatal(got, err)
		}
	}
	if err = s.PermanentlyDeleteApplication(a.Key, a.Revision); err != nil {
		t.Fatal(err)
	}
	if err = s.PermanentlyDeleteVendor(v.ID, v.Revision); err != nil {
		t.Fatal(err)
	}
	v2, err := s.CreateVendor(VendorInput{ID: v.ID, Name: v.Name})
	if err != nil {
		t.Fatal(err)
	}
	a2, err := s.CreateApplication(v2.ID, ApplicationInput{ID: a.ID, Name: a.Name, Provider: "info"})
	if err != nil {
		t.Fatal(err)
	}
	if v2.UID == v.UID || a2.UID == a.UID {
		t.Fatal("identity reused")
	}
	for _, item := range []struct{ kind, key string }{{"vendor", v2.ID}, {"app", a2.Key}} {
		got, err := s.AdminNotes(item.kind, item.key)
		if err != nil || got != (AdminNotes{}) {
			t.Fatal("notes leaked to recreated identity", got, err)
		}
	}
}

func TestAdminNotesUpgradeDDLFailureRollsBackAndRetries(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(dir, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schemaV8 + `CREATE VIEW application_admin_notes AS SELECT 1;`); err != nil {
		t.Fatal(err)
	}
	if opened, err := Open(dir); err == nil {
		opened.DB.Close()
		t.Fatal("DDL failure ignored")
	}
	if err = checkSchemaDefinition(db, schemaV8, 8); err != nil {
		t.Fatal("failed upgrade changed legacy definition", err)
	}
	var version, count int
	if err = db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil || version != 8 {
		t.Fatal(version, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='vendor_admin_notes'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial table committed", count, err)
	}
	if _, err = db.Exec(`DROP VIEW application_admin_notes`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.DB.Close()
}
