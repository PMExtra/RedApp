package httpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/releaseconfig"
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
	// A JSON release application whose feed is a local server.
	setup := []byte("setup bytes")
	var feedURL string
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/meta":
			json.NewEncoder(w).Encode(releaseconfig.Release{Version: "2.0.0", Artifacts: []releaseconfig.Artifact{{Key: "win-x64", OS: "windows", Arch: "x64", Format: "exe", URL: feedURL + "/files/setup.exe", SHA256: fmt.Sprintf("%x", sha256.Sum256(setup))}}})
		case "/files/setup.exe":
			w.Write(setup)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(feed.Close)
	feedURL = feed.URL
	feedConfig := func(query string) map[string]any {
		return map[string]any{
			"default_channel": "beta", "refresh_seconds": 60,
			"channels": map[string]any{
				"stable": map[string]any{"requests": []any{map[string]any{"url": feed.URL + "/meta"}}, "query": query},
				"beta":   map[string]any{"requests": []any{map[string]any{"url": feed.URL + "/meta"}}, "query": query},
			},
			"sources": map[string]any{"metadata": []any{map[string]any{"origin": feed.URL, "prefix": "/"}}, "artifacts": []any{map[string]any{"origin": feed.URL, "prefix": "/files/"}}},
		}
	}
	a.createApp("acme", "feed", application.JSONRelease, map[string]any{"json_release": feedConfig(".body")})

	b.login("")
	b.expectError("POST", "/admin/api/upstreams", map[string]string{"name": "Self", "origin": b.http.URL}, 508, codeLoopDetected, nil)
	b.expectError("POST", "/admin/api/upstreams", map[string]string{"name": "Bad", "origin": b.http.URL + "/path"}, 400, codeValidationFailed, nil)
	body, headers := b.request("POST", "/admin/api/upstreams", map[string]string{"name": "DMZ", "origin": a.http.URL + "/"}, 201, nil)
	up := decodeJSONBody[upstreamDTO](t, body)
	if up.InstanceID != aID || up.Origin != a.http.URL || headers.Get("Location") != "/admin/api/upstreams/"+up.UID {
		t.Fatalf("%+v %v", up, headers)
	}
	b.expectError("POST", "/admin/api/upstreams", map[string]string{"name": "Again", "origin": a.http.URL}, 409, codeAlreadyExists, nil)
	if code, page, _ := b.raw("GET", "/admin/upstreams/"+up.UID, nil, "", nil); code != 200 || entryDocument(page) != "admin" {
		t.Fatal("upstream page", code)
	}

	catalog := getJSON[pageDTO[upstreamCatalogItemDTO]](b, "/admin/api/upstreams/"+up.UID+"/catalog?limit=100")
	supported := map[string]bool{}
	for _, item := range catalog.Items {
		supported[item.Key] = item.Supported
	}
	if !supported["acme/files"] || !supported[codexKey] || !supported["acme/feed"] {
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

	// A JSON release is mirrored without a query to write: B reads A's
	// download choices and downloads through A.
	b.request("POST", "/admin/api/upstreams/"+up.UID+"/mirrors", map[string]string{"remote_key": "acme/feed", "vendor": "mirror", "app": "feed"}, 201, nil)
	if desc := getJSON[mirrorAppDTO](a, "/api/apps/acme/feed/mirror"); desc.DefaultChannel == nil || *desc.DefaultChannel != "beta" {
		t.Fatalf("A's default channel %+v", desc)
	}
	choices := getJSON[releaseDownloadsDTO](b, "/api/apps/mirror/feed/downloads")
	if choices.Channel != "beta" || choices.Version != "2.0.0" || len(choices.Artifacts) != 1 || !strings.HasPrefix(choices.Artifacts[0].URL, b.http.URL+"/mirror/feed/releases/") {
		t.Fatalf("B's download choices %+v", choices)
	}
	if data, _ := b.request("GET", "/mirror/feed/releases/"+choices.ReleaseID+"/win-x64", nil, 200, nil); !bytes.Equal(data, setup) {
		t.Fatal("JSON release artifact through the mirror", string(data))
	}
	// A new source upstream gives the mirror a new epoch too, so the same
	// release discovered again under the upstream's new source is not taken
	// for a changed immutable release.
	epoch := b.adminApp("mirror/feed").SourceEpoch
	a.patchApp("acme/feed", map[string]any{"json_release": feedConfig(". | .body")})
	b.request("POST", "/admin/api/apps/mirror/feed/mirror/sync", nil, 200, nil)
	if next := b.adminApp("mirror/feed").SourceEpoch; next != epoch+1 {
		t.Fatal("the mirror kept its epoch after the upstream source changed", epoch, next)
	}
	if again := getJSON[releaseDownloadsDTO](b, "/api/apps/mirror/feed/downloads"); again.ReleaseID != choices.ReleaseID {
		t.Fatalf("release after the upstream epoch change %+v", again)
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
