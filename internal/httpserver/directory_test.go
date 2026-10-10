package httpserver

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/presets"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/media"
	"github.com/PMExtra/RedApp/internal/store"
)

func TestDirectoryHTTPProviderCreationCASAndSettings(t *testing.T) {
	h := newHarness(t)
	h.request("GET", "/admin/api/providers", nil, 401, nil)
	h.login(h.password)
	data, _ := h.request("GET", "/admin/api/providers", nil, 200, nil)
	definitions := directoryDecode[[]application.Definition](t, data, "providers")
	providers := map[string]application.Definition{}
	for _, definition := range definitions {
		providers[definition.Key] = definition
	}
	if len(providers) != 5 || providers[application.HttpCache].Capabilities.Versions || !providers[application.HttpCache].Capabilities.TimeCleanup || providers[application.Codex].DefaultBaseURL == "" || providers[application.ClaudeCode].DefaultBaseURL == "" {
		t.Fatal("provider API lost its peer capability/default contract", providers)
	}
	input := map[string]any{"id": "blocked", "name": store.LocalizedText{En: "Blocked", ZhCN: "阻止"}}
	h.request("POST", "/admin/api/vendors", input, 403, map[string]string{"X-CSRF-Token": ""})
	h.request("POST", "/admin/api/vendors", input, 403, map[string]string{"Origin": "https://foreign.example"})
	initialRevision, _ := h.publicCatalog()
	vendor := h.createVendor("enterprise")
	h.request("POST", "/admin/api/vendors", input, 201, nil)
	h.request("POST", "/admin/api/vendors", input, 409, nil)
	for _, provider := range []string{application.Codex, application.ClaudeCode} {
		app := h.createApp(vendor.ID, provider, provider, nil)
		if app.BaseURL != providers[provider].DefaultBaseURL || app.CacheTTLSeconds != 60 || !app.Enabled || app.SourceEpoch != 1 || app.UID == "" {
			t.Fatal("release provider defaults were not persisted", app)
		}
	}
	missingBase := map[string]any{"id": "missing-base", "provider": application.HttpCache, "name": store.LocalizedText{En: "Files", ZhCN: "文件"}}
	h.request("POST", "/admin/api/vendors/enterprise/apps", missingBase, 400, nil)
	general := h.createApp(vendor.ID, "files", application.HttpCache, map[string]any{"base_url": "http://intranet.example:8081/packages/", "cache_ttl_seconds": 0})
	if general.BaseURL != "http://intranet.example:8081/packages" || general.CacheTTLSeconds != 0 {
		t.Fatal("explicit zero TTL or enterprise BaseURL was lost", general)
	}
	defaultTTL := h.createApp(vendor.ID, "files-default", application.HttpCache, map[string]any{"base_url": "http://intranet.example/packages"})
	if defaultTTL.CacheTTLSeconds != 300 {
		t.Fatal("omitted GeneralHttp TTL did not use the provider default")
	}
	api := "/admin/api/apps/" + general.Key
	for _, immutable := range []map[string]any{{"id": "renamed"}, {"provider": application.Codex}, {"vendor_id": "openai"}} {
		immutable["revision"] = general.Revision
		h.request("PATCH", api, immutable, 400, nil)
	}
	// Two localized strings within the documented per-field limit can exceed
	// the old generic 8 KiB JSON body cap even at 2,000 characters each.
	description := store.LocalizedText{En: strings.Repeat("介紹", 1000), ZhCN: strings.Repeat("介绍", 1000)}
	data, _ = h.request("PATCH", api, map[string]any{"description": description}, 200, map[string]string{"If-Match": fmt.Sprint(general.Revision)})
	edited := directoryDecode[store.Application](t, data, "app")
	if edited.Revision != general.Revision+1 || edited.SourceEpoch != general.SourceEpoch || edited.UID != general.UID || edited.CacheTTLSeconds != 0 || edited.Description != description {
		t.Fatal("partial edit lost identity/source/TTL", edited)
	}
	h.request("PATCH", api, map[string]any{"revision": general.Revision, "enabled": false}, 409, nil)
	h.request("PATCH", api, map[string]any{"revision": edited.Revision}, 400, map[string]string{"If-Match": "1"})
	data, _ = h.request("PATCH", api, map[string]any{"revision": edited.Revision, "base_url": "https://new-source.example/files"}, 200, nil)
	rebound := directoryDecode[store.Application](t, data, "app")
	if rebound.SourceEpoch != 2 || rebound.MetricsID() != general.MetricsID() || rebound.StorageID() == general.StorageID() {
		t.Fatal("API source edit did not isolate the cache namespace", rebound)
	}
	currentRevision, public := h.publicCatalog()
	if initialRevision == currentRevision || public[general.Key].Provider != application.HttpCache || public[general.Key].Capabilities.Versions || public[general.Key].Vendor.Name.ZhCN != "企业" {
		t.Fatal("dynamic public bootstrap did not reflect directory/provider metadata", public)
	}

	// Existing release settings now edit the same directory record seen by the
	// directory form; they must not leave a second independent TTL setting behind.
	codexAPI := "/admin/api/apps/enterprise/codex"
	data, _ = h.request("GET", codexAPI, nil, 200, nil)
	codex := directoryDecode[store.Application](t, data, "app")
	data, headers := h.request("GET", codexAPI+"/settings", nil, 200, nil)
	if directoryDecode[int](t, data, "channel_ttl_seconds") != codex.CacheTTLSeconds || directoryDecode[int64](t, data, "revision") != codex.Revision || headers.Get("ETag") == "" {
		t.Fatal("settings view does not use directory TTL/revision", string(data))
	}
	h.request("PUT", codexAPI+"/settings", map[string]int{"channel_ttl_seconds": 120}, 200, map[string]string{"If-Match": fmt.Sprint(codex.Revision)})
	data, _ = h.request("GET", codexAPI, nil, 200, nil)
	updated := directoryDecode[store.Application](t, data, "app")
	if updated.CacheTTLSeconds != 120 || updated.Revision != codex.Revision+1 || updated.SourceEpoch != codex.SourceEpoch {
		t.Fatal("settings update diverged from application record", updated)
	}
	h.request("PUT", codexAPI+"/settings", map[string]int{"channel_ttl_seconds": 30}, 409, map[string]string{"If-Match": fmt.Sprint(codex.Revision)})
	entry, ok := h.server.registry.Lookup(updated.Key)
	if !ok || entry.Descriptor.DefaultChannelTTLSeconds != 120 || entry.Revision != updated.Revision {
		t.Fatal("settings write did not publish the new runtime snapshot")
	}
	installer, _ := h.request("GET", "/enterprise/codex/install.sh", nil, 200, nil)
	if !bytes.Contains(installer, []byte(h.http.URL+"/enterprise/codex")) || bytes.Contains(installer, []byte("@REDAPP_BASE_URL@")) {
		t.Fatal("custom instance installer was not bound to its public root")
	}
	h.request("GET", "/enterprise/codex/licenses/LICENSE", nil, 200, nil)
}

