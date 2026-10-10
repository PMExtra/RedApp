package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/store"
)

func TestDeploymentIsStrictAndValidationDoesNotTouchData(t *testing.T) {
	t.Setenv("REDAPP_PUBLIC_URL", "https://Downloads.Example.com/")
	dir := t.TempDir()
	data := filepath.Join(dir, "uncreated")
	// YAML flow syntax keeps the field edits below simple.
	path := filepath.Join(dir, "config.yaml")
	base := `{"schema_version":1,"data_dir":` + quoted(data) + `}`
	if err := os.WriteFile(path, []byte(base), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, nil)
	if err != nil || c.Listen != ":8080" || c.DownloadLimits.MaxWriters != 16 || c.DownloadLimits.MaxDownloadsPerClient != DefaultMaxDownloadsPerClient || c.EnvironmentPublicURL != "https://downloads.example.com" {
		t.Fatal(c, err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatal("validation touched the data directory")
	}
	for _, change := range []string{
		`"unknown":true`, `"Schema_version":1`, `"schema_version":2`, `"schema_version":null`,
		`"download_limits":{"max_readers":0}`, `"download_limits":{"max_readers":1,"max_readers":2}`,
		`"download_limits":{"max_readers":null}`, `"download_limits":{"MaxReaders":5}`,
		`"download_limits":{"max_downloads_per_client":0}`, `"download_limits":{"max_downloads_per_client":65537}`,
		`"allowed_hosts":["downloads.example.com"]`, `"download_limits":{"max_active_writers":16}`,
		`"listen":"localhost:0"`, `"trusted_proxies":["host"]`,
	} {
		// Replace top-level keys so range/type failures are not accidentally
		// covered only by duplicate-key rejection.
		var fields map[string]json.RawMessage
		json.Unmarshal([]byte(base), &fields)
		key := strings.SplitN(change, ":", 2)[0]
		delete(fields, strings.Trim(key, `"`))
		rawBase, _ := json.Marshal(fields)
		raw := strings.TrimSuffix(string(rawBase), "}") + "," + change + "}"
		os.WriteFile(path, []byte(raw), 0600)
		if _, err := Load(path, nil); err == nil {
			t.Errorf("invalid configuration accepted: %s", change)
		}
	}
	os.WriteFile(path, []byte(base), 0600)
	t.Setenv("REDAPP_PUBLIC_URL", "https://example.com/subpath")
	if _, err := Load(path, nil); err == nil {
		t.Fatal("invalid environment origin accepted")
	}
}

func quoted(value string) string { raw, _ := json.Marshal(value); return string(raw) }

func TestPublicURLPrecedenceClearCASAndRestart(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := LoadPublicSettings(db, "https://env.example")
	if err != nil {
		t.Fatal(err)
	}
	view := p.View("http://request.example")
	if view.Source != "environment" || view.Revision != 1 {
		t.Fatal(view)
	}
	override := "https://override.example/"
	if _, err = p.Set(&override, 1); err != nil {
		t.Fatal(err)
	}
	view = p.View("http://request.example")
	if view.EffectiveURL != "https://override.example" || view.Source != "override" || view.Revision != 2 {
		t.Fatal(view)
	}
	// Callers cannot mutate the stored snapshot through returned pointers.
	*view.OverrideURL = "http://changed.example"
	if _, err = p.Set(nil, 1); err == nil {
		t.Fatal("stale clear accepted")
	}
	if p.View("http://request.example").EffectiveURL != "https://override.example" {
		t.Fatal("failed update changed snapshot")
	}
	if _, err = p.Set(nil, 2); err != nil {
		t.Fatal(err)
	}
	p, err = LoadPublicSettings(db, "https://env.example")
	if err != nil {
		t.Fatal(err)
	}
	view = p.View("http://request.example")
	if view.Source != "environment" || view.OverrideURL != nil || view.Revision != 3 {
		t.Fatal(view)
	}
	p, err = LoadPublicSettings(db, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.View("http://one.example").EffectiveURL != "http://one.example" || p.View("https://two.example").EffectiveURL != "https://two.example" {
		t.Fatal("request fallback cached across hosts")
	}
	db.Close()
	if _, err = p.Set(&override, 3); err == nil || p.View("http://one.example").Source != "request" {
		t.Fatal("failed persistence became effective")
	}
}
