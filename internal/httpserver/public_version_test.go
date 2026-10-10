package httpserver

import (
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/presets"
	"net/url"
	"testing"
	"time"
)

func TestPublicKnownVersionUsesProtocolOrderAndFirstDiscovery(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	for _, key := range []string{"openai/codex", "anthropic/claude-code"} {
		entry, _ := h.server.Registry.Lookup(key)
		for version, first := range map[string]int64{"1.9.0": 200, "1.10.0": 100, "invalid": 300} {
			if _, err := h.server.DB.DB.Exec("INSERT INTO app_versions(app_id,version,first_seen_s) VALUES(?,?,?)", entry.StorageID(), version, first); err != nil {
				t.Fatal(err)
			}
		}
		if err := h.server.DB.SeenFor(entry.StorageID(), "1.10.0"); err != nil {
			t.Fatal(err)
		}
		item, err := h.server.publicApplication(entry, "http://local")
		if err != nil {
			t.Fatal(err)
		}
		latest := item["latest_known_version"].(map[string]any)
		if latest["version"] != "1.10.0" || latest["first_seen"].(*time.Time).Unix() != 100 {
			t.Fatal(latest)
		}
		entry.SourceEpoch++
		item, err = h.server.publicApplication(entry, "http://local")
		if err != nil {
			t.Fatal(err)
		}
		if item["latest_known_version"] != nil {
			t.Fatal("previous source leaked", item)
		}
		entry.SourceEpoch--
		if _, err := h.server.DB.DB.Exec("INSERT INTO app_versions(app_id,version,first_seen_s) VALUES(?,?,0)", entry.StorageID(), "2.0.0"); err != nil {
			t.Fatal(err)
		}
		item, err = h.server.publicApplication(entry, "http://local")
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(item["latest_known_version"])
		if string(encoded) != `{"first_seen":null,"version":"2.0.0"}` {
			t.Fatal(string(encoded))
		}
		entry.Provider = application.Info
		entry.Protocol = nil
		item, err = h.server.publicApplication(entry, "http://local")
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := item["latest_known_version"]; exists {
			t.Fatal("non-version provider has version metadata")
		}
	}
	data, _ := h.request("GET", "/api/bootstrap", nil, 200, nil)
	var body struct {
		Apps []struct {
			Latest *struct {
				Version string `json:"version"`
			} `json:"latest_known_version"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	for _, app := range body.Apps {
		if app.Latest == nil || app.Latest.Version != "2.0.0" {
			t.Fatal(string(data))
		}
	}
}

func TestReviewedAnthropicIconsAndDisabledApplication(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	vendorIcon, appIcon := presets.ImagePrefix+"builtin/anthropic.svg", presets.ImagePrefix+"anthropic/claude-code/icon.svg"
	_, headers := h.request("GET", vendorIcon, nil, 200, nil)
	if headers.Get("Content-Type") != "image/svg+xml" {
		t.Fatal(headers)
	}
	h.request("GET", presets.ImagePrefix+"builtin/missing.svg", nil, 404, nil)
	h.login(h.password)
	v, _ := h.server.DB.Vendor("anthropic")
	h.request("PATCH", "/admin/api/vendors/anthropic", map[string]any{"revision": v.Revision, "enabled": false, "icon": vendorIcon}, 200, nil)
	// Reviewed images do not depend on the state of the vendor using them.
	h.request("GET", appIcon, nil, 200, nil)
	h.request("GET", "/admin/api/assets/builtin-icon?path="+url.QueryEscape(appIcon), nil, 200, nil)
}