func TestDirectoryHTTPDisableDeleteAndEmptyRestart(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, withDir(dir))
	password := h.password
	h.login(password)
	vendor := h.createVendor("enterprise")
	app := h.createApp(vendor.ID, "codex", application.Codex, nil)
	if err := h.server.store.AddFor(app.MetricsID(), "artifact_requests", 7); err != nil {
		t.Fatal(err)
	}
	before, _ := h.publicCatalog()
	data, _ := h.request("PATCH", "/admin/api/vendors/enterprise", map[string]any{"revision": vendor.Revision, "enabled": false}, 200, nil)
	disabledVendor := directoryDecode[store.Vendor](t, data, "vendor")
	after, public := h.publicCatalog()
	if before == after {
		t.Fatal("vendor visibility change did not update bootstrap revision")
	}
	if _, present := public[app.Key]; present {
		t.Fatal("disabled vendor's application remained public")
	}
	for _, path := range []string{"/enterprise/codex", "/enterprise/codex/install.sh", "/api/apps/enterprise/codex"} {
		h.request("GET", path, nil, 404, nil)
	}
	data, _ = h.request("GET", "/admin/api/apps/enterprise/codex", nil, 200, nil)
	retained := directoryDecode[store.Application](t, data, "app")
	if !retained.Enabled || retained.Revision != app.Revision {
		t.Fatal("vendor disable rewrote the app enable flag or revision")
	}
	h.request("GET", "/admin/vendors/enterprise/apps/codex/settings", nil, 200, nil)
	h.request("PATCH", "/admin/api/vendors/enterprise", map[string]any{"revision": disabledVendor.Revision, "enabled": true}, 200, nil)
	h.request("GET", "/enterprise/codex", nil, 200, nil)
	h.request("PATCH", "/admin/api/apps/enterprise/codex", map[string]any{"revision": app.Revision, "enabled": false}, 200, nil)
	h.request("GET", "/enterprise/codex/install.sh", nil, 404, nil)
	data, _ = h.request("GET", "/admin/api/vendors/enterprise", nil, 200, nil)
	currentVendor := directoryDecode[store.Vendor](t, data, "vendor")
	h.request("DELETE", "/admin/api/vendors/enterprise", map[string]any{"revision": currentVendor.Revision, "confirm_key": "enterprise"}, 409, nil)

	// Built-in keys are protected even when disabled; custom deletion is permanent.
	data, _ = h.request("GET", "/admin/api/apps", nil, 200, nil)
	apps := directoryDecode[[]store.Application](t, data, "items")
	for _, row := range apps {
		if row.BuiltinTemplate {
			h.request("DELETE", "/admin/api/apps/"+row.Key, map[string]any{"revision": row.Revision, "confirm_key": row.Key, "confirm_uid": row.UID}, 409, nil)
			h.request("PATCH", "/admin/api/apps/"+row.Key, map[string]any{"revision": row.Revision, "enabled": false}, 200, nil)
		} else {
			h.request("DELETE", "/admin/api/apps/"+row.Key, map[string]any{"revision": row.Revision, "confirm_key": row.Key, "confirm_uid": row.UID}, 200, nil)
		}
	}
	h.request("DELETE", "/admin/api/vendors/enterprise", map[string]any{"revision": currentVendor.Revision, "confirm_key": "enterprise"}, 200, nil)
	h.request("DELETE", "/admin/api/vendors/openai", map[string]any{"revision": 1, "confirm_key": "openai"}, 409, nil)
	_, public = h.publicCatalog()
	if len(public) != 0 {
		t.Fatal("disabled or deleted application remained public")
	}
	h.close()
	h = newHarness(t, withDir(dir))
	if h.password != "" {
		t.Fatal("admin credentials reset")
	}
	h.login(password)
	_, public = h.publicCatalog()
	if len(public) != 0 {
		t.Fatal("restart enabled a protected template")
	}
	h.request("GET", "/admin/api/apps/enterprise/codex", nil, 404, nil)
	data, _ = h.request("GET", "/admin/api/apps?state=deleted", nil, 200, nil)
	if rows := directoryDecode[[]store.Application](t, data, "items"); len(rows) != 0 {
		t.Fatal("permanent removal left tombstones")
	}
	counters, err := h.server.store.CountersFor(app.MetricsID())
	if err != nil || len(counters) != 0 {
		t.Fatal("deleted history survived", counters, err)
	}
	h.createVendor("enterprise")
	h.createVendor("after-restart")
	h.createApp("after-restart", "files", application.HttpCache, map[string]any{"base_url": "http://intranet.example/files"})
	_, public = h.publicCatalog()
	if len(public) != 1 || public["after-restart/files"].ID == "" {
		t.Fatal("empty runtime could not admit a new application")
	}

}

