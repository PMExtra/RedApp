package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/PMExtra/RedApp/internal/configexchange"
	"github.com/PMExtra/RedApp/presets"
)

func TestProviderScopeResetReopenAndExchange(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	_, err = s.CreateVendor(directoryVendor("microsoft"))
	if err != nil {
		t.Fatal(err)
	}
	input := directoryApplication("vscode")
	input.CacheTTLSeconds = 0
	input.Enabled = false
	before, err := s.CreateApplication("microsoft", input)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := s.Source(before.StorageID())
	set := presets.Embedded()
	if err = s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.ApplicationConfiguration(before.Key)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TemplateProvider == nil || *cfg.TemplateProvider != "json-release" || cfg.Effective["provider"] != "http-cache" || cfg.Fields["cache_ttl_seconds"].Resettable || !cfg.Fields["name.en"].Resettable {
		t.Fatal(cfg)
	}
	if _, ok := cfg.Defaults["json_release"]; ok {
		t.Fatal("foreign options exposed as defaults")
	}
	for _, path := range []string{"json_release", "cache_ttl_seconds", "base_urls"} {
		if _, err = s.PatchApplicationConfiguration(before.Key, ConfigurationPatch{Revision: cfg.Revision, Unset: []string{path}}); !errors.Is(err, ErrInvalidDirectory) {
			t.Fatal(path, err)
		}
	}
	patch(t, s, before.Key, nil, "name.en")
	index := slices.IndexFunc(set.Apps, func(a presets.App) bool { return a.Key() == before.Key })
	set.Apps[index].Spec.Name.En = "Upgraded editor"
	if err = s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Application(before.Key)
	if after.Name.En != "Upgraded editor" || after.CacheTTLSeconds != 0 || after.Enabled || after.Provider != before.Provider || after.SourceEpoch != before.SourceEpoch || after.RuntimeRevision != before.RuntimeRevision || after.UID != before.UID {
		t.Fatal(after)
	}
	current, _ := s.Source(before.StorageID())
	if !reflect.DeepEqual(source, current) {
		t.Fatal("source changed")
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.ApplicationConfiguration(before.Key)
	if cfg.Fields["name.en"].Source != "inherited" {
		t.Fatal("reset refrozen", cfg)
	}
	missing := set
	missing.Apps = slices.Delete(slices.Clone(set.Apps), index, index+1)
	if err = s.ReconcileTemplates(missing); err != nil {
		t.Fatal(err)
	}
	frozen, _ := s.Application(before.Key)
	if frozen.Name != after.Name {
		t.Fatal(frozen)
	}
	if err = s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	pack, err := s.ExportConfiguration(ExportOptions{Selection: []ExportSelection{{Kind: "App", Key: before.Key}}, Mode: "linked"})
	if err != nil {
		t.Fatal(err)
	}
	var doc configexchange.Document
	for _, d := range pack.Documents {
		if d.Kind == "App" {
			doc = d
		}
	}
	if doc.SchemaVersion != 2 || doc.ProviderKey != "http-cache" || doc.Validate() != nil {
		t.Fatal(doc)
	}
	plan, err := s.PreviewConfigurationImport([]configexchange.Document{doc}, []ImportChoice{{Kind: "App", Key: before.Key, Action: "update"}})
	if err != nil || !plan.Ready {
		t.Fatal(plan, err)
	}
	if _, err = s.ExecuteConfigurationImport(plan, "provider-scope", true); err != nil {
		t.Fatal(err)
	}
	restored, _ := s.Application(before.Key)
	if restored.Provider != before.Provider || restored.BaseURL != before.BaseURL || restored.SourceEpoch != before.SourceEpoch || restored.UID != before.UID || restored.Enabled {
		t.Fatal(restored)
	}
	doc.ProviderKey = "json-release"
	if _, err = s.PreviewConfigurationImport([]configexchange.Document{doc}, []ImportChoice{{Kind: "App", Key: before.Key, Action: "update"}}); err == nil {
		t.Fatal("provider switch accepted")
	}
	doc.ProviderKey = ""
	doc.SchemaVersion = 1
	if _, err = s.PreviewConfigurationImport([]configexchange.Document{doc}, []ImportChoice{{Kind: "App", Key: before.Key, Action: "update"}}); err == nil {
		t.Fatal("v1 options reinterpreted under a foreign provider")
	}
}

func TestJSONReleaseBlockReplacesAtomicallyAndResetsSource(t *testing.T) {
	s := openTest(t)
	if err := s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Application("microsoft/vscode")
	local := jsonConfigFixture()
	patch(t, s, before.Key, map[string]any{"json_release": local})
	after, _ := s.Application(before.Key)
	if !bytes.Equal(encode(after.JSONRelease), encode(local)) || after.SourceEpoch != before.SourceEpoch+1 {
		t.Fatal(after)
	}
	old, err := s.Source(before.StorageID())
	if err != nil || old.Active || old.JSONRelease == nil {
		t.Fatal(old, err)
	}
	cfg := patch(t, s, before.Key, nil, "json_release")
	reset, _ := s.Application(before.Key)
	if reset.SourceEpoch != after.SourceEpoch+1 || !reflect.DeepEqual(reset.JSONRelease, before.JSONRelease) || cfg.Fields["json_release"].Source != "inherited" {
		t.Fatal(reset, cfg)
	}
	// A partial structured block cannot borrow sources/channels from the template.
	raw := json.RawMessage(`{"refresh_seconds":600}`)
	if _, err = s.PatchApplicationConfiguration(before.Key, ConfigurationPatch{Revision: reset.Revision, Set: map[string]json.RawMessage{"json_release": raw}}); err == nil {
		t.Fatal("partial provider block merged")
	}
}

func TestAbsentFieldsInheritButExplicitValuesRemain(t *testing.T) {
	s := openTest(t)
	s.CreateVendor(directoryVendor("microsoft"))
	app, err := s.CreateApplication("microsoft", ApplicationInput{ID: "vscode", Name: LocalizedText{En: "Local", ZhCN: "本地"}, Provider: "json-release", JSONRelease: jsonConfigFixture()})
	if err != nil {
		t.Fatal(err)
	}
	// Model a supported historic independent spec that omitted display fields.
	err = s.writeConfiguration(func(w *configSet) error {
		a, e := w.appByKey(app.Key)
		if e != nil {
			return e
		}
		delete(a.config.Spec, "description")
		a.changed = true
		a.Revision++
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.ApplicationConfiguration(app.Key)
	if cfg.Fields["description.en"].Source != "inherited" || cfg.Fields["name.en"].Source != "custom" || cfg.Effective["name"].(map[string]any)["en"] != "Local" {
		t.Fatal(cfg)
	}
}
