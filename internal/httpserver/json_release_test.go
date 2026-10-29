package httpserver

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PMExtra/RedApp/internal/releaseconfig"
	"github.com/PMExtra/RedApp/internal/store"
)

// Real HTTP and source-policy fixture; no official metadata or package downloads.
func TestJSONReleasePreviewDownloadsAndInternalAliases(t *testing.T) {
	var artifactReads atomic.Int32
	var upstreamFailed atomic.Bool
	body := []byte("fixture package")
	var origin string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metadata":
			if upstreamFailed.Load() {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(releaseconfig.Release{Version: "1.2.3", Build: "abc", Artifacts: []releaseconfig.Artifact{{Key: "windows-x64-user", OS: "windows", Arch: "x64", Format: "exe", Scope: "user", URL: origin + "/files/setup.exe", SHA256: fmt.Sprintf("%x", sha256.Sum256(body))}}})
		case "/files/setup.exe":
			artifactReads.Add(1)
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			w.Write(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	origin = upstream.URL
	cfg := releaseconfig.Config{DefaultChannel: "stable", RefreshSeconds: 60, Channels: map[string]releaseconfig.Channel{"stable": {Requests: []releaseconfig.Request{{URL: origin + "/metadata"}}, Query: ".body"}}, Sources: releaseconfig.Sources{Metadata: []releaseconfig.Source{{Origin: origin, Prefix: "/metadata"}}, Artifacts: []releaseconfig.Source{{Origin: origin, Prefix: "/files/"}}}}
	// Source prefixes are directories; metadata is authorized under the origin.
	cfg.Sources.Metadata[0].Prefix = "/"
	h := newHarness(t, func(c *harnessConfig) { c.preserveTemplateDefaults = true })
	h.login("")
	// All response shapes use the same strict App schema, including nested vendors.
	h.request("GET", "/admin/api/vendors?limit=100", nil, 200, nil)
	page := getJSON[pageDTO[appListItemDTO]](h, "/admin/api/apps?limit=100")
	candidates := 0
	for _, item := range page.Items {
		if item.Provider == "json-release" {
			candidates++
			if item.Enabled || item.JSONRelease == nil || len(item.JSONRelease.Prewarm) != 0 {
				t.Fatal("candidate activated", item.Key)
			}
		}
	}
	if candidates != 12 {
		t.Fatal("candidate inventory", candidates)
	}
	h.createVendor("fixture")
	h.request("POST", "/admin/api/apps", map[string]any{"vendor": "fixture", "id": "editor", "provider": "json-release", "name": map[string]string{"en": "Editor", "zh-CN": "编辑器"}, "enabled": true, "json_release": cfg}, 201, nil)
	h.request("GET", "/admin/api/apps/fixture/editor", nil, 200, nil)
	upstreamFailed.Store(true)
	h.expectError("POST", "/admin/api/apps/fixture/editor/release-preview", map[string]any{"config": cfg, "channel": "stable"}, 502, codeUpstreamUnavailable, nil)
	upstreamFailed.Store(false)
	preview, _ := h.request("POST", "/admin/api/apps/fixture/editor/release-preview", map[string]any{"config": cfg, "channel": "stable"}, 200, nil)
	if got := decodeJSONBody[releaseconfig.Release](t, preview); got.Version != "1.2.3" || len(got.Artifacts) != 1 {
		t.Fatal(string(preview))
	}
	if artifactReads.Load() != 0 {
		t.Fatal("preview downloaded package")
	}
	app, err := h.store.Application("fixture/editor")
	if err != nil {
		t.Fatal(err)
	}
	if channel, err := h.store.Channel(app.StorageID(), "stable"); !errors.Is(err, store.ErrNotFound) || channel.Version != "" {
		t.Fatal("preview published channel", channel, err)
	}
	choices := getJSON[releaseDownloadsDTO](h, "/api/apps/fixture/editor/downloads")
	if choices.Channel != "stable" || choices.Version != "1.2.3" || len(choices.Artifacts) != 1 || !strings.Contains(choices.Artifacts[0].URL, "/fixture/editor/releases/1.2.3/windows-x64-user") {
		t.Fatal(choices)
	}
	if strings.HasPrefix(choices.Artifacts[0].URL, origin) || artifactReads.Load() != 0 {
		t.Fatal("public metadata bypassed local download")
	}
	if choices.Artifacts[0].Path != "releases/1.2.3/windows-x64-user" {
		t.Fatal("download choice lacks its application-relative path", choices.Artifacts[0].Path)
	}
	mirror := getJSON[mirrorAppDTO](h, "/api/apps/fixture/editor/mirror")
	if mirror.Provider != "json-release" || strings.Join(mirror.Channels, ",") != "latest,stable" {
		t.Fatal("mirror description of a JSON release", mirror)
	}
	testDownloadChoices(t, h)
	for _, alias := range []string{"latest", "stable"} {
		body, headers := h.request("GET", "/fixture/editor/download/"+alias+"/windows-x64-user", nil, 307, nil)
		if len(body) != 0 || headers.Get("Content-Type") != "" || headers.Get("Location") != "/fixture/editor/releases/1.2.3/windows-x64-user" || headers.Get("Cache-Control") != "no-store" {
			t.Fatal(headers)
		}
	}
	h.expectError("GET", "/fixture/editor/download/stable/linux-arm64", nil, 404, codeFileNotFound, nil)
	h.expectError("GET", "/api/apps/fixture/editor/downloads?channel=nightly", nil, 404, codeFileNotFound, nil)
	data, headers := h.request("GET", "/fixture/editor/releases/1.2.3/windows-x64-user", nil, 200, nil)
	if string(data) != string(body) || artifactReads.Load() != 1 || headers.Get("Content-Disposition") != "attachment; filename=setup.exe" {
		t.Fatal("download", string(data), artifactReads.Load())
	}
}

// Download choices are presentation only: they never start a source epoch,
// hidden packages stay listed and downloadable, and they apply to JSON
// releases only.
func testDownloadChoices(t *testing.T, h *harness) {
	t.Helper()
	if vscode, err := h.store.Application("microsoft/vscode"); err != nil || vscode.DefaultArtifact != "win32-x64-user" || len(vscode.HiddenArtifacts) != 0 {
		t.Fatal("preset download choices", vscode.DefaultArtifact, vscode.HiddenArtifacts, err)
	}
	choices := getJSON[releaseDownloadsDTO](h, "/api/apps/fixture/editor/downloads")
	if choices.DefaultArtifact != nil || choices.Artifacts[0].Hidden {
		t.Fatal("no download choices configured", choices)
	}
	if known := getJSON[releaseArtifactsDTO](h, "/admin/api/apps/fixture/editor/artifacts"); len(known.Items) != 1 || known.Items[0] != (releaseArtifactDTO{Key: "windows-x64-user", OS: "windows", Arch: "x64", Format: "exe", Scope: "user"}) {
		t.Fatal("known artifacts", known)
	}
	before, err := h.store.Application("fixture/editor")
	if err != nil {
		t.Fatal(err)
	}
	h.request("PATCH", "/admin/api/apps/fixture/editor/configuration", map[string]any{"set": map[string]any{"default_artifact": "windows-x64-user", "hidden_artifacts": []string{"windows-x64-user", "gone-key"}}}, 200, ifMatchHeader(before.Revision))
	after, err := h.store.Application("fixture/editor")
	if err != nil || after.SourceEpoch != before.SourceEpoch || after.DefaultArtifact != "windows-x64-user" || strings.Join(after.HiddenArtifacts, ",") != "windows-x64-user,gone-key" {
		t.Fatal("download choices", after, err)
	}
	choices = getJSON[releaseDownloadsDTO](h, "/api/apps/fixture/editor/downloads")
	if choices.Version != "1.2.3" || choices.DefaultArtifact == nil || *choices.DefaultArtifact != "windows-x64-user" || !choices.Artifacts[0].Hidden {
		t.Fatal("configured download choices", choices)
	}
	if mirror := getJSON[mirrorAppDTO](h, "/api/apps/fixture/editor/mirror"); mirror.DefaultArtifact != "windows-x64-user" || strings.Join(mirror.HiddenArtifacts, ",") != "windows-x64-user,gone-key" {
		t.Fatal("mirror description lacks download choices", mirror)
	}
	// A default the current release lacks is not reported.
	h.request("PATCH", "/admin/api/apps/fixture/editor/configuration", map[string]any{"set": map[string]any{"default_artifact": "gone-key"}}, 200, ifMatchHeader(after.Revision))
	if choices = getJSON[releaseDownloadsDTO](h, "/api/apps/fixture/editor/downloads"); choices.DefaultArtifact != nil {
		t.Fatal("default artifact missing from the release", *choices.DefaultArtifact)
	}
	revision := h.appRevision("fixture/editor")
	for _, set := range []map[string]any{{"default_artifact": "Bad Key"}, {"hidden_artifacts": []string{"a", "a"}}} {
		h.expectError("PATCH", "/admin/api/apps/fixture/editor/configuration", map[string]any{"set": set}, 400, codeValidationFailed, ifMatchHeader(revision))
	}
	h.expectError("PATCH", "/admin/api/apps/fixture/editor/configuration", map[string]any{"set": map[string]any{"hidden_artifacts": nil}}, 400, codeInvalidRequest, ifMatchHeader(revision))
	h.request("POST", "/admin/api/apps", map[string]any{"vendor": "fixture", "id": "plain", "provider": "http-cache", "name": map[string]string{"en": "Plain", "zh-CN": "普通"}, "base_urls": []string{"https://plain.example"}, "enabled": true}, 201, nil)
	h.expectError("PATCH", "/admin/api/apps/fixture/plain/configuration", map[string]any{"set": map[string]any{"default_artifact": "x"}}, 400, codeValidationFailed, ifMatchHeader(h.appRevision("fixture/plain")))
	h.expectError("GET", "/admin/api/apps/fixture/plain/artifacts", nil, 404, codeCapabilityUnsupported, nil)
}