func TestDirectoryHTTPIconUploadAndPublicBoundary(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	vendor := h.createVendor("enterprise")
	upload := func(svg, csrf string, want int) string {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		file, err := form.CreateFormFile("file", "untrusted-name.svg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(file, svg); err != nil {
			t.Fatal(err)
		}
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		code, data, _ := h.raw("POST", "/admin/api/assets/icons", &body, form.FormDataContentType(), map[string]string{"X-CSRF-Token": csrf})
		if code != want {
			t.Fatalf("icon upload got %d, want %d: %s", code, want, data)
		}
		if code == 201 {
			return directoryDecode[string](t, data, "icon")
		}
		return ""
	}
	staticSVG := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#123456" d="M0 0 L24 0 L12 24 Z"/></svg>`
	upload(staticSVG, "", 403)
	upload(`<svg onload="alert(1)"><script>alert(1)</script></svg>`, h.csrf, 400)
	path := upload(staticSVG, h.csrf, 201)
	if !strings.HasPrefix(path, media.PublicPrefix) || strings.Contains(path, "untrusted-name") {
		t.Fatal("upload filename became a public/storage path", path)
	}
	body, headers := h.request("GET", path, nil, 200, nil)
	if !bytes.Contains(body, []byte("#123456")) || headers.Get("Content-Type") != "image/svg+xml" || headers.Get("X-Content-Type-Options") != "nosniff" || headers.Get("Content-Security-Policy") != "sandbox; default-src 'none'" || !strings.Contains(headers.Get("Cache-Control"), "immutable") {
		t.Fatal("uploaded SVG lost the inert public response contract", headers)
	}
	head, _ := h.request("HEAD", path, nil, 200, nil)
	if len(head) != 0 {
		t.Fatal("icon HEAD returned a response body")
	}
	h.request("PATCH", "/admin/api/vendors/enterprise", map[string]any{"revision": vendor.Revision, "icon": media.PublicPrefix + strings.Repeat("f", 64) + ".png"}, 400, nil)
	h.request("PATCH", "/admin/api/vendors/enterprise", map[string]any{"revision": vendor.Revision, "icon": path}, 200, nil)
	app := h.createApp(vendor.ID, "files", application.HttpCache, map[string]any{"base_url": "http://intranet.example/files"})
	_, public := h.publicCatalog()
	if public[app.Key].Icon != path {
		t.Fatal("application without its own icon did not inherit vendor icon")
	}
	// The public URL serves only normalized bytes even after the upload session
	// has gone away; it cannot expose the raw upload name or arbitrary files.
	h.request("GET", media.PublicPrefix+"untrusted-name.svg", nil, 404, nil)
	h.request("GET", path+"/extra", nil, 404, nil)
}

