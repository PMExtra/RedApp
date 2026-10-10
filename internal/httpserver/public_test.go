package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
)

func TestSPADocumentsFollowTheRouteFamilies(t *testing.T) {
	h := newHarness(t)
	h.createVendor("acme")
	h.createApp("acme", "hidden", "info", map[string]any{"enabled": false})
	document := func(path string, status int, entry string) {
		t.Helper()
		code, body, header := h.raw("GET", path, nil, "", nil)
		if code != status || entryDocument(body) != entry || !strings.Contains(header.Get("Content-Type"), "text/html") || !strings.Contains(header.Get("Content-Security-Policy"), "script-src 'self'") {
			t.Fatalf("%s: %d %q %v", path, code, entryDocument(body), header)
		}
	}
	for _, path := range []string{"/", "/all", "/all?q=x&category=tools&page=2", "/openai", "/openai/codex", "/anthropic/claude-code", "/acme"} {
		document(path, 200, "public")
	}
	for _, path := range []string{"/admin/login?returnTo=/admin/vendors", "/admin/overview", "/admin/settings/site", "/admin/vendors?state=deleted&cleanup=pending", "/admin/vendors/acme/apps/hidden/settings", "/admin/vendors/anthropic/apps/claude-code/settings", "/admin/vendors/anthropic/apps/claude-code/versions", "/admin/vendors/anthropic/apps/claude-code", "/admin/vendors/acme/apps/hidden", "/admin/vendors/openai/apps/new"} {
		document(path, 200, "admin")
	}
	// Unknown admin pages, missing objects and unsupported tabs: admin document with 404.
	for _, path := range []string{"/admin/missing", "/admin/vendors/missing", "/admin/vendors/anthropic/apps/claude-code/files", "/admin/vendors/anthropic/apps/missing/settings", "/admin/vendors/anthropic/apps/missing", "/admin/vendors/missing/apps/codex", "/admin/a/b/c/d", "/admin/api"} {
		document(path, 404, "admin")
	}
	// Unknown or unpublished vendors and applications: public document with 404.
	for _, path := range []string{"/missing", "/install.sh", "/acme/hidden", "/acme/missing", "/apps/codex", "/Bad/codex", "/all/x"} {
		document(path, 404, "public")
	}
	for _, tc := range []struct {
		path string
		code errorCode
	}{
		{"/openai/codex/missing", codeFileNotFound},
		{"/acme/hidden/file", codeApplicationNotFound},
		{"/api/x/y/z", codeNotFound},
		{"/api/info", codeNotFound},
		{"/health", codeNotFound},
		{"/assets/a/b/c", codeNotFound},
		{"/admin/vendors?state=gone", codeInvalidQuery},
		{"/openai?unknown=1", codeInvalidQuery},
	} {
		code, body, _ := h.raw("GET", tc.path, nil, "", nil)
		if code < 400 || errorCodeOf(t, body) != string(tc.code) {
			t.Fatalf("%s: %d %s", tc.path, code, body)
		}
	}
	if code, _, header := h.raw("GET", "/admin", nil, "", nil); code != 307 || header.Get("Location") != "/admin/overview" {
		t.Fatal("admin redirect", code, header)
	}
	if code, _, header := h.raw("GET", "/openai/codex/", nil, "", nil); code != 308 || header.Get("Location") != "/openai/codex" {
		t.Fatal("trailing slash redirect", code, header)
	}
	for _, path := range []string{"/assets/public-fixture.js", "/assets/style-fixture.css", "/assets/JetBrainsMono-OFL-v2.304.txt"} {
		h.request("GET", path, nil, 200, nil)
	}
}

// The committed bundle must provide both entry documents and every asset they reference.
func TestEmbeddedFrontendBundleIsServable(t *testing.T) {
	h := newHarness(t, withEmbeddedFrontend())
	code, page, _ := h.raw("GET", "/", nil, "", nil)
	if code != 200 {
		t.Fatal("public document", code)
	}
	if _, err := fs.Stat(h.server.frontend, adminSPADocument); err != nil {
		// The pre-rewrite bundle has no admin entry; admin pages then fail
		// instead of falling back to the public document.
		h.expectError("GET", "/admin/overview", nil, 500, codeInternalError, nil)
	} else {
		code, admin, _ := h.raw("GET", "/admin/overview", nil, "", nil)
		if code != 200 {
			t.Fatal("admin document", code)
		}
		page = append(page, admin...)
	}
	assets := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllSubmatch(page, -1)
	if len(assets) == 0 {
		t.Fatal("entry documents reference no assets")
	}
	for _, asset := range assets {
		h.request("GET", string(asset[1]), nil, 200, nil)
	}
}

