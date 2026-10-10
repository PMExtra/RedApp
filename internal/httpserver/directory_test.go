package httpserver

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/media"
)

func TestProvidersListCapabilitiesAndNullDefaults(t *testing.T) {
	h := newHarness(t)
	h.expectError("GET", "/admin/api/providers", nil, 401, codeAuthRequired, nil)
	h.login("")
	list := getJSON[providerListDTO](h, "/admin/api/providers")
	providers := map[string]providerDTO{}
	for _, p := range list.Items {
		providers[p.Key] = p
	}
	if len(providers) != 5 || providers[application.HttpCache].Capabilities.Versions || !providers[application.HttpCache].Capabilities.TimeCleanup {
		t.Fatal("provider capabilities", list)
	}
	for _, key := range []string{application.Codex, application.ClaudeCode} {
		if p := providers[key]; p.DefaultBaseURL == nil || *p.DefaultBaseURL == "" || p.DefaultCacheTTLSeconds == nil || *p.DefaultCacheTTLSeconds != 60 {
			t.Fatal("release provider defaults", key, p)
		}
	}
	for _, key := range []string{application.Info, application.Hosted} {
		if p := providers[key]; p.DefaultBaseURL != nil || p.DefaultCacheTTLSeconds != nil {
			t.Fatal("content providers have no upstream defaults", key, p)
		}
	}
	if p := providers[application.HttpCache]; p.DefaultBaseURL != nil || p.DefaultCacheTTLSeconds == nil || *p.DefaultCacheTTLSeconds != 300 {
		t.Fatal("http-cache defaults", p)
	}
	h.expectError("GET", "/admin/api/providers?view=all", nil, 400, codeInvalidQuery, nil)
}