func TestDirectoryOpenAISharedIconAndEnabledOnlyPATCH(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	icon, _ := h.request("GET", presets.ImagePrefix+"builtin/openai.svg", nil, 200, nil)
	codex, _ := h.request("GET", presets.ImagePrefix+"openai/codex/icon.svg", nil, 200, nil)
	if !bytes.Equal(icon, codex) {
		t.Fatal("vendor and application icon bytes differ")
	}
	v, _ := h.server.store.Vendor("openai")
	a, _ := h.server.store.Application("openai/codex")
	data, _ := h.request("PATCH", "/admin/api/vendors/openai", map[string]any{"revision": v.Revision, "enabled": false}, 200, nil)
	changedVendor := directoryDecode[store.Vendor](t, data, "vendor")
	if changedVendor.Enabled || changedVendor.Revision != v.Revision+1 || changedVendor.Name != v.Name || changedVendor.Description != v.Description || changedVendor.Icon != v.Icon {
		t.Fatal("enabled patch changed vendor fields", changedVendor)
	}
	h.request("PATCH", "/admin/api/vendors/openai", map[string]any{"revision": v.Revision, "enabled": true}, 409, nil)
	data, _ = h.request("PATCH", "/admin/api/apps/openai/codex", map[string]any{"revision": a.Revision, "enabled": false}, 200, nil)
	changedApp := directoryDecode[store.Application](t, data, "app")
	if changedApp.Enabled || changedApp.Revision != a.Revision+1 || changedApp.Name != a.Name || changedApp.Description != a.Description || changedApp.Icon != a.Icon || changedApp.BaseURL != a.BaseURL || changedApp.SourceEpoch != a.SourceEpoch {
		t.Fatal("enabled patch changed app fields", changedApp)
	}
	h.request("PATCH", "/admin/api/apps/openai/codex", map[string]any{"revision": a.Revision, "enabled": true}, 409, nil)
	independent, _ := h.request("GET", presets.ImagePrefix+"builtin/openai.svg", nil, 200, nil)
	if !bytes.Equal(icon, independent) {
		t.Fatal("vendor icon depends on application state")
	}
}
