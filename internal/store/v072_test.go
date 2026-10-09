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
)

func TestV072TemplateInsertionProtectionAndSelectiveCAS(t *testing.T) {
	s := openTest(t)
	v, err := s.CreateVendor(VendorInput{ID: "openai", Name: LocalizedText{"Custom OpenAI", "自定义"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateApplication(v.ID, ApplicationInput{ID: "codex", Name: LocalizedText{"My info", "我的介绍"}, Provider: "info", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveInstructions(a.Key, 0, LocalizedText{}); err != nil {
		t.Fatal(err)
	}
	a, _ = s.Application(a.Key)
	if err = s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	same, _ := s.Application(a.Key)
	if same.UID != a.UID || same.Provider != "info" || same.Name != a.Name || same.Revision != a.Revision || !same.Enabled {
		t.Fatal("existing template key overwritten", same)
	}
	blank, _ := s.Instructions(a.UID)
	if blank.Revision != 0 || blank.En != "" {
		t.Fatal("explicit blank replaced", blank)
	}
	claude, _ := s.Application("anthropic/claude-code")
	vendor, _ := s.Vendor("anthropic")
	if claude.Enabled || vendor.Enabled {
		t.Fatal("new templates must start disabled")
	}
	if err = s.PermanentlyDeleteApplication(a.Key, a.Revision); !errors.Is(err, ErrBuiltinTemplate) {
		t.Fatal("provider mismatch bypassed protection", err)
	}
	if err = s.PermanentlyDeleteVendor(v.ID, v.Revision); !errors.Is(err, ErrVendorHasApplications) {
		t.Fatal("vendor bypassed protection", err)
	}
	// Field-level reset is a configuration unset; an independent entity has nothing to inherit.
	if _, err = s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: a.Revision, Unset: []string{"name.en", "instructions.en"}}); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatal("independent entity must not acquire inheritance through reset", err)
	}
	after, _ := s.Application(a.Key)
	if after.Provider != a.Provider || after.SourceEpoch != a.SourceEpoch || after.Enabled != a.Enabled || after.UID != a.UID {
		t.Fatal("unselected fields changed", after)
	}
	blank, _ = s.Instructions(a.UID)
	if blank.En != "" || blank.Revision != 0 {
		t.Fatal("unselected instructions changed")
	}
	if err = s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Application(a.Key)
	if !reflect.DeepEqual(again, after) {
		t.Fatal("restart rewrote existing template")
	}
}
func TestV072RankingMergesClientsAndExpiresBuckets(t *testing.T) {
	s := openTest(t)
	s.CreateVendor(directoryVendor("acme"))
	a, err := s.CreateApplication("acme", directoryApplication("tool"))
	if err != nil {
		t.Fatal(err)
	}
	// Fixed fixture salt makes approximation bounds reproducible; production salts remain random.
	if _, err = s.DB.Exec(`UPDATE catalog_state SET ranking_salt=zeroblob(32)`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	var firstDay int64
	for day := 6; day >= 0; day-- {
		for ip := 1; ip <= 10; ip++ {
			if err = s.RecordDownload(a.UID, fmt.Sprintf("192.0.2.%d", ip), now.Add(-time.Duration(day)*24*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
		if day == 6 {
			first, e := s.DownloadRanking(now, 20)
			if e != nil || len(first) != 1 {
				t.Fatal(first, e)
			}
			firstDay = first[0].Clients
		}
	}
	s.RecordDownload(a.UID, "::ffff:192.0.2.1", now)
	scores, err := s.DownloadRanking(now, 20)
	if err != nil || len(scores) != 1 || scores[0].Clients != firstDay || firstDay < 8 || firstDay > 12 {
		t.Fatal("daily cardinalities were added instead of merged", scores, err)
	}
	var bytesStored int
	if err = s.DB.QueryRow(`SELECT sum(length(registers)) FROM download_sketches`).Scan(&bytesStored); err != nil || bytesStored != 7*1024 {
		t.Fatal("unbounded sketches", bytesStored, err)
	}
	scores, err = s.DownloadRanking(now.Add(7*24*time.Hour), 20)
	if err != nil || len(scores) != 0 {
		t.Fatal("expired clients remain ranked", scores, err)
	}
	for i := 0; i < 5000; i++ {
		if err = s.RecordDownload(a.UID, fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256), now); err != nil {
			t.Fatal(err)
		}
	}
	scores, _ = s.DownloadRanking(now, 20)
	if scores[0].Clients < 4500 || scores[0].Clients > 5500 {
		t.Fatal("invalid estimate", scores)
	}
	if _, err = s.DB.Exec(`UPDATE vendors SET enabled=0`); err != nil {
		t.Fatal(err)
	}
	scores, _ = s.DownloadRanking(now, 20)
	if len(scores) != 0 {
		t.Fatal("disabled vendor ranked")
	}
}
func TestV072PermanentRemovalIsScopedAndRestartable(t *testing.T) {
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
	if _, err = s.DB.Exec(`INSERT INTO hosted_files VALUES(?,'file',?,'hash',7,1)`, a.UID, id); err != nil {
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
	for _, table := range []string{"metric_counters", "events", "settings"} {
		var n int
		s.DB.QueryRow(`SELECT count(*) FROM `+table+` WHERE app_id=?`, a.MetricsID()).Scan(&n)
		if n != 0 {
			t.Fatal("private data recreated", table)
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

func TestV072CompatibleResetPreservesOtherLanguageAndOwnedData(t *testing.T) {
	s := openTest(t)
	if err := s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Vendor("openai")
	v, err := s.UpdateVendor(v.ID, v.Revision, VendorChanges{Name: LocalizedText{En: "Custom", ZhCN: "自定义"}, Description: v.Description, Icon: v.Icon, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchVendorConfiguration(v.ID, ConfigurationPatch{Revision: v.Revision - 1, Unset: []string{"name.en"}}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err = s.PatchVendorConfiguration(v.ID, ConfigurationPatch{Revision: v.Revision, Unset: []string{"name.en", "name.zh-CN"}}); err != nil {
		t.Fatal(err)
	}
	resetVendor, _ := s.Vendor(v.ID)
	if !resetVendor.Enabled || resetVendor.UID != v.UID || resetVendor.ID != v.ID || resetVendor.Name == v.Name {
		t.Fatal("vendor reset changed unselected state", resetVendor)
	}
	a, _ := s.Application("openai/codex")
	a, err = s.UpdateApplication(a.Key, a.Revision, ApplicationChanges{Name: a.Name, Description: a.Description, Icon: a.Icon, Enabled: true, BaseURL: a.BaseURL + "/custom", CacheTTLSeconds: 123})
	if err != nil {
		t.Fatal(err)
	}
	instructions, _ := s.Instructions(a.UID)
	instructions, err = s.SaveInstructions(a.Key, instructions.Revision, LocalizedText{En: "Custom English", ZhCN: "保留中文"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AddFor(a.MetricsID(), "artifact_requests", 7); err != nil {
		t.Fatal(err)
	}
	resource := releaseFixture(t, s, a.StorageID(), "1.0.0")
	a, _ = s.Application(a.Key)
	if _, err = s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: a.Revision, Unset: []string{"cache_ttl_seconds", "instructions.en"}}); err != nil {
		t.Fatal(err)
	}
	reset, _ := s.Application(a.Key)
	if reset.CacheTTLSeconds != 60 || reset.BaseURL != a.BaseURL || reset.SourceEpoch != a.SourceEpoch || reset.Enabled != a.Enabled || reset.UID != a.UID {
		t.Fatal("reset changed unselected configuration", reset)
	}
	updated, _ := s.Instructions(a.UID)
	if updated.ZhCN != "保留中文" || updated.En == instructions.En || updated.Revision != instructions.Revision+1 {
		t.Fatal(updated)
	}
	if _, err = s.Release(resource.AppID, resource.Version); err != nil {
		t.Fatal("reset deleted stored release", err)
	}
	counts, _ := s.CountersFor(a.MetricsID())
	if counts["artifact_requests"] != 7 {
		t.Fatal("reset removed history", counts)
	}
}
