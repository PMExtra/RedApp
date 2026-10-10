package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPermanentDeleteIsScopedAndRestartable(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	s.CreateVendor(directoryVendor("acme"))
	a, err := s.CreateApplication("acme", ApplicationInput{ID: "files", Name: LocalizedText{"Files", "文件"}, Provider: "hosted", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := s.CreateApplication("acme", directoryApplication("codex"))
	if err != nil {
		t.Fatal(err)
	}
	s.AddFor(a.MetricsID(), "artifact_requests", 4)
	s.AddFor(sibling.MetricsID(), "artifact_requests", 7)
	s.SaveInstructions(a.Key, 0, LocalizedText{"remove me", "移除"})
	a, _ = s.Application(a.Key)
	s.SaveHomepagePins(HomepagePins{Keys: []string{a.Key, sibling.Key}})
	id := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	path := filepath.Join(dir, "objects", "hosted", id)
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("fixture"), 0600)
	if _, err = s.DB.Exec(`INSERT INTO hosted_files(app_uid,path,id,sha256,size_bytes,created_at_s) VALUES(?,'file',?,'hash',7,1)`, a.UID, id); err != nil {
		t.Fatal(err)
	}
	if err = s.PermanentlyDeleteApplication(a.Key, a.Revision+1); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err = s.PermanentlyDeleteApplication(a.Key, a.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Application(a.Key); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("record still exists", err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("transaction unexpectedly touched files before cleanup")
	}
	if err = s.ProcessPendingDeletes(dir); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned body survived cleanup", err)
	}
	s.AddFor(a.MetricsID(), "download_success", 1)
	s.RecordEvent(Event{AppID: a.MetricsID(), Category: "fixture", Code: "late", Message: "late"})
	if err = s.FlushCounters(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"metric_counters", "events"} {
		var n int
		if err = s.DB.QueryRow(`SELECT count(*) FROM `+table+` WHERE app_id=?`, a.MetricsID()).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: %d private rows recreated after deletion: %v", table, n, err)
		}
	}
	// UID-owned rows go with the application through ON DELETE CASCADE.
	for _, table := range []string{"application_config", "application_instructions", "hosted_files", "homepage_pins"} {
		column := "app_uid"
		if table == "application_config" {
			column = "entity_uid"
		}
		var n int
		if err = s.DB.QueryRow(`SELECT count(*) FROM `+table+` WHERE `+column+`=?`, a.UID).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s kept %d rows of the deleted application: %v", table, n, err)
		}
	}
	counts, _ := s.CountersFor(sibling.MetricsID())
	if counts["artifact_requests"] != 7 {
		t.Fatal("sibling changed", counts)
	}
	pins, _ := s.HomepagePins()
	if !reflect.DeepEqual(pins.Keys, []string{sibling.Key}) {
		t.Fatal(pins)
	}
	recreated, err := s.CreateApplication("acme", ApplicationInput{ID: a.ID, Name: a.Name, Provider: a.Provider})
	if err != nil || recreated.UID == a.UID {
		t.Fatal("public key not reusable with new private identity", err)
	}
}