func TestVendorCreateReadToggleAndPermanentDelete(t *testing.T) {
	h := newHarness(t)
	h.login("")
	input := map[string]any{"id": "enterprise", "name": map[string]string{"en": "Enterprise", "zh-CN": "企业"}, "enabled": true}
	h.expectError("POST", "/admin/api/vendors", input, 403, codeCSRFRejected, map[string]string{"X-CSRF-Token": ""})
	h.expectError("POST", "/admin/api/vendors", input, 403, codeOriginRejected, map[string]string{"Origin": "https://foreign.example"})
	h.expectError("POST", "/admin/api/vendors", map[string]any{"id": "enterprise", "name": input["name"]}, 400, codeInvalidRequest, nil)
	h.expectError("POST", "/admin/api/vendors", map[string]any{"id": "all", "name": input["name"], "enabled": true}, 400, codeValidationFailed, nil)
	h.expectError("POST", "/admin/api/vendors", map[string]any{"id": "blank", "name": map[string]string{"en": " ", "zh-CN": "x"}, "enabled": true}, 400, codeValidationFailed, nil)
	h.expectError("POST", "/admin/api/vendors", map[string]any{"id": "icon", "name": input["name"], "enabled": true, "icon": media.PublicPrefix + strings.Repeat("f", 64) + ".png"}, 400, codeValidationFailed, nil)
	data, headers := h.request("POST", "/admin/api/vendors", input, 201, nil)
	created := decodeJSONBody[vendorDTO](t, data)
	if headers.Get("Location") != "/admin/api/vendors/enterprise" || headers.Get("ETag") != etag(created.Revision) || created.HasTemplate || !created.Enabled || created.DeletedAt != nil {
		t.Fatal("created vendor", headers, string(data))
	}
	h.expectError("POST", "/admin/api/vendors", input, 409, codeAlreadyExists, nil)
	_, headers = h.request("GET", "/admin/api/vendors/enterprise", nil, 200, nil)
	if headers.Get("ETag") != etag(created.Revision) {
		t.Fatal("GET ETag", headers)
	}
	h.expectError("GET", "/admin/api/vendors/missing", nil, 404, codeVendorNotFound, nil)
	h.expectError("GET", "/admin/api/vendors/Bad_ID", nil, 400, codeInvalidPath, nil)

	app := h.createApp("enterprise", "tool", application.Info, nil)
	path := "/admin/api/vendors/enterprise"
	h.expectError("PATCH", path, map[string]any{"enabled": false}, 400, codeIfMatchRequired, nil)
	h.expectError("PATCH", path, map[string]any{"enabled": false}, 400, codeIfMatchRequired, map[string]string{"If-Match": "W/\"1\""})
	h.expectError("PATCH", path, map[string]any{"enabled": false, "name": input["name"]}, 400, codeInvalidRequest, ifMatchHeader(created.Revision))
	h.expectError("PATCH", path, map[string]any{"enabled": false}, 409, codeRevisionConflict, ifMatchHeader(created.Revision+1))
	h.expectError("PATCH", "/admin/api/vendors/missing", map[string]any{"enabled": false}, 404, codeVendorNotFound, ifMatchHeader(1))
	data, headers = h.request("PATCH", path, map[string]any{"enabled": false}, 200, ifMatchHeader(created.Revision))
	disabled := decodeJSONBody[vendorDTO](t, data)
	if disabled.Enabled || disabled.Revision != created.Revision+1 || disabled.Name != created.Name || headers.Get("ETag") != etag(disabled.Revision) {
		t.Fatal("enabled toggle changed other fields", string(data))
	}
	// Disabling the vendor unpublishes its applications without touching them.
	h.expectError("GET", "/api/apps/"+app.Key, nil, 404, codeApplicationNotFound, nil)
	if retained := h.adminApp(app.Key); !retained.Enabled || retained.Revision != app.Revision {
		t.Fatal("vendor toggle rewrote the application", retained)
	}

	h.expectError("DELETE", path, nil, 400, codeIfMatchRequired, nil)
	h.expectError("DELETE", path, nil, 409, codeVendorNotEmpty, ifMatchHeader(disabled.Revision))
	openai := h.adminVendor("openai")
	h.expectError("DELETE", "/admin/api/vendors/openai", nil, 409, codeBuiltinProtected, ifMatchHeader(openai.Revision))
	h.deleteApp(app.Key, app.UID, app.Revision, 200)
	h.expectError("DELETE", path, nil, 409, codeRevisionConflict, ifMatchHeader(created.Revision))
	if _, headers = h.request("DELETE", path, nil, 204, ifMatchHeader(disabled.Revision)); headers.Get("Content-Type") != "" {
		t.Fatal("204 with a body type", headers)
	}
	// The deleted vendor is gone from every vendor operation.
	h.expectError("GET", path, nil, 404, codeVendorNotFound, nil)
	h.expectError("DELETE", path, nil, 404, codeVendorNotFound, ifMatchHeader(disabled.Revision))
	h.expectError("PATCH", path, map[string]any{"enabled": true}, 404, codeVendorNotFound, ifMatchHeader(disabled.Revision))
	h.expectError("GET", path+"/configuration", nil, 404, codeVendorNotFound, nil)
	h.expectError("PATCH", path+"/configuration", map[string]any{}, 404, codeVendorNotFound, ifMatchHeader(disabled.Revision))
	h.expectError("GET", path+"/admin-notes", nil, 404, codeVendorNotFound, nil)
	h.expectError("PUT", path+"/admin-notes", map[string]any{"text": ""}, 404, codeVendorNotFound, ifMatchHeader(1))
	h.expectError("GET", "/admin/api/apps?vendor=enterprise", nil, 404, codeVendorNotFound, nil)
	h.expectError("POST", "/admin/api/apps", map[string]any{"vendor": "enterprise", "id": "tool", "provider": "info", "name": input["name"], "enabled": true}, 404, codeVendorNotFound, nil)
}

