package site

import (
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/store"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultsPartialRecordsAndRestart(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := LoadSnapshot(db)
	if err != nil || !reflect.DeepEqual(initial.Settings, Defaults()) {
		t.Fatal(initial, err)
	}
	if initial.Revision != 1 {
		t.Fatal("initial revision", initial.Revision)
	}
	if _, err = db.SaveSiteSettings(initial.Revision, map[string]any{"title": map[string]string{"en": "Company tools"}}); err != nil {
		t.Fatal(err)
	}
	partial, err := LoadSnapshot(db)
	if err != nil || partial.Title.EN != "Company tools" || partial.Title.ZHCN != initial.Title.ZHCN || partial.Subtitle != initial.Subtitle {
		t.Fatal(partial, err)
	}
	partial.Disclaimer.EN = "<img src=x onerror=alert(1)>"
	partial.Subtitle.EN = ""
	if partial, err = SaveCAS(db, partial.Settings, partial.Revision); err != nil {
		t.Fatal(err)
	}
	db.DB.Close()
	db, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	got, err := LoadSnapshot(db)
	if err != nil || !reflect.DeepEqual(got, partial) {
		t.Fatal(got, err)
	}
	if _, err = SaveCAS(db, got.Settings, got.Revision-1); err == nil {
		t.Fatal("stale revision overwrote site settings")
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "<img") {
		t.Fatal("JSON failed to escape markup")
	}
	for _, value := range []string{"", strings.Repeat("x", 81), "bad\x00text", string([]byte{0xff})} {
		bad := got
		bad.Title.EN = value
		if _, err := SaveCAS(db, bad.Settings, bad.Revision); err == nil {
			t.Fatal("invalid title accepted")
		}
	}
	after, _ := LoadSnapshot(db)
	if !reflect.DeepEqual(after, got) {
		t.Fatal("invalid update changed stored settings")
	}
	if _, err = db.SaveSiteSettings(got.Revision, map[string]any{"title": false}); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadSnapshot(db); err == nil {
		t.Fatal("corrupt settings silently accepted")
	}
}
