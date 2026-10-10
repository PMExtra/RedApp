package httpserver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/pathmatch"
)

func TestPathMatchUsesCanonicalDecodedPaths(t *testing.T) {
	h := newHarness(t)
	h.login("")
	const endpoint = "/admin/api/path-match"
	for _, test := range []struct {
		kind, pattern, path string
		matches             bool
	}{
		{"glob", "/", "/any/nested/file.bin", true},
		{"glob", "/downloads/", "/downloads/a.bin", true},
		{"glob", "/downloads/", "/downloads", false},
		{"re2", `/downloads/[^/]+\.bin`, "/downloads/a.bin", true},
		{"re2", `/downloads/[^/]+\.bin`, "/other/downloads/a.bin", false},
		{"re2", `/downloads/[^/]+\.bin`, "/downloads/a.bin/signature", false},
		{"glob", "/资料/*", "/资料/file name.bin", true},
	} {
		body, _ := h.request("POST", endpoint, map[string]any{"match": pathmatch.Spec{Type: test.kind, Pattern: test.pattern}, "path": test.path}, 200, nil)
		result := decodeJSONBody[pathMatchResultDTO](t, body)
		if result.Matches != test.matches || result.Path != test.path {
			t.Fatalf("path matcher changed path or semantics: %s", body)
		}
	}
	glob := pathmatch.Spec{Type: "glob", Pattern: "/"}
	for _, path := range []string{"relative/file", "/a/../b", "/a//b", "/a?secret=yes", "/a#fragment", "/a\\b", "/a%2fb", "/a%252fb", "/%2e%2e/file", "/a\x00b", "/" + strings.Repeat("a", 4096)} {
		h.expectError("POST", endpoint, map[string]any{"match": glob, "path": path}, 400, codeValidationFailed, nil)
	}
	for _, invalid := range []pathmatch.Spec{{Type: "re2", Pattern: "["}, {Type: "glob", Pattern: "/["}, {Type: "regex", Pattern: "/"}, {Type: "glob", Pattern: "/" + strings.Repeat("a", pathmatch.MaxPatternBytes)}} {
		h.expectError("POST", endpoint, map[string]any{"match": invalid, "path": "/a"}, 400, codeValidationFailed, nil)
	}
	h.expectError("POST", endpoint, map[string]any{"match": glob, "path": "/a", "decode_twice": true}, 400, codeInvalidRequest, nil)
	h.expectError("POST", endpoint, map[string]any{"match": glob}, 400, codeInvalidRequest, nil)
	h.expectError("POST", endpoint, map[string]any{"match": glob, "path": "/a"}, 403, codeCSRFRejected, map[string]string{"X-CSRF-Token": ""})
}

func TestAutoCleanupStatusIsServiceWide(t *testing.T) {
	h := newHarness(t)
	h.expectError("GET", "/admin/api/cache/auto-cleanup", nil, 401, codeAuthRequired, nil)
	h.login("")
	body, _ := h.request("GET", "/admin/api/cache/auto-cleanup", nil, 200, nil)
	status := decodeJSONBody[autoCleanupStatusDTO](t, body)
	if status.IntervalSeconds != 900 || status.ScanLimitPerApp != 1000 || status.RetireLimitPerApp != 100 || status.LastAttemptAt != nil || status.LastError != nil {
		t.Fatalf("scheduler bounds or initial state missing: %s", body)
	}
	h.expectError("GET", "/admin/api/cache/auto-cleanup?app=policy-test/files", nil, 400, codeInvalidQuery, nil)
}

func TestSourcesListEpochsWithProviderFields(t *testing.T) {
	h := newHarness(t)
	h.expectError("GET", "/admin/api/apps/openai/codex/sources", nil, 401, codeAuthRequired, nil)
	h.login("")
	h.createVendor("source-test")
	base := []string{"http://127.0.0.1:9/first", "http://127.0.0.1:9/second"}
	app := h.createApp("source-test", "files", application.HttpCache, map[string]any{"base_urls": base})
	api := "/admin/api/apps/" + app.Key
	sources := func(path string) sourceListDTO {
		t.Helper()
		body, _ := h.request("GET", path+"/sources", nil, 200, nil)
		return decodeJSONBody[sourceListDTO](t, body)
	}
	h.patchApp(app.Key, map[string]any{"cache_ttl_seconds": 60})
	if list := sources(api); len(list.Items) != 1 || list.Items[0].Epoch != 1 {
		t.Fatal("a TTL edit created a source epoch", list)
	}
	reordered := []string{base[1], base[0]}
	h.patchApp(app.Key, map[string]any{"base_urls": reordered})
	h.patchApp(app.Key, map[string]any{"source_strategy": "round_robin"})
	list := sources(api)
	if len(list.Items) != 3 {
		t.Fatal("source order and strategy changes did not isolate their epochs", list)
	}
	for i, item := range list.Items {
		if item.Epoch != int64(i+1) || item.Current != (i == 2) || item.Active != (i == 2) || item.BaseURL != nil || item.SourceStrategy == nil || len(item.BaseURLs) != 2 {
			t.Fatal("HTTP cache epochs lost order, eligibility or provider fields", list)
		}
	}
	if !reflect.DeepEqual(list.Items[0].BaseURLs, base) || !reflect.DeepEqual(list.Items[2].BaseURLs, reordered) || *list.Items[2].SourceStrategy != "round_robin" {
		t.Fatal("source snapshots changed", list)
	}
	// Release applications list their single base URL.
	codex := sources("/admin/api/apps/openai/codex")
	if len(codex.Items) != 1 || codex.Items[0].BaseURL == nil || codex.Items[0].BaseURLs != nil || codex.Items[0].SourceStrategy != nil {
		t.Fatal("release source fields", codex)
	}
	h.createApp("source-test", "about", application.Info, nil)
	h.expectError("GET", "/admin/api/apps/source-test/about/sources", nil, 404, codeCapabilityUnsupported, nil)
	h.expectError("GET", "/admin/api/apps/source-test/unknown/sources", nil, 404, codeApplicationNotFound, nil)
	h.expectError("GET", "/admin/api/apps/source-test/Files/sources", nil, 400, codeInvalidPath, nil)
	// Disabled and deleted applications stay inspectable; no epoch is active.
	h.setAppEnabled(app.Key, false)
	for _, item := range sources(api).Items {
		if item.Active {
			t.Fatal("disabled source remained active", item)
		}
	}
	row, _ := h.store.Application(app.Key)
	if err := h.store.DeleteApplication(app.Key, row.Revision); err != nil {
		t.Fatal(err)
	}
	if list := sources(api); len(list.Items) != 3 {
		t.Fatal("deleted application lost its sources", list)
	}
	// Once its data is purged the application no longer exists.
	row, _ = h.store.Application(app.Key)
	if err := h.store.PermanentlyDeleteApplication(app.Key, row.Revision); err != nil {
		t.Fatal(err)
	}
	h.expectError("GET", api+"/sources", nil, 404, codeApplicationNotFound, nil)
	h.expectError("GET", api+"/status", nil, 404, codeApplicationNotFound, nil)
}

