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
	body := []byte("fixture package")
	var origin string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metadata":
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
	h := newHarness(t)
	h.login("")
	h.createVendor("fixture")
	h.request("POST", "/admin/api/apps", map[string]any{"vendor": "fixture", "id": "editor", "provider": "json-release", "name": map[string]string{"en": "Editor", "zh-CN": "编辑器"}, "enabled": true, "json_release": cfg}, 201, nil)
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
	if choices.Channel != "stable" || choices.Version != "1.2.3" || len(choices.Artifacts) != 1 || !strings.Contains(choices.Artifacts[0].URL, "/fixture/editor/releases/"+choices.ReleaseID+"/windows-x64-user") {
		t.Fatal(choices)
	}
	if strings.HasPrefix(choices.Artifacts[0].URL, origin) || artifactReads.Load() != 0 {
		t.Fatal("public metadata bypassed local download")
	}
	for _, alias := range []string{"latest", "stable"} {
		_, headers := h.request("GET", "/fixture/editor/download/"+alias+"/windows-x64-user", nil, 307, nil)
		if headers.Get("Location") != "/fixture/editor/releases/"+choices.ReleaseID+"/windows-x64-user" || headers.Get("Cache-Control") != "no-store" {
			t.Fatal(headers)
		}
	}
	h.expectError("GET", "/fixture/editor/download/stable/linux-arm64", nil, 404, codeFileNotFound, nil)
	h.expectError("GET", "/api/apps/fixture/editor/downloads?channel=nightly", nil, 404, codeFileNotFound, nil)
	data, headers := h.request("GET", "/fixture/editor/releases/"+choices.ReleaseID+"/windows-x64-user", nil, 200, nil)
	if string(data) != string(body) || artifactReads.Load() != 1 || headers.Get("Content-Disposition") != "attachment; filename=setup.exe" {
		t.Fatal("download", string(data), artifactReads.Load())
	}
}
