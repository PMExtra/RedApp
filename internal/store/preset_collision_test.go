package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/PMExtra/RedApp/presets"
)

func TestNewPresetPreservesIndependentAppAndSources(t *testing.T) {
	for _, provider := range []string{"info", "http-cache", "json-release"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%t", provider, enabled), func(t *testing.T) {
				s := openTest(t)
				_, err := s.CreateVendor(VendorInput{ID: "microsoft", Name: LocalizedText{En: "Local vendor", ZhCN: "本地"}, Enabled: enabled})
				if err != nil {
					t.Fatal(err)
				}
				in := ApplicationInput{ID: "vscode", Name: LocalizedText{En: "Local editor", ZhCN: "本地编辑器"}, Provider: provider, Enabled: enabled}
				if provider == "http-cache" {
					in.BaseURLs = []string{"https://local.example/one", "https://local.example/two"}
					in.SourceStrategy = "round_robin"
					in.CacheTTLSeconds = 0
				}
				if provider == "json-release" {
					in.JSONRelease = jsonConfigFixture()
				}
				before, err := s.CreateApplication("microsoft", in)
				if err != nil {
					t.Fatal(err)
				}
				cfgBefore, err := s.ApplicationConfiguration(before.Key)
				if err != nil {
					t.Fatal(err)
				}
				sourceBefore, sourceErr := s.Source(before.StorageID())
				if provider == "info" {
					if !errors.Is(sourceErr, sql.ErrNoRows) {
						t.Fatal("unexpected content source result", sourceErr)
					}
				} else if sourceErr != nil {
					t.Fatal(sourceErr)
				}
				set := presets.Embedded()
				for pass := 0; pass < 2; pass++ {
					if pass == 1 {
						for i := range set.Apps {
							if set.Apps[i].Key() == before.Key {
								set.Apps[i].Spec.Name.En = "Changed upstream"
								set.Apps[i].Spec.JSONRelease.RefreshSeconds = 777
							}
						}
					}
					if err = s.ReconcileTemplates(set); err != nil {
						t.Fatal(err)
					}
					after, err := s.Application(before.Key)
					if err != nil {
						t.Fatal(err)
					}
					if !after.BuiltinTemplate {
						t.Fatal("expected path protection flag")
					}
					after.BuiltinTemplate = before.BuiltinTemplate
					after.Revision = before.Revision
					if !reflect.DeepEqual(after, before) {
						t.Fatalf("independent app changed: before=%+v after=%+v", before, after)
					}
					cfg, err := s.ApplicationConfiguration(before.Key)
					if err != nil {
						t.Fatal(err)
					}
					if cfg.TemplateRef == nil || *cfg.TemplateRef != before.Key || cfg.Defaults == nil || !reflect.DeepEqual(cfg.Effective, cfgBefore.Effective) {
						t.Fatal("association changed effective configuration", cfg)
					}
					src, err := s.Source(before.StorageID())
					if sourceErr == nil {
						if err != nil || !reflect.DeepEqual(src, sourceBefore) {
							t.Fatal("source changed", src, err)
						}
					} else if !errors.Is(err, sql.ErrNoRows) {
						t.Fatal("unexpected content source result", err)
					}
				}
				current, _ := s.Application(before.Key)
				if _, err = s.PatchApplicationConfiguration(before.Key, ConfigurationPatch{Revision: current.Revision, Unset: []string{"name.en"}}); err != nil {
					t.Fatal(err)
				}
				current, _ = s.Application(before.Key)
				if current.Name.En != "Changed upstream" || current.Provider != provider {
					t.Fatal(current)
				}

				if provider != "json-release" {
					b, err := json.Marshal(jsonConfigFixture())
					if err != nil {
						t.Fatal(err)
					}
					if _, err = s.PatchApplicationConfiguration(before.Key, ConfigurationPatch{Revision: current.Revision, Set: map[string]json.RawMessage{"json_release": b}}); !errors.Is(err, ErrInvalidDirectory) {
						t.Fatal("foreign provider config accepted", err)
					}
					in.ID = "incompatible-copy"
					ref := "microsoft/vscode"
					copy, err := s.CreateConfiguredApplication("microsoft", in, &ref)
					if err != nil || copy.Provider != provider || copy.JSONRelease != nil {
						t.Fatal(copy, err)
					}
					if provider == "http-cache" && !reflect.DeepEqual(copy.BaseURLs, in.BaseURLs) {
						t.Fatal("explicit local sources lost", copy)
					}
				}
			})
		}
	}
}

func TestPresetMatchingIgnoresDisplayName(t *testing.T) {
	s := openTest(t)
	if _, err := s.CreateVendor(directoryVendor("local")); err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateApplication("local", ApplicationInput{ID: "editor", Name: LocalizedText{En: "Visual Studio Code", ZhCN: "Visual Studio Code"}, Provider: "info", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	after, err := s.Application(a.Key)
	if err != nil || !reflect.DeepEqual(a, after) {
		t.Fatal(after, err)
	}
	preset, err := s.Application("microsoft/vscode")
	if err != nil || preset.UID == a.UID || preset.Enabled || preset.Provider != "json-release" {
		t.Fatal(preset, err)
	}
}
