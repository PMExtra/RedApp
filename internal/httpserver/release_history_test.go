package httpserver

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/releaseconfig"
)

// A JSON release lists every release it discovered, serves each by version,
// and discovers older versions on request.
func TestReleaseHistoryAndLookup(t *testing.T) {
	packages := map[string][]byte{"1.0.0": []byte("old package"), "1.10.0": []byte("new package")}
	var origin string
	release := func(version string) releaseconfig.Release {
		return releaseconfig.Release{Version: version, Artifacts: []releaseconfig.Artifact{{Key: "win-x64", OS: "windows", Arch: "x64", Format: "exe", URL: origin + "/files/" + version + "/setup.exe", SHA256: fmt.Sprintf("%x", sha256.Sum256(packages[version]))}}}
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/latest/win":
			json.NewEncoder(w).Encode(release("1.10.0"))
		case r.URL.Path == "/versions/1.0.0/win":
			json.NewEncoder(w).Encode(release("1.0.0"))
		case r.URL.Path == "/versions/2.0.0/win":
			// An upstream answering with another version is not trusted.
			json.NewEncoder(w).Encode(release("1.0.0"))
		case strings.HasPrefix(r.URL.Path, "/files/"):
			version := strings.Split(r.URL.Path, "/")[2]
			w.Write(packages[version])
		default:
			// No version has a Linux package; 9.9.9 does not exist at all.
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	origin = upstream.URL
	cfg := releaseconfig.Config{
		DefaultChannel: "stable", RefreshSeconds: 60,
		Channels: map[string]releaseconfig.Channel{"stable": {Requests: []releaseconfig.Request{{URL: origin + "/latest/win"}}, Query: ".body"}},
		Lookup:   &releaseconfig.Channel{Requests: []releaseconfig.Request{{URL: origin + "/versions/{version}/win"}, {URL: origin + "/versions/{version}/linux"}}, Query: ".body"},
		Sources:  releaseconfig.Sources{Metadata: []releaseconfig.Source{{Origin: origin, Prefix: "/"}}, Artifacts: []releaseconfig.Source{{Origin: origin, Prefix: "/files/"}}},
	}
	h := newHarness(t)
	h.login("")
	h.createVendor("fixture")
	h.request("POST", "/admin/api/apps", map[string]any{"vendor": "fixture", "id": "editor", "provider": "json-release", "name": map[string]string{"en": "Editor", "zh-CN": "编辑器"}, "enabled": true, "json_release": cfg}, 201, nil)
	current := getJSON[releaseDownloadsDTO](h, "/api/apps/fixture/editor/downloads")
	history := getJSON[releaseHistoryDTO](h, "/api/apps/fixture/editor/releases")
	if len(history.Items) != 1 || history.Truncated || history.Items[0].Version != current.Version || strings.Join(history.Items[0].Channels, ",") != "stable" {
		t.Fatalf("history after the first refresh %+v", history)
	}

	before := h.appRevision("fixture/editor")
	looked := decodeJSONBody[releaseSummaryDTO](t, mustBody(h.request("POST", "/admin/api/apps/fixture/editor/releases/lookup", map[string]string{"version": " 1.0.0 "}, 200, nil)))
	if looked.Version != "1.0.0" || len(looked.Channels) != 0 {
		t.Fatalf("looked-up release %+v", looked)
	}
	if h.appRevision("fixture/editor") != before {
		t.Fatal("a lookup changed the configuration")
	}
	// Looking it up again finds the identical release.
	h.request("POST", "/admin/api/apps/fixture/editor/releases/lookup", map[string]string{"version": "1.0.0"}, 200, nil)
	history = getJSON[releaseHistoryDTO](h, "/api/apps/fixture/editor/releases")
	if len(history.Items) != 2 || history.Items[0].Version != "1.10.0" || history.Items[1].Version != "1.0.0" {
		t.Fatalf("history is ordered by version %+v", history)
	}
	if stable := getJSON[releaseDownloadsDTO](h, "/api/apps/fixture/editor/downloads"); stable.Version != current.Version {
		t.Fatal("a lookup moved the channel", stable.Version)
	}

	for _, query := range []string{"version=1.0.0"} {
		old := getJSON[releaseDownloadsDTO](h, "/api/apps/fixture/editor/downloads?"+query)
		if old.Version != "1.0.0" || old.Artifacts[0].Path != "releases/1.0.0/win-x64" || old.Channel != "" || old.Stale || len(old.Artifacts) != 1 {
			t.Fatalf("%s: %+v", query, old)
		}
		if data, _ := h.request("GET", "/fixture/editor/"+old.Artifacts[0].Path, nil, 200, nil); string(data) != "old package" {
			t.Fatal("old package", string(data))
		}
	}
	// The versions tab names releases by version, newest first, and the
	// metrics tab is available.
	versions := getJSON[cursorPage[versionDTO]](h, "/admin/api/apps/fixture/editor/versions")
	if len(versions.Items) != 2 || versions.Items[0].Version != "1.10.0" || versions.Items[1].Version != "1.0.0" {
		t.Fatalf("versions %+v", versions.Items)
	}
	h.request("GET", "/admin/api/apps/fixture/editor/status", nil, 200, nil)
	// Versions order naturally, so cleanup below 1.10.0 selects 1.0.0's file.
	cleanup := decodeJSONBody[versionCleanupDTO](t, mustBody(h.request("POST", "/admin/api/apps/fixture/editor/version-cleanup/preview", map[string]string{"minimum_version": "1.10.0"}, 201, nil)))
	if len(cleanup.UnknownVersions) != 0 || cleanup.LogicalBytes != int64(len(packages["1.0.0"])) {
		t.Fatalf("version cleanup %+v", cleanup)
	}
	h.expectError("GET", "/api/apps/fixture/editor/downloads?version=3.0.0", nil, 404, codeFileNotFound, nil)
	h.expectError("GET", "/api/apps/fixture/editor/downloads?version=a%20b", nil, 404, codeFileNotFound, nil)
	h.expectError("GET", "/api/apps/fixture/editor/downloads?channel=stable&version=1.0.0", nil, 400, codeInvalidQuery, nil)

	h.expectError("POST", "/admin/api/apps/fixture/editor/releases/lookup", map[string]string{"version": "9.9.9"}, 404, codeVersionNotFound, nil)
	h.expectError("POST", "/admin/api/apps/fixture/editor/releases/lookup", map[string]string{"version": "2.0.0"}, 502, codeMetadataUntrusted, nil)
	h.expectError("POST", "/admin/api/apps/fixture/editor/releases/lookup", map[string]string{"version": "../x"}, 400, codeValidationFailed, nil)

	cfg.Lookup = nil
	h.request("POST", "/admin/api/apps", map[string]any{"vendor": "fixture", "id": "plain", "provider": "json-release", "name": map[string]string{"en": "Plain", "zh-CN": "普通"}, "enabled": true, "json_release": cfg}, 201, nil)
	h.expectError("POST", "/admin/api/apps/fixture/plain/releases/lookup", map[string]string{"version": "1.0.0"}, 400, codeValidationFailed, nil)
	h.setAppEnabled("fixture/editor", false)
	h.expectError("POST", "/admin/api/apps/fixture/editor/releases/lookup", map[string]string{"version": "1.0.0"}, 409, codeApplicationDisabled, nil)
	h.request("POST", "/admin/api/apps", map[string]any{"vendor": "fixture", "id": "files", "provider": "hosted", "name": map[string]string{"en": "Files", "zh-CN": "文件"}, "enabled": true}, 201, nil)
	h.expectError("GET", "/api/apps/fixture/files/releases", nil, 404, codeCapabilityUnsupported, nil)
}
