package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/store"
)

// Two real instances: B mirrors applications of A over HTTP.
func TestMirrorApplicationsOfAnotherInstance(t *testing.T) {
	a := newHarness(t)
	b := newHarness(t)
	aID, _ := a.store.InstanceUID()
	bID, _ := b.store.InstanceUID()

	// A publishes a hosted application with listings and a Codex release.
	a.createVendor("acme")
	files := a.createApp("acme", "files", application.Hosted, nil)
	a.patchApp(files.Key, map[string]any{"auto_index": true, "instructions.en": "Get {{app_url}}/tool.bin", "description.en": "Shared files"})
	a.login("")
	a.upload(files.Key, "tool.bin", "", "tool bytes", 201)
	archive := []byte("codex archive")
	// The release fixture is the proxy of A's "fixture" vendor only, so that
	// A reaches B directly.
	proxy := httptest.NewServer(codexRelease("0.159.2", map[string][]byte{"archive.tgz": archive}, nil))
	t.Cleanup(proxy.Close)
	codexKey := a.releaseApp("fixture", "codex", "codex")
	vendor, _ := a.store.Vendor("fixture")
	if _, err := a.store.PatchVendorConfiguration("fixture", store.ConfigurationPatch{Revision: vendor.Revision, Set: map[string]json.RawMessage{"proxy": encodeJSON(networkproxy.Config{Mode: "url", URL: proxy.URL})}}); err != nil {
		t.Fatal(err)
	}
	a.createApp("acme", "feed", application.JSONRelease, map[string]any{"json_release": map[string]any{
		"default_channel": "stable", "refresh_seconds": 60,
		"channels": map[string]any{"stable": map[string]any{"requests": []any{map[string]any{"url": "https://feed.example/meta"}}, "query": ".body"}},
		"sources":  map[string]any{"metadata": []any{map[string]any{"origin": "https://feed.example", "prefix": "/"}}, "artifacts": []any{map[string]any{"origin": "https://feed.example", "prefix": "/files/"}}},
	}})

	b.login("")
	b.expectError("POST", "/admin/api/upstreams", map[string]string{"name": "Self", "origin": b.http.URL}, 508, codeLoopDetected, nil)
	b.expectError("POST", "/admin/api/upstreams", map[string]string{"name": "Bad", "origin": b.http.URL + "/path"}, 400, codeValidationFailed, nil)
	body, headers := b.request("POST", "/admin/api/upstreams", map[string]string{"name": "DMZ", "origin": a.http.URL + "/"}, 201, nil)
	up := decodeJSONBody[upstreamDTO](t, body)
	if up.InstanceID != aID || up.Origin != a.http.URL || headers.Get("Location") != "/admin/api/upstreams/"+up.UID {
		t.Fatalf("%+v %v", up, headers)
	}
	b.expectError("POST", "/admin/api/upstreams", map[string]string{"name": "Again", "origin": a.http.URL}, 409, codeAlreadyExists, nil)

	catalog := getJSON[pageDTO[upstreamCatalogItemDTO]](b, "/admin/api/upstreams/"+up.UID+"/catalog?limit=100")
	supported := map[string]bool{}
	for _, item := range catalog.Items {
		supported[item.Key] = item.Supported
	}
	if !supported["acme/files"] || !supported[codexKey] || supported["acme/feed"] {
		t.Fatalf("catalog support %+v", supported)
	}

	// A hosted application becomes an HTTP cache that follows A.
	body, _ = b.request("POST", "/admin/api/upstreams/"+up.UID+"/mirrors", map[string]string{"remote_key": "acme/files", "vendor": "mirror", "app": "files"}, 201, nil)
	mirrored := decodeJSONBody[appDTO](t, body)
	if mirrored.Provider != "http-cache" || mirrored.Key != "mirror/files" {
		t.Fatalf("%+v", mirrored)
	}
	b.expectError("POST", "/admin/api/upstreams/"+up.UID+"/mirrors", map[string]string{"remote_key": "acme/files", "vendor": "mirror", "app": "again"}, 409, codeAlreadyExists, nil)
	if data, _ := b.request("GET", "/mirror/files/tool.bin", nil, 200, nil); string(data) != "tool bytes" {
		t.Fatal("file through the mirror", string(data))
	}
	listing := decodeIndex(t, mustBody(b.request("GET", "/mirror/files/", nil, 200, map[string]string{"Accept": "application/json"})))
	if len(listing.Entries) != 1 || listing.Entries[0].Name != "tool.bin" {
		t.Fatalf("listing through the mirror %+v", listing)
	}
	if desc := getJSON[mirrorAppDTO](b, "/api/apps/mirror/files/mirror"); len(desc.UpstreamChain) != 1 || desc.UpstreamChain[0] != aID || desc.Instructions.En != "Get {{app_url}}/tool.bin" {
		t.Fatalf("B's description must pass on A %+v", desc)
	}
	if state := getJSON[appMirrorDTO](b, "/admin/api/apps/mirror/files/mirror"); !state.Available || state.RemoteKey != "acme/files" || state.Error != nil {
		t.Fatalf("%+v", state)
	}

	// A mirroring B's mirror of itself would be a loop.
	body, _ = a.request("POST", "/admin/api/upstreams", map[string]string{"name": "Internal", "origin": b.http.URL}, 201, nil)
	back := decodeJSONBody[upstreamDTO](t, body)
	if back.InstanceID != bID {
		t.Fatal(back)
	}
	a.expectError("POST", "/admin/api/upstreams/"+back.UID+"/mirrors", map[string]string{"remote_key": "mirror/files", "vendor": "loop", "app": "files"}, 508, codeLoopDetected, nil)

	// Synchronization: a local override stays, the rest follows A.
	b.patchApp("mirror/files", map[string]any{"name.en": "Local name"})
	a.patchApp(files.Key, map[string]any{"name.en": "Renamed", "description.en": "Updated files"})
	b.request("POST", "/admin/api/apps/mirror/files/mirror/sync", nil, 200, nil)
	cfg := b.adminApp("mirror/files")
	if cfg.Name.En != "Local name" || cfg.Description.En != "Updated files" {
		t.Fatalf("after sync %+v", cfg)
	}
	b.expectError("PATCH", "/admin/api/apps/mirror/files/configuration", map[string]any{"set": map[string]any{"base_urls": []string{"https://elsewhere.example"}}}, 400, codeValidationFailed, map[string]string{"If-Match": strconv.Quote(strconv.FormatInt(b.appRevision("mirror/files"), 10))})

	// B's built-in Codex application adopts A's Codex release.
	codex := b.adminApp("openai/codex")
	body, _ = b.request("POST", "/admin/api/upstreams/"+up.UID+"/mirrors", map[string]any{"remote_key": codexKey, "vendor": "openai", "app": "codex", "revision": codex.Revision}, 201, nil)
	if adopted := decodeJSONBody[appDTO](t, body); adopted.Provider != "codex" {
		t.Fatalf("%+v", adopted)
	}
	if data, _ := b.request("GET", "/openai/codex/releases/0.159.2/archive.tgz", nil, 200, nil); !bytes.Equal(data, archive) {
		t.Fatal("Codex artifact through the mirror", string(data))
	}

	// A stops publishing the application: B keeps it, marked unavailable.
	a.setAppEnabled(files.Key, false)
	state := decodeJSONBody[appMirrorDTO](t, mustBody(b.request("POST", "/admin/api/apps/mirror/files/mirror/sync", nil, 200, nil)))
	if state.Available || state.Error == nil || *state.Error != "APPLICATION_NOT_FOUND" {
		t.Fatalf("gone upstream application %+v", state)
	}
	if c := getJSON[configurationDTO](b, "/admin/api/apps/mirror/files/configuration"); !c.TemplateMissing {
		t.Fatal("gone upstream application not reported as a missing template")
	}
	up = getJSON[upstreamDTO](b, "/admin/api/upstreams/"+up.UID)
	b.expectError("DELETE", "/admin/api/upstreams/"+up.UID, nil, 409, codeUpstreamInUse, map[string]string{"If-Match": strconv.Quote(strconv.FormatInt(up.Revision, 10))})

	// Unbinding keeps the values; the upstream can then go.
	revision := b.appRevision("mirror/files")
	b.request("DELETE", "/admin/api/apps/mirror/files/mirror", nil, 200, map[string]string{"If-Match": strconv.Quote(strconv.FormatInt(revision, 10))})
	b.expectError("GET", "/admin/api/apps/mirror/files/mirror", nil, 404, codeMirrorNotFound, nil)
	if cfg = b.adminApp("mirror/files"); cfg.Name.En != "Local name" {
		t.Fatalf("unbound application lost its values %+v", cfg)
	}
}
