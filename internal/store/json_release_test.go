package store

import (
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/releaseconfig"
	"strings"
	"testing"
)

func jsonConfigFixture() *releaseconfig.Config {
	return &releaseconfig.Config{DefaultChannel: "stable", RefreshSeconds: 300, Channels: map[string]releaseconfig.Channel{"stable": {Query: ".body", Requests: []releaseconfig.Request{{URL: "https://updates.example/api"}}}}, Sources: releaseconfig.Sources{Metadata: []releaseconfig.Source{{Origin: "https://updates.example", Prefix: "/"}}, Artifacts: []releaseconfig.Source{{Origin: "https://files.example", Prefix: "/"}}}}
}
func TestJSONReleaseConfigurationPersistsAndFencesSources(t *testing.T) {
	s := openTest(t)
	if _, err := s.CreateVendor(VendorInput{ID: "editor", Name: LocalizedText{En: "Editor", ZhCN: "编辑器"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	app, err := s.CreateApplication("editor", ApplicationInput{ID: "ide", Name: LocalizedText{En: "IDE", ZhCN: "IDE"}, Provider: "json-release", JSONRelease: jsonConfigFixture(), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if app.JSONRelease == nil || app.BaseURL != "https://updates.example" || app.CacheTTLSeconds != 300 {
		t.Fatal(app)
	}
	source, err := s.Source(app.StorageID())
	if err != nil || source.JSONRelease == nil {
		t.Fatal(source, err)
	}
	oldEpoch := app.SourceEpoch
	config := jsonConfigFixture()
	config.RefreshSeconds = 600
	config.Prewarm = []releaseconfig.PrewarmSelection{{Channel: "stable", Keys: []string{"win-x64-user"}}}
	updated, err := s.UpdateApplication(app.Key, app.Revision, ApplicationChanges{Name: app.Name, JSONRelease: config, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SourceEpoch != oldEpoch || updated.RuntimeRevision <= app.RuntimeRevision {
		t.Fatal("scheduling changed source or failed to fence", updated)
	}
	config.Channels["stable"] = releaseconfig.Channel{Query: ".body.release", Requests: []releaseconfig.Request{{URL: "https://updates.example/api"}}}
	updated, err = s.UpdateApplication(updated.Key, updated.Revision, ApplicationChanges{Name: app.Name, JSONRelease: config, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SourceEpoch != oldEpoch+1 {
		t.Fatal("query did not create source epoch", updated)
	}
	source, err = s.Source(app.StorageID())
	if err != nil || source.JSONRelease.Channels["stable"].Query != ".body" {
		t.Fatal("old source mutated", source, err)
	}
	cfg, err := s.ApplicationConfiguration(app.Key)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(cfg.Effective["json_release"])
	if !strings.Contains(string(raw), ".body.release") {
		t.Fatal(string(raw))
	}
}

func TestJSONReleasePresetsStartDisabled(t *testing.T) {
	s := openTest(t)
	if err := s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	apps, err := s.Applications(false)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, app := range apps {
		if app.Provider != "json-release" {
			continue
		}
		count++
		if app.Enabled || app.JSONRelease == nil || len(app.JSONRelease.Prewarm) != 0 {
			t.Fatal("unverified preset activated", app.Key)
		}
	}
	if count != 12 {
		t.Fatal(count)
	}
}
