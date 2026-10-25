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
	db.Close()
	db, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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

func TestNavigationValidationAndPersistence(t *testing.T) {
	for _, raw := range []string{"/legacy/", "/tools/a.zip?x=1#download", "https://example.com/files", "http://localhost:8080/"} {
		if !ValidNavigationURL(raw) {
			t.Errorf("valid URL rejected: %q", raw)
		}
	}
	for _, raw := range []string{"", "javascript:alert(1)", "data:text/html,test", "//evil.test", "/%2fevil.test", "/\\evil.test", "/%5cevil.test", "https://user:pass@example.com", "https:example.com", "https:///example.com", " /legacy", "/bad\npath", "/bad%0apath", "/bad path", "/bad%20path", "https://example.com:bad", "https://", "relative/path", "/bad%zz"} {
		if ValidNavigationURL(raw) {
			t.Errorf("unsafe URL accepted: %q", raw)
		}
	}
	directory := t.TempDir()
	db, err := store.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	before, err := LoadSnapshot(db)
	if err != nil {
		t.Fatal(err)
	}
	before.Navigation = []NavigationItem{{Title: "  旧版资源  ", URL: "/legacy/"}, {Title: "Docs", LocalizedTitle: &Text{EN: " Documentation ", ZHCN: " 文档 "}, URL: "https://example.com/", NewTab: true}}
	saved, err := SaveCAS(db, before.Settings, before.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	loaded, err := LoadSnapshot(db)
	if err != nil || !reflect.DeepEqual(loaded, saved) || loaded.Navigation[0].Title != "旧版资源" || loaded.Navigation[1].LocalizedTitle.EN != "Documentation" || loaded.Navigation[1].LocalizedTitle.ZHCN != "文档" {
		t.Fatalf("navigation persistence: %+v %v", loaded, err)
	}
	for _, items := range [][]NavigationItem{{{Title: "", URL: "/"}}, {{Title: "bad\nname", URL: "/"}}, {{Title: "safe", LocalizedTitle: &Text{EN: "bad\nname"}, URL: "/"}}, {{Title: "unsafe", URL: "javascript:alert(1)"}}, make([]NavigationItem, 21)} {
		invalid := saved.Settings
		invalid.Navigation = items
		if _, err := SaveCAS(db, invalid, saved.Revision); err == nil {
			t.Fatal("invalid navigation accepted")
		}
	}
	after, err := LoadSnapshot(db)
	if err != nil || !reflect.DeepEqual(after, saved) {
		t.Fatal("invalid save changed settings")
	}
	saved.Navigation = []NavigationItem{saved.Navigation[1], saved.Navigation[0]}
	reordered, err := SaveCAS(db, saved.Settings, saved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if reordered.Navigation[0].Title != "Docs" {
		t.Fatal("order lost")
	}
	reordered.Navigation = nil
	cleared, err := SaveCAS(db, reordered.Settings, reordered.Revision)
	if err != nil || len(cleared.Navigation) != 0 || cleared.Navigation == nil {
		t.Fatal("clear must produce empty array")
	}
}