func TestCacheEntriesPaginateByPathPerSourceEpoch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		fmt.Fprint(w, r.URL.Path)
	}))
	defer upstream.Close()
	h, app, api := policyHTTPApp(t, upstream.URL+"/files")
	// Paths longer than the cursor prefix share it, so continuation must count
	// rows inside the prefix block instead of storing the whole path.
	long := strings.Repeat("l", 900)
	paths := []string{"/a.bin", "/b/c.bin", "/" + long + "/1.bin", "/" + long + "/2.bin", "/" + long + "/3.bin", "/" + long + "x.bin", "/z.bin"}
	for _, path := range paths {
		h.request("GET", "/"+app.Key+path, nil, 200, nil)
	}
	sort.Strings(paths)
	for _, limit := range []int{1, 2, 3, 100} {
		entries := cacheEntries(t, h, api, fmt.Sprintf("?limit=%d", limit))
		got := []string{}
		for _, entry := range entries {
			got = append(got, entry.Path)
			if entry.SizeBytes != int64(len("/files"+entry.Path)) || entry.ETag == nil || entry.SourceURL != upstream.URL+"/files"+entry.Path {
				t.Fatal("entry fields", entry)
			}
		}
		if !reflect.DeepEqual(got, paths) {
			t.Fatalf("limit %d: pages skipped, repeated or reordered paths: %q", limit, got)
		}
	}
	body, _ := h.request("GET", api+"/cache/entries?limit=1", nil, 200, nil)
	first := decodeJSONBody[cursorPage[cacheEntryDTO]](t, body)
	if len(first.Items) != 1 || first.NextCursor == nil || len(*first.NextCursor) > maxCursorLength {
		t.Fatal("first page", string(body))
	}
	// A new source epoch has its own entries; the old one stays readable and
	// its cursors are bound to it.
	h.patchApp(app.Key, map[string]any{"base_urls": []string{upstream.URL + "/other"}})
	h.request("GET", "/"+app.Key+"/a.bin", nil, 200, nil)
	if current := cacheEntries(t, h, api, ""); len(current) != 1 || current[0].SourceURL != upstream.URL+"/other/a.bin" {
		t.Fatal("current epoch listing", current)
	}
	if old := cacheEntries(t, h, api, "?source_epoch=1"); len(old) != len(paths) {
		t.Fatal("historical epoch listing", len(old))
	}
	h.request("GET", api+"/cache/entries?source_epoch=1&cursor="+*first.NextCursor, nil, 200, nil)
	for query, code := range map[string]errorCode{
		"?cursor=" + *first.NextCursor:                             codeInvalidCursor,
		"?source_epoch=2&cursor=" + *first.NextCursor:              codeInvalidCursor,
		"?cursor=not-a-cursor":                                     codeInvalidCursor,
		"?cursor=" + url.QueryEscape("eyJvcCI6Imxpc3RFdmVudHMifQ"): codeInvalidCursor,
		"?cursor=" + strings.Repeat("A", maxCursorLength+1):        codeInvalidCursor,
		"?source_epoch=01":                                         codeInvalidQuery,
		"?source_epoch=0":                                          codeInvalidQuery,
		"?source_epoch=1&source_epoch=2":                           codeInvalidQuery,
		"?limit=101":                                               codeInvalidQuery,
		"?unexpected=1":                                            codeInvalidQuery,
	} {
		h.expectError("GET", api+"/cache/entries"+query, nil, 400, code, nil)
	}
	h.expectError("GET", api+"/cache/entries?source_epoch=3", nil, 404, codeSourceNotFound, nil)
	h.expectError("GET", "/admin/api/apps/openai/codex/cache/entries", nil, 404, codeCapabilityUnsupported, nil)
	// A cursor of another application is rejected.
	other := h.createApp("policy-test", "other", application.HttpCache, map[string]any{"base_url": upstream.URL + "/files"})
	h.expectError("GET", "/admin/api/apps/"+other.Key+"/cache/entries?cursor="+*first.NextCursor, nil, 400, codeInvalidCursor, nil)
	// The removed whole-list route is gone.
	h.request("GET", api+"/cache", nil, 404, nil)
}