func TestApplicationCreateAppliesProviderFieldsAndDefaults(t *testing.T) {
	h := newHarness(t)
	h.login("")
	h.createVendor("enterprise")
	name := map[string]string{"en": "Tool", "zh-CN": "工具"}
	create := func(body map[string]any, status int) []byte {
		t.Helper()
		body["vendor"], body["name"], body["enabled"] = "enterprise", name, true
		data, _ := h.request("POST", "/admin/api/apps", body, status, nil)
		return data
	}
	data, headers := h.request("POST", "/admin/api/apps", map[string]any{"vendor": "enterprise", "id": "codex", "provider": "codex", "name": name, "enabled": true}, 201, nil)
	codex := decodeJSONBody[appDTO](t, data)
	providers := getJSON[providerListDTO](h, "/admin/api/providers")
	defaultBase := ""
	for _, p := range providers.Items {
		if p.Key == "codex" {
			defaultBase = *p.DefaultBaseURL
		}
	}
	if headers.Get("Location") != "/admin/api/apps/enterprise/codex" || headers.Get("ETag") != etag(codex.Revision) || codex.BaseURL == nil || *codex.BaseURL != defaultBase || codex.CacheTTLSeconds == nil || *codex.CacheTTLSeconds != 60 || codex.SourceEpoch != 1 || codex.BaseURLs != nil {
		t.Fatal("release defaults", string(data))
	}
	if !bytes.Contains(data, []byte(`"base_url"`)) || bytes.Contains(data, []byte(`"base_urls"`)) || bytes.Contains(data, []byte(`"source_strategy"`)) {
		t.Fatal("release application fields", string(data))
	}
	files := decodeJSONBody[appDTO](t, create(map[string]any{"id": "files", "provider": "http-cache", "base_urls": []string{"http://intranet.example:8081/packages/", "https://mirror.example/packages"}, "tags": []string{"#Mirror"}}, 201))
	if len(files.BaseURLs) != 2 || files.BaseURLs[0] != "http://intranet.example:8081/packages" || files.SourceStrategy != "ordered" || *files.CacheTTLSeconds != 300 || files.BaseURL != nil || strings.Join(files.Tags, ",") != "Mirror" {
		t.Fatal("http-cache defaults", files)
	}
	zero := decodeJSONBody[appDTO](t, create(map[string]any{"id": "zero", "provider": "http-cache", "base_urls": []string{"http://intranet.example/zero"}, "cache_ttl_seconds": 0, "source_strategy": "random"}, 201))
	if *zero.CacheTTLSeconds != 0 || zero.SourceStrategy != "random" {
		t.Fatal("explicit zero TTL or strategy lost", zero)
	}
	data = create(map[string]any{"id": "about", "provider": "info"}, 201)
	if about := decodeJSONBody[appDTO](t, data); about.SourceEpoch != 0 || bytes.Contains(data, []byte(`"cache_ttl_seconds"`)) || bytes.Contains(data, []byte(`"base_url`)) {
		t.Fatal("info application has upstream fields", string(data))
	}
	for _, invalid := range []map[string]any{
		{"id": "missing-base", "provider": "http-cache"},
		{"id": "single-base", "provider": "http-cache", "base_url": "http://intranet.example/files"},
		{"id": "info-base", "provider": "info", "base_url": "https://example.com"},
		{"id": "hosted-ttl", "provider": "hosted", "cache_ttl_seconds": 1},
		{"id": "codex-sources", "provider": "codex", "base_urls": []string{"https://example.com"}},
		{"id": "codex-zero", "provider": "codex", "cache_ttl_seconds": 0},
		{"id": "credentials", "provider": "codex", "base_url": "https://user:secret@example.com"},
		{"id": "unknown-category", "provider": "info", "categories": []string{"missing"}},
		{"id": "Bad", "provider": "info"},
		{"id": "bad-provider", "provider": "plugin"},
	} {
		expectCode(t, create(invalid, 400), codeValidationFailed)
	}
	expectCode(t, create(map[string]any{"id": "files", "provider": "info"}, 409), codeAlreadyExists)
	h.expectError("POST", "/admin/api/apps", map[string]any{"vendor": "missing", "id": "tool", "provider": "info", "name": name, "enabled": true}, 404, codeVendorNotFound, nil)
	h.expectError("POST", "/admin/api/apps", map[string]any{"vendor": "enterprise", "id": "tool", "provider": "info", "name": name}, 400, codeInvalidRequest, nil)
	h.expectError("POST", "/admin/api/apps", map[string]any{"vendor": "enterprise", "id": "tool", "provider": "info", "name": name, "enabled": true, "localized_icons": map[string]string{"en": "", "zh-CN": ""}}, 400, codeInvalidRequest, nil)
	gone := h.createVendor("gone")
	if err := h.store.DeleteVendor(gone.ID, gone.Revision); err != nil {
		t.Fatal(err)
	}
	h.expectError("POST", "/admin/api/apps", map[string]any{"vendor": "gone", "id": "tool", "provider": "info", "name": name, "enabled": true}, 409, codeEntityDeleted, nil)
	_, public := h.publicCatalog()
	if public["enterprise/files"].Provider != application.HttpCache || public["enterprise/files"].Vendor.Name.ZhCN != "企业" {
		t.Fatal("created application is not published", public)
	}
}