func TestBootstrapHasNoApplicationData(t *testing.T) {
	h := newHarness(t)
	data, _ := h.request("GET", "/api/bootstrap", nil, 200, nil)
	var info map[string]json.RawMessage
	if json.Unmarshal(data, &info) != nil || len(info) != 6 || string(info["version"]) != `"test"` || !strings.HasPrefix(string(info["public_url"]), `"http://127.0.0.1:`) {
		t.Fatal(string(data))
	}
	for _, secret := range []string{"password", "has_credentials", "upstream_proxy", "apps"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("bootstrap carries", secret)
		}
	}
	revision := string(info["revision"])
	public := "https://published.example"
	if _, err := h.server.public.Set(&public, 1); err != nil {
		t.Fatal(err)
	}
	data, _ = h.request("GET", "/api/bootstrap", nil, 200, nil)
	if err := json.Unmarshal(data, &info); err != nil || string(info["public_url"]) != `"`+public+`"` || string(info["revision"]) == revision {
		t.Fatal("public URL change did not change the bootstrap revision", string(data))
	}
}

func TestPublicApplicationsCatalogAndSearch(t *testing.T) {
	h := newHarness(t)
	vendor := h.createVendor("acme")
	files := h.createApp(vendor.ID, "files", application.Hosted, map[string]any{"tags": []string{"secret-tag"}})
	h.createApp(vendor.ID, "hidden", application.Info, map[string]any{"enabled": false})
	data, _ := h.request("GET", "/api/apps/acme/files", nil, 200, nil)
	app := decodeJSONBody[publicAppDTO](t, data)
	if app.Key != "acme/files" || app.ID != "files" || app.Vendor.ID != "acme" || app.Vendor.Name.ZhCN != "企业" || !app.Capabilities.HostedFiles || app.LatestKnownVersion != nil || app.InstructionsAvailable.En {
		t.Fatal(string(data))
	}
	for _, private := range []string{files.UID, "secret-tag", "base_url", "objects/"} {
		if bytes.Contains(data, []byte(private)) {
			t.Fatal("public application exposes", private)
		}
	}
	h.expectError("GET", "/api/apps/acme/hidden", nil, 404, codeApplicationNotFound, nil)
	h.expectError("GET", "/api/apps/acme/hidden/instructions/document?lang=en", nil, 404, codeApplicationNotFound, nil)
	h.expectError("GET", "/api/apps/ACME/files", nil, 400, codeInvalidPath, nil)
	h.expectError("GET", "/api/vendors/admin", nil, 400, codeInvalidPath, nil)
	h.expectError("GET", "/api/vendors/missing", nil, 404, codeVendorNotFound, nil)
	data, _ = h.request("GET", "/api/vendors/acme", nil, 200, nil)
	if v := decodeJSONBody[publicVendorDTO](t, data); v.ID != "acme" || v.Name.En != "Enterprise" {
		t.Fatal(string(data))
	}
	h.expectError("GET", "/api/vendors/acme?page=1", nil, 400, codeInvalidQuery, nil)

	data, _ = h.request("GET", "/api/catalog?vendor=acme", nil, 200, nil)
	page := decodeJSONBody[catalogPageDTO](t, data)
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Key != "acme/files" || page.Limit != 24 {
		t.Fatal(string(data))
	}
	data, _ = h.request("GET", "/api/catalog?q=secret-tag", nil, 200, nil)
	if page = decodeJSONBody[catalogPageDTO](t, data); page.Total != 1 || bytes.Contains(data, []byte("secret-tag")) {
		t.Fatal("tag search", string(data))
	}
	data, _ = h.request("GET", "/api/catalog?page=50&limit=1", nil, 200, nil)
	if page = decodeJSONBody[catalogPageDTO](t, data); page.Page != 50 || len(page.Items) != 0 || page.Total < 3 {
		t.Fatal("page beyond the end", string(data))
	}
	for _, query := range []string{"?vendor=Bad", "?category=Bad", "?limit=0", "?limit=101", "?page=0", "?page=x", "?q=" + strings.Repeat("x", 129), "?sort=name"} {
		h.expectError("GET", "/api/catalog"+query, nil, 400, codeInvalidQuery, nil)
	}
	h.expectError("GET", "/api/catalog?vendor=missing", nil, 404, codeVendorNotFound, nil)

	data, _ = h.request("GET", "/api/search?q=ACME", nil, 200, nil)
	var search struct {
		Items []searchHitDTO `json:"items"`
	}
	json.Unmarshal(data, &search)
	if len(search.Items) != 2 || search.Items[0].Kind != "vendor" || search.Items[0].Key != "acme" || search.Items[0].LocalizedIcons == nil || search.Items[1].Key != "acme/files" || search.Items[1].LocalizedIcons != nil {
		t.Fatal(string(data))
	}
	if data, _ = h.request("GET", "/api/search", nil, 200, nil); string(data) != "{\"items\":[]}\n" {
		t.Fatal("empty search", string(data))
	}
}

