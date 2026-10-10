package httpserver

import (
	"encoding/json"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/presets"
)

func TestPublicKnownVersionUsesProtocolOrderAndFirstDiscovery(t *testing.T) {
	h := newHarness(t)
	for _, key := range []string{"openai/codex", "anthropic/claude-code"} {
		entry, _ := h.server.registry.Lookup(key)
		for version, first := range map[string]int64{"1.9.0": 200, "1.10.0": 100, "invalid": 300} {
			if _, err := h.server.store.DB.Exec("INSERT INTO app_versions(app_id,version,first_seen_s) VALUES(?,?,?)", entry.StorageID(), version, first); err != nil {
				t.Fatal(err)
			}
		}
		if err := h.server.store.SeenFor(entry.StorageID(), "1.10.0"); err != nil {
			t.Fatal(err)
		}
		item, err := h.server.publicApp(entry, "http://local", nil)
		if err != nil {
			t.Fatal(err)
		}
		if latest := item.LatestKnownVersion; latest == nil || latest.Version != "1.10.0" || latest.FirstSeen.Unix() != 100 {
			t.Fatal(latest)
		}
		before := item.Revision
		entry.SourceEpoch++
		if item, err = h.server.publicApp(entry, "http://local", nil); err != nil || item.LatestKnownVersion != nil {
			t.Fatal("previous source leaked", item, err)
		}
		entry.SourceEpoch--
		if _, err := h.server.store.DB.Exec("INSERT INTO app_versions(app_id,version,first_seen_s) VALUES(?,?,0)", entry.StorageID(), "2.0.0"); err != nil {
			t.Fatal(err)
		}
		if item, err = h.server.publicApp(entry, "http://local", nil); err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(item.LatestKnownVersion)
		if string(encoded) != `{"version":"2.0.0","first_seen":null}` || item.Revision == before {
			t.Fatal("latest version or document revision", string(encoded), item.Revision)
		}
		entry.Provider = application.Info
		entry.Protocol = nil
		if item, err = h.server.publicApp(entry, "http://local", nil); err != nil || item.LatestKnownVersion != nil {
			t.Fatal("non-version provider has version metadata", item)
		}
	}
	_, apps := h.publicCatalog()
	for _, key := range []string{"openai/codex", "anthropic/claude-code"} {
		if latest := apps[key].LatestKnownVersion; latest == nil || latest.Version != "2.0.0" {
			t.Fatal(key, latest)
		}
	}
}

func TestReviewedAnthropicIconsAndDisabledApplication(t *testing.T) {
	h := newHarness(t)
	vendorIcon, appIcon := presets.ImagePrefix+"builtin/anthropic.svg", presets.ImagePrefix+"anthropic/claude-code/icon.svg"
	_, headers := h.request("GET", vendorIcon, nil, 200, nil)
	if headers.Get("Content-Type") != "image/svg+xml" {
		t.Fatal(headers)
	}
	h.request("GET", presets.ImagePrefix+"builtin/missing.svg", nil, 404, nil)
	h.login(h.password)
	h.patchConfiguration("vendors/anthropic", map[string]any{"set": map[string]any{"icon": vendorIcon}}, 200)
	h.setEnabled("vendors/anthropic", false)
	// Reviewed images do not depend on the state of the vendor using them.
	h.request("GET", appIcon, nil, 200, nil)
	h.request("GET", vendorIcon, nil, 200, nil)
}