func TestApplicationStateToggleChangesOnlyEnabled(t *testing.T) {
	h := newHarness(t)
	h.login("")
	before := h.adminApp("openai/codex")
	path := "/admin/api/apps/openai/codex"
	h.expectError("PATCH", path, map[string]any{"enabled": false, "base_url": "https://example.com"}, 400, codeInvalidRequest, ifMatchHeader(before.Revision))
	h.expectError("PATCH", path, map[string]any{}, 400, codeInvalidRequest, ifMatchHeader(before.Revision))
	data, _ := h.request("PATCH", path, map[string]any{"enabled": false}, 200, ifMatchHeader(before.Revision))
	after := decodeJSONBody[appDTO](t, data)
	if after.Enabled || after.Revision != before.Revision+1 || after.Name != before.Name || *after.BaseURL != *before.BaseURL || after.SourceEpoch != before.SourceEpoch || after.Icon != before.Icon {
		t.Fatal("enabled toggle changed other fields", after)
	}
	h.request("GET", "/openai/codex/install.sh", nil, 404, nil)
	h.expectError("PATCH", path, map[string]any{"enabled": true}, 409, codeRevisionConflict, ifMatchHeader(before.Revision))
	h.expectError("PATCH", "/admin/api/apps/openai/missing", map[string]any{"enabled": true}, 404, codeApplicationNotFound, ifMatchHeader(1))
	h.createVendor("soft")
	soft := h.createApp("soft", "deleted", application.Info, nil)
	if err := h.store.DeleteApplication(soft.Key, soft.Revision); err != nil {
		t.Fatal(err)
	}
	deleted := h.adminApp(soft.Key)
	if deleted.DeletedAt == nil || deleted.Enabled {
		t.Fatal("deleted applications stay readable", deleted)
	}
	h.expectError("PATCH", "/admin/api/apps/"+soft.Key, map[string]any{"enabled": true}, 409, codeEntityDeleted, ifMatchHeader(deleted.Revision))
}

func TestApplicationPermanentDeleteGuardsIdentityAndIsFinal(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, withDir(dir))
	password := h.password
	h.login("")
	h.createVendor("enterprise")
	app := h.createApp("enterprise", "codex", application.Codex, nil)
	if err := h.store.AddFor(app.MetricsID(), "artifact_requests", 7); err != nil {
		t.Fatal(err)
	}
	path := "/admin/api/apps/" + app.Key
	h.expectError("DELETE", path, nil, 400, codeInvalidQuery, ifMatchHeader(app.Revision))
	h.expectError("DELETE", path+"?confirm_uid=short", nil, 400, codeInvalidQuery, ifMatchHeader(app.Revision))
	h.expectError("DELETE", path+"?confirm_uid="+app.UID, nil, 400, codeIfMatchRequired, nil)
	expectCode(t, h.deleteApp(app.Key, strings.Repeat("0", 32), app.Revision, 409), codeRevisionConflict)
	expectCode(t, h.deleteApp(app.Key, app.UID, app.Revision+1, 409), codeRevisionConflict)
	builtin := h.adminApp("openai/codex")
	expectCode(t, h.deleteApp(builtin.Key, builtin.UID, builtin.Revision, 409), codeBuiltinProtected)
	data := h.deleteApp(app.Key, app.UID, app.Revision, 200)
	if deletion := decodeJSONBody[appDeletionDTO](t, data); deletion.CleanupPending {
		t.Fatal("cleanup pending without files", string(data))
	}
	// A retry after success is not found; a recreated application keeps its new identity.
	expectCode(t, h.deleteApp(app.Key, app.UID, app.Revision, 404), codeApplicationNotFound)
	replacement := h.createApp("enterprise", "codex", application.Info, nil)
	expectCode(t, h.deleteApp(app.Key, app.UID, replacement.Revision, 409), codeRevisionConflict)
	if counters, err := h.store.CountersFor(app.MetricsID()); err != nil || len(counters) != 0 {
		t.Fatal("deleted history survived", counters, err)
	}
	h.deleteApp(replacement.Key, replacement.UID, replacement.Revision, 200)
	expectGoneApplication(h, app.Key, replacement.Revision)
	h.close()
	h = newHarness(t, withDir(dir))
	if h.password != "" {
		t.Fatal("administrator credentials reset")
	}
	h.login(password)
	h.expectError("GET", path, nil, 404, codeApplicationNotFound, nil)
	page := getJSON[pageDTO[appListItemDTO]](h, "/admin/api/apps?state=deleted")
	if page.Total != 0 {
		t.Fatal("permanent removal left tombstones", page)
	}
}