func TestPublicHostedFileListAndDownload(t *testing.T) {
	h := newHarness(t)
	vendor := h.createVendor("acme")
	app := h.createApp(vendor.ID, "files", application.Hosted, nil)
	entry, _ := h.server.registry.Lookup(app.Key)
	content := []byte("hosted payload")
	file, err := h.server.hosted.Put(context.Background(), entry, "tools/setup one.bin", "", "0123456789abcdef0123456789abcdef", func(context.Context) (io.ReadCloser, int64, error) {
		return io.NopCloser(bytes.NewReader(content)), int64(len(content)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := h.request("GET", "/api/apps/acme/files/files?limit=1", nil, 200, nil)
	page := decodeJSONBody[pageDTO[hostedFileDTO]](t, data)
	if page.Total != 1 || page.Limit != 1 || len(page.Items) != 1 || page.Items[0].Path != "tools/setup one.bin" || page.Items[0].ID != file.ID || page.Items[0].SizeBytes != int64(len(content)) {
		t.Fatal(string(data))
	}
	if bytes.Contains(data, []byte(entry.UID)) || bytes.Contains(data, []byte("objects")) {
		t.Fatal("file list exposes internal identifiers", string(data))
	}
	body, header := h.request("GET", "/acme/files/tools/setup%20one.bin", nil, 200, nil)
	if !bytes.Equal(body, content) || header.Get("ETag") != `"sha256-`+file.SHA256+`"` || !strings.Contains(header.Get("Content-Disposition"), "attachment") || header.Get("Cache-Control") != "no-cache" {
		t.Fatal(string(body), header)
	}
	if body, _ = h.request("GET", "/acme/files/tools/setup%20one.bin", nil, 206, map[string]string{"Range": "bytes=0-5"}); string(body) != "hosted" {
		t.Fatal("range", string(body))
	}
	h.request("GET", "/acme/files/tools/setup%20one.bin", nil, 304, map[string]string{"If-None-Match": header.Get("ETag")})
	if body, _ = h.request("HEAD", "/acme/files/tools/setup%20one.bin", nil, 200, nil); len(body) != 0 {
		t.Fatal("HEAD body")
	}
	h.expectError("GET", "/acme/files/tools/missing.bin", nil, 404, codeFileNotFound, nil)
	h.expectError("GET", "/acme/files/tools/setup%20one.bin?download=1", nil, 400, codeInvalidPath, nil)
	h.expectError("POST", "/acme/files/tools/setup%20one.bin", nil, 405, codeMethodNotAllowed, nil)
	h.expectError("GET", "/api/apps/openai/codex/files", nil, 404, codeCapabilityUnsupported, nil)
	h.expectError("GET", "/api/apps/acme/files/files?limit=500", nil, 400, codeInvalidQuery, nil)
}
