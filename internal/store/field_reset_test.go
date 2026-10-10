package store

import (
	"errors"
	"testing"
)

func TestFieldResetPreservesOtherLanguageAndOwnedData(t *testing.T) {
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