// expectGoneApplication requires every application operation to report a
// permanently deleted application as not found.
func expectGoneApplication(h *harness, key string, revision int64) {
	h.t.Helper()
	path := "/admin/api/apps/" + key
	h.expectError("GET", path, nil, 404, codeApplicationNotFound, nil)
	h.expectError("PATCH", path, map[string]any{"enabled": true}, 404, codeApplicationNotFound, ifMatchHeader(revision))
	h.expectError("GET", path+"/configuration", nil, 404, codeApplicationNotFound, nil)
	h.expectError("PATCH", path+"/configuration", map[string]any{"set": map[string]any{"name.en": "x"}}, 404, codeApplicationNotFound, ifMatchHeader(revision))
	h.expectError("GET", path+"/admin-notes", nil, 404, codeApplicationNotFound, nil)
	h.expectError("PUT", path+"/admin-notes", map[string]any{"text": ""}, 404, codeApplicationNotFound, ifMatchHeader(1))
	h.expectError("POST", path+"/copy", map[string]any{"source_uid": strings.Repeat("0", 32), "target_vendor": "openai", "target_id": "gone-copy", "mode": "independent", "include_notes": false}, 404, codeApplicationNotFound, ifMatchHeader(revision))
	h.expectError("POST", "/admin/api/configuration/export", map[string]any{"selection": []map[string]any{{"kind": "app", "key": key}}, "mode": "linked", "include_notes": false, "include_proxy_credentials": false}, 404, codeApplicationNotFound, nil)
}

func TestIconUploadValidatesAndServesInertImages(t *testing.T) {
	h := newHarness(t)
	h.login("")
	h.createVendor("enterprise")
	static := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#123456" d="M0 0 L24 0 L12 24 Z"/></svg>`
	body, contentType := multipartForm(t, [3]string{"file", "a.svg", static})
	if code, data, _ := h.raw("POST", "/admin/api/icons", body, contentType, map[string]string{"X-CSRF-Token": ""}); code != 403 {
		t.Fatal("upload without CSRF", code, string(data))
	}
	h.uploadIcon(`<svg onload="alert(1)"><script>alert(1)</script></svg>`, 400)
	code, data, _ := h.raw("POST", "/admin/api/icons", strings.NewReader(static), "image/svg+xml", nil)
	if code != 415 {
		t.Fatal("non-multipart upload", code, string(data))
	}
	expectCode(t, data, codeUnsupportedMediaType)
	body, contentType = multipartForm(t, [3]string{"file", "a.svg", static}, [3]string{"file", "b.svg", static})
	code, data, _ = h.raw("POST", "/admin/api/icons", body, contentType, nil)
	if code != 400 {
		t.Fatal("two files accepted", code)
	}
	expectCode(t, data, codeInvalidRequest)
	path := h.uploadIcon(static, 201)
	if again := h.uploadIcon(static, 201); again != path || !strings.HasPrefix(path, media.PublicPrefix) || strings.Contains(path, "untrusted-name") {
		t.Fatal("icon storage is not content addressed", path, again)
	}
	served, headers := h.request("GET", path, nil, 200, nil)
	if !bytes.Contains(served, []byte("#123456")) || headers.Get("Content-Type") != "image/svg+xml" || headers.Get("Content-Security-Policy") != "sandbox; default-src 'none'" || !strings.Contains(headers.Get("Cache-Control"), "immutable") {
		t.Fatal("uploaded SVG lost the inert response contract", headers)
	}
	// Only uploaded or preset icons can be selected.
	expectCode(t, h.patchConfiguration("vendors/enterprise", map[string]any{"set": map[string]any{"icon": media.PublicPrefix + strings.Repeat("f", 64) + ".png"}}, 400), codeValidationFailed)
	h.patchConfiguration("vendors/enterprise", map[string]any{"set": map[string]any{"icon": path}}, 200)
	app := h.createApp("enterprise", "files", application.HttpCache, map[string]any{"base_url": "http://intranet.example/files"})
	if _, public := h.publicCatalog(); public[app.Key].Vendor.Icon != path {
		t.Fatal("published vendor icon", public[app.Key])
	}
	h.request("GET", media.PublicPrefix+"untrusted-name.svg", nil, 404, nil)
}
