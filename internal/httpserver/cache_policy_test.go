package httpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/httpcache"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
)

type policyAPIResponse struct {
	Revision      int64                     `json:"revision"`
	Rules         []cachepolicy.CacheRule   `json:"rules"`
	AutoCleanup   []cachepolicy.CleanupRule `json:"auto_cleanup"`
	StaleFallback *bool                     `json:"stale_fallback"`
}

func decodePolicyAPI(t *testing.T, body []byte, headers http.Header) policyAPIResponse {
	t.Helper()
	var result policyAPIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Revision < 1 || headers.Get("ETag") != fmt.Sprintf(`"%d"`, result.Revision) || result.Rules == nil || result.AutoCleanup == nil || result.StaleFallback == nil {
		t.Fatalf("incomplete policy response/ETag: %s %v", body, headers)
	}
	return result
}

func readPolicyAPI(t *testing.T, h *harness, endpoint string) policyAPIResponse {
	t.Helper()
	body, headers := h.request("GET", endpoint, nil, 200, nil)
	return decodePolicyAPI(t, body, headers)
}

func savePolicyAPI(t *testing.T, h *harness, endpoint string, revision int64, config any) policyAPIResponse {
	t.Helper()
	body, headers := h.request("PUT", endpoint, config, 200, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, revision)})
	return decodePolicyAPI(t, body, headers)
}

func policyHTTPApp(t *testing.T, source string) (*harness, store.Application, string) {
	t.Helper()
	h := newHarness(t)
	h.login(h.password)
	vendor := h.createVendor("policy-test")
	app := h.createApp(vendor.ID, "files", application.HttpCache, map[string]any{"base_url": source})
	return h, app, "/admin/api/apps/" + app.Key
}

func TestHTTPCachePolicyAPIAuthorizationCASAndPersistence(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, withDir(dir))
	h.request("GET", "/admin/api/apps/openai/codex/cache/policy", nil, 401, nil)
	h.login(h.password)
	h.request("GET", "/admin/api/apps/openai/codex/cache/policy", nil, 404, nil)
	vendor := h.createVendor("policy-test")
	app := h.createApp(vendor.ID, "files", application.HttpCache, map[string]any{"base_url": "http://127.0.0.1:9/files"})
	endpoint := "/admin/api/apps/" + app.Key + "/cache/policy"
	initial := readPolicyAPI(t, h, endpoint)
	if initial.Revision != app.Revision || len(initial.Rules) != 0 || len(initial.AutoCleanup) != 0 || !*initial.StaleFallback {
		t.Fatal("new application policy defaults changed", initial)
	}
	config := map[string]any{
		"rules": []map[string]any{
			{"match": pathmatch.Spec{Type: "glob", Pattern: "/downloads/"}, "ttl_seconds": 0},
			{"match": pathmatch.Spec{Type: "re2", Pattern: `/packages/[^/]+\.zip`}, "ttl_seconds": 3600},
		},
		"auto_cleanup":   []map[string]any{{"match": pathmatch.Spec{Type: "glob", Pattern: "/old/**"}, "basis": "last_access", "age_seconds": 86400}},
		"stale_fallback": false,
	}
	etag := fmt.Sprintf(`"%d"`, initial.Revision)
	for _, headers := range []map[string]string{
		{"If-Match": etag, "X-CSRF-Token": ""},
		{"If-Match": etag, "X-CSRF-Token": "wrong-token"},
		{"If-Match": etag, "Origin": "https://other.example"},
	} {
		h.request("PUT", endpoint, config, 403, headers)
	}
	h.request("PUT", endpoint, config, 400, nil)
	h.request("PUT", endpoint, config, 400, map[string]string{"If-Match": `"not-a-revision"`})
	saved := savePolicyAPI(t, h, endpoint, initial.Revision, config)
	if saved.Revision != initial.Revision+1 || *saved.StaleFallback || len(saved.Rules) != 2 || saved.Rules[0].TTLSeconds != 0 || saved.Rules[0].ID == "" || saved.Rules[1].ID == "" || saved.Rules[0].ID == saved.Rules[1].ID {
		t.Fatal("policy order, generated IDs, zero TTL or explicit false was lost", saved)
	}
	persisted, err := h.server.store.Application(app.Key)
	if err != nil || persisted.SourceEpoch != app.SourceEpoch || persisted.Revision != saved.Revision {
		t.Fatalf("policy save changed source epoch or missed directory revision: %+v %v", persisted, err)
	}
	h.request("PUT", endpoint, config, 409, map[string]string{"If-Match": etag})
	if reread := readPolicyAPI(t, h, endpoint); !reflect.DeepEqual(saved, reread) {
		t.Fatal("GET regenerated rule IDs or lost policy data", reread)
	}

	cacheRule := func(match any, ttl any) map[string]any { return map[string]any{"match": match, "ttl_seconds": ttl} }
	glob := pathmatch.Spec{Type: "glob", Pattern: "/"}
	many := make([]map[string]any, cachepolicy.MaxRules+1)
	for i := range many {
		many[i] = cacheRule(glob, 1)
	}
	for name, invalid := range map[string]any{
		"unknown field":  map[string]any{"rules": []any{}, "auto_cleanup": []any{}, "unknown": true},
		"retired mode":   map[string]any{"rules": []any{map[string]any{"match": glob, "ttl_seconds": 0, "mode": "bypass"}}},
		"negative TTL":   map[string]any{"rules": []any{cacheRule(glob, -1)}},
		"TTL limit":      map[string]any{"rules": []any{cacheRule(glob, cachepolicy.MaxTTLSeconds+1)}},
		"fractional TTL": map[string]any{"rules": []any{cacheRule(glob, 0.5)}},
		"invalid RE2":    map[string]any{"rules": []any{cacheRule(pathmatch.Spec{Type: "re2", Pattern: `(?<=prefix)file`}, 0)}},
		"invalid glob":   map[string]any{"rules": []any{cacheRule(pathmatch.Spec{Type: "glob", Pattern: "["}, 0)}},
		"pattern limit":  map[string]any{"rules": []any{cacheRule(pathmatch.Spec{Type: "glob", Pattern: "/" + strings.Repeat("a", pathmatch.MaxPatternBytes)}, 0)}},
		"rule count":     map[string]any{"rules": many},
		"cleanup age":    map[string]any{"auto_cleanup": []any{map[string]any{"match": glob, "basis": "last_access", "age_seconds": cachepolicy.MinAgeSeconds - 1}}},
		"cleanup basis":  map[string]any{"auto_cleanup": []any{map[string]any{"match": glob, "basis": "created", "age_seconds": 86400}}},
		"stale type":     map[string]any{"stale_fallback": "true"},
	} {
		t.Run(name, func(t *testing.T) {
			local := *h
			local.t = t
			local.request("PUT", endpoint, invalid, 400, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, saved.Revision)})
		})
	}
	if after := readPolicyAPI(t, h, endpoint); !reflect.DeepEqual(saved, after) {
		t.Fatal("rejected save mutated the policy", after)
	}
	// Omitting the new switch keeps its documented default; it is not an
	// implicit request to disable fallback when saving older policy forms.
	defaulted := savePolicyAPI(t, h, endpoint, saved.Revision, map[string]any{"rules": saved.Rules, "auto_cleanup": saved.AutoCleanup})
	if !*defaulted.StaleFallback || !reflect.DeepEqual(defaulted.Rules, saved.Rules) || defaulted.Revision != saved.Revision+1 {
		t.Fatal("omitted stale switch changed defaults or regenerated rule IDs", defaulted)
	}
	saved = defaulted
	password := h.password
	h.close()
	restarted := newHarness(t, withDir(dir))
	restarted.login(password)
	if after := readPolicyAPI(t, restarted, endpoint); !reflect.DeepEqual(saved, after) {
		t.Fatal("restart lost policy IDs, revision or stale setting", after)
	}
}

func TestHTTPCacheMatchAPIUsesCanonicalDecodedPaths(t *testing.T) {
	h, _, api := policyHTTPApp(t, "http://127.0.0.1:9/files")
	endpoint := api + "/cache/match"
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
		var result struct {
			Matches       bool   `json:"matches"`
			CanonicalPath string `json:"canonical_path"`
		}
		if err := json.Unmarshal(body, &result); err != nil || result.Matches != test.matches || result.CanonicalPath != test.path {
			t.Fatalf("path matcher changed path/semantics: %s %v", body, err)
		}
	}
	for _, path := range []string{"relative/file", "/a/../b", "/a//b", "/a?secret=yes", "/a#fragment", "/a\\b", "/a%2fb", "/a%252fb", "/%2e%2e/file", "/a\x00b"} {
		h.request("POST", endpoint, map[string]any{"match": pathmatch.Spec{Type: "glob", Pattern: "/"}, "path": path}, 400, nil)
	}
	h.request("POST", endpoint, map[string]any{"match": pathmatch.Spec{Type: "re2", Pattern: "["}, "path": "/a"}, 400, nil)
	h.request("POST", endpoint, map[string]any{"match": pathmatch.Spec{Type: "glob", Pattern: "/"}, "path": "/a", "decode_twice": true}, 400, nil)
	h.request("POST", endpoint, map[string]any{"match": pathmatch.Spec{Type: "glob", Pattern: "/"}, "path": "/a"}, 403, map[string]string{"X-CSRF-Token": ""})
	for _, path := range []string{api + "/cache/policy", api + "/cache/cleanup/status"} {
		h.request("GET", path+"?source_epoch=1", nil, 400, nil)
	}
	h.request("PUT", api+"/cache/policy?source_epoch=1", map[string]any{"rules": []any{}, "auto_cleanup": []any{}}, 400, map[string]string{"If-Match": `"1"`})
	h.request("POST", endpoint+"?source_epoch=1", map[string]any{"match": pathmatch.Spec{Type: "glob", Pattern: "/"}, "path": "/a"}, 400, nil)
	other := h.createApp("policy-test", "other", application.HttpCache, map[string]any{"base_url": "http://127.0.0.1:9/other"})
	body, _ := h.request("GET", api+"/cache/cleanup/status", nil, 200, nil)
	var status httpcache.AutomaticCleanupStatus
	if err := json.Unmarshal(body, &status); err != nil || status.IntervalSeconds != 900 || status.ScanLimitPerApp != 1000 || status.RetireLimitPerApp != 100 {
		t.Fatalf("scheduler bounds missing: %s %v", body, err)
	}
	otherBody, _ := h.request("GET", "/admin/api/apps/"+other.Key+"/cache/cleanup/status", nil, 200, nil)
	if !bytes.Equal(body, otherBody) {
		t.Fatal("scheduler status was incorrectly scoped to an application")
	}
}

func TestHTTPCacheCleanupAPIFreezesMatcherAndFencesRevision(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		fmt.Fprint(w, r.URL.Path)
	}))
	defer upstream.Close()
	h, app, api := policyHTTPApp(t, upstream.URL)
	fetch := func(path string) { t.Helper(); h.request("GET", "/"+app.Key+path, nil, 200, nil) }
	age := func() {
		t.Helper()
		if _, err := h.server.store.DB.Exec(`UPDATE http_cache_generations SET fetched_at_s=? WHERE storage_id=?`, time.Now().Add(-2*time.Hour).Unix(), app.StorageID()); err != nil {
			t.Fatal(err)
		}
	}
	fetch("/remove/first.bin")
	fetch("/keep.bin")
	age()
	before := time.Now().Add(-time.Hour).UTC()
	match := pathmatch.Spec{Type: "glob", Pattern: "/remove/"}
	body, _ := h.request("POST", api+"/cache/cleanup/preview", map[string]any{"match": match, "basis": "fetched_at", "before": before}, 200, nil)
	job := directoryDecode[httpcache.MaintenancePreview](t, body, "job")
	if job.Match != match || job.SelectedFiles != 1 {
		t.Fatal("cleanup preview lost match/selection", job)
	}
	body, _ = h.request("GET", api+"/cache/cleanup/"+job.ID, nil, 200, nil)
	persistedJob := directoryDecode[httpcache.MaintenancePreview](t, body, "job")
	if persistedJob.Match != match || persistedJob.SelectedFiles != 1 {
		t.Fatal("reloading the preview lost its frozen matcher/selection", persistedJob)
	}
	fetch("/remove/later.bin")
	age()
	body, _ = h.request("POST", api+"/cache/cleanup/"+job.ID+"/execute", map[string]any{}, 200, nil)
	result := directoryDecode[httpcache.CleanupResult](t, body, "result")
	if result.SelectedFiles != 1 || result.RetiredFiles != 1 {
		t.Fatal("execution expanded frozen preview", result)
	}
	body, _ = h.request("GET", api+"/cache", nil, 200, nil)
	rows := directoryDecode[[]httpcache.Row](t, body, "items")
	if len(rows) != 2 {
		t.Fatalf("unselected or later cached file was removed: %+v", rows)
	}
	// Legacy callers omit match; this means the root glob, not an empty match.
	body, _ = h.request("POST", api+"/cache/cleanup/preview", map[string]any{"basis": "fetched_at", "before": before}, 200, nil)
	legacy := directoryDecode[httpcache.MaintenancePreview](t, body, "job")
	if legacy.Match != (pathmatch.Spec{Type: "glob", Pattern: "/"}) || legacy.SelectedFiles != 2 {
		t.Fatal("legacy cleanup did not default to all paths", legacy)
	}
	saved := savePolicyAPI(t, h, api+"/cache/policy", app.Revision, map[string]any{"rules": []any{}, "auto_cleanup": []any{}, "stale_fallback": false})
	h.request("POST", api+"/cache/cleanup/"+legacy.ID+"/execute", map[string]any{}, 409, nil)
	h.request("PATCH", api+"/configuration", map[string]any{"set": map[string]any{"base_urls": []string{upstream.URL + "/new-source"}}}, 200, ifMatchHeader(saved.Revision))
	changed := h.adminApp(app.Key)
	if changed.SourceEpoch != app.SourceEpoch+1 {
		t.Fatal("fixture did not create a historical source")
	}
	body, _ = h.request("GET", api+"/cache?source_epoch=1", nil, 200, nil)
	if len(directoryDecode[[]httpcache.Row](t, body, "items")) != 2 {
		t.Fatal("explicit historic epoch lost its retained cache")
	}
	body, _ = h.request("POST", api+"/cache/cleanup/preview?source_epoch=1", map[string]any{"match": match, "basis": "fetched_at", "before": before}, 200, nil)
	historical := directoryDecode[httpcache.MaintenancePreview](t, body, "job")
	if historical.SelectedFiles != 1 {
		t.Fatal("historical preview used the new source", historical)
	}
	h.request("POST", api+"/cache/cleanup/"+historical.ID+"/execute?source_epoch=1", map[string]any{}, 200, nil)
}

func TestHTTPCachePolicyAPIChangesLiveTTLAndStaleFallback(t *testing.T) {
	var requests atomic.Int32
	var fail atomic.Bool
	payload := "a rule can retain this explicitly selected private response"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		fmt.Fprint(w, payload)
	}))
	defer upstream.Close()
	h, app, api := policyHTTPApp(t, upstream.URL)
	public := "/" + app.Key + "/tool.bin"
	h.request("GET", public, nil, 200, nil)
	h.request("GET", public, nil, 200, nil)
	body, _ := h.request("GET", api+"/cache", nil, 200, nil)
	if requests.Load() != 2 || len(directoryDecode[[]httpcache.Row](t, body, "items")) != 0 {
		t.Fatal("unmatched private/no-store source was shared")
	}
	saved := savePolicyAPI(t, h, api+"/cache/policy", app.Revision, map[string]any{"rules": []any{map[string]any{"match": pathmatch.Spec{Type: "glob", Pattern: "/"}, "ttl_seconds": 0}}, "auto_cleanup": []any{}, "stale_fallback": true})
	h.request("GET", public, nil, 200, nil)
	h.request("GET", public, nil, 200, nil)
	body, _ = h.request("GET", api+"/cache", nil, 200, nil)
	if requests.Load() != 4 || len(directoryDecode[[]httpcache.Row](t, body, "items")) != 1 {
		t.Fatal("explicit TTL 0 failed to retain body or revalidate each request")
	}
	fail.Store(true)
	body, _ = h.request("GET", public, nil, 200, nil)
	if string(body) != payload {
		t.Fatalf("enabled stale fallback lost retained body: %q", body)
	}
	savePolicyAPI(t, h, api+"/cache/policy", saved.Revision, map[string]any{"rules": saved.Rules, "auto_cleanup": saved.AutoCleanup, "stale_fallback": false})
	h.request("GET", public, nil, 502, nil)
}

func TestHTTPCacheRefreshAPIPaginatesAndExecutesCompleteFrozenSet(t *testing.T) {
	const selected = 103 // Selection is not truncated to a display page or 100 files.
	var requests atomic.Int32
	var block atomic.Bool
	started, release := make(chan struct{}), make(chan struct{})
	var first, released sync.Once
	unblock := func() { released.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if block.Load() {
			first.Do(func() { close(started) })
			<-release
		}
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Header().Set("ETag", `"unchanged"`)
		if r.Header.Get("If-None-Match") == `"unchanged"` {
			w.WriteHeader(304)
			return
		}
		fmt.Fprint(w, r.URL.Path)
	}))
	defer func() {
		unblock()
		upstream.Close()
	}()
	h, app, api := policyHTTPApp(t, upstream.URL)
	for i := range selected {
		h.request("GET", fmt.Sprintf("/%s/selected/file-%03d.bin", app.Key, i), nil, 200, nil)
	}
	h.request("GET", "/"+app.Key+"/untouched.bin", nil, 200, nil)
	baseline := requests.Load()
	previewInput := map[string]any{"match": pathmatch.Spec{Type: "glob", Pattern: "/selected/"}}
	h.request("POST", api+"/cache/refresh/preview", previewInput, 403, map[string]string{"X-CSRF-Token": ""})
	h.request("POST", api+"/cache/refresh/preview", map[string]any{"match": pathmatch.Spec{Type: "re2", Pattern: "["}}, 400, nil)
	h.request("POST", api+"/cache/refresh", map[string]any{"path": "/uncached.bin"}, 409, nil)
	body, _ := h.request("POST", api+"/cache/refresh/preview", map[string]any{"match": pathmatch.Spec{Type: "glob", Pattern: "/not-cached/**"}}, 200, nil)
	if job := directoryDecode[httpcache.MaintenancePreview](t, body, "job"); job.SelectedFiles != 0 || requests.Load() != baseline {
		t.Fatal("preview/unknown resource crawled or downloaded an origin", job)
	}
	body, _ = h.request("POST", api+"/cache/refresh/preview", previewInput, 200, nil)
	job := directoryDecode[httpcache.MaintenancePreview](t, body, "job")
	if job.SelectedFiles != selected || job.State != "ready" {
		t.Fatal("refresh preview truncated its selection", job)
	}
	jobAPI := api + "/cache/refresh/" + job.ID
	body, _ = h.request("GET", jobAPI+"/items?limit=2", nil, 200, nil)
	var page httpcache.PreviewPage
	if err := json.Unmarshal(body, &page); err != nil || len(page.Items) != 2 || page.NextCursor == "" || page.TotalFiles != selected {
		t.Fatalf("preview pagination missing: %s %v", body, err)
	}
	body, _ = h.request("GET", jobAPI+"/items?limit=2&cursor="+page.NextCursor, nil, 200, nil)
	var next httpcache.PreviewPage
	if err := json.Unmarshal(body, &next); err != nil || len(next.Items) != 2 || next.Items[0].GenerationID == page.Items[0].GenerationID {
		t.Fatalf("preview pagination repeated its first page: %s %v", body, err)
	}
	h.request("GET", jobAPI+"/items?limit=101", nil, 400, nil)
	h.request("POST", jobAPI+"/execute", map[string]any{}, 403, map[string]string{"X-CSRF-Token": ""})
	h.request("POST", jobAPI+"/execute?source_epoch=1", map[string]any{}, 400, nil)
	// A file fetched after preview is outside the frozen generation set.
	h.request("GET", "/"+app.Key+"/selected/later.bin", nil, 200, nil)
	baseline = requests.Load()
	block.Store(true)
	type startResult struct {
		code int
		body []byte
		err  error
	}
	response := make(chan startResult, 1)
	go func() {
		r, err := http.NewRequest("POST", h.http.URL+jobAPI+"/execute", strings.NewReader("{}"))
		if err != nil {
			response <- startResult{err: err}
			return
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", h.csrf)
		res, err := h.client.Do(r)
		if err != nil {
			response <- startResult{err: err}
			return
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		response <- startResult{code: res.StatusCode, body: data, err: err}
	}()
	select {
	case result := <-response:
		if result.err != nil || result.code != 200 {
			t.Fatalf("refresh start failed: %d %s %v", result.code, result.body, result.err)
		}
		if running := directoryDecode[httpcache.MaintenancePreview](t, result.body, "job"); running.State != "running" {
			t.Fatal("execute did not return a running job", running)
		}
	case <-time.After(3 * time.Second):
		unblock()
		t.Fatal("execute waited for upstream instead of returning a background job")
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		unblock()
		t.Fatal("refresh worker never started")
	}
	h.request("POST", jobAPI+"/execute", map[string]any{}, 409, nil)
	unblock()
	deadline := time.Now().Add(10 * time.Second)
	for {
		body, _ = h.request("GET", jobAPI, nil, 200, nil)
		finished := directoryDecode[httpcache.MaintenancePreview](t, body, "job")
		if finished.State == "done" {
			if finished.CompletedFiles != selected || finished.FailedFiles != 0 {
				t.Fatal("refresh processed only a displayed page", finished)
			}
			break
		}
		if finished.State == "failed" || time.Now().After(deadline) {
			t.Fatal("refresh job failed or failed to finish", finished)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if requests.Load()-baseline != selected {
		t.Fatal("refresh expanded its selection or skipped undisplayed files", requests.Load()-baseline)
	}
	body, _ = h.request("GET", jobAPI+"/items?limit=2", nil, 200, nil)
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.ResultStatus != "not_modified" {
			t.Fatal("refresh item outcome missing", item)
		}
	}
	h.request("POST", jobAPI+"/execute", map[string]any{}, 200, nil)
	if requests.Load()-baseline != selected {
		t.Fatal("completed execute receipt repeated upstream work")
	}
	// Cleanup uses the same frozen, paginated collection, and executes beyond
	// the displayed page without requesting any upstream files.
	if _, err := h.server.store.DB.Exec(`UPDATE http_cache_generations SET fetched_at_s=? WHERE storage_id=?`, time.Now().Add(-2*time.Hour).Unix(), app.StorageID()); err != nil {
		t.Fatal(err)
	}
	body, _ = h.request("POST", api+"/cache/cleanup/preview", map[string]any{"match": pathmatch.Spec{Type: "glob", Pattern: "/selected/"}, "basis": "fetched_at", "before": time.Now().Add(-time.Hour).UTC()}, 200, nil)
	cleanup := directoryDecode[httpcache.MaintenancePreview](t, body, "job")
	if cleanup.SelectedFiles != selected+1 {
		t.Fatal("cleanup preview truncated its full selection", cleanup)
	}
	cleanupAPI := api + "/cache/cleanup/" + cleanup.ID
	body, _ = h.request("GET", cleanupAPI+"/items?limit=2", nil, 200, nil)
	if err := json.Unmarshal(body, &page); err != nil || len(page.Items) != 2 || page.TotalFiles != selected+1 || page.NextCursor == "" {
		t.Fatalf("cleanup page lost total/cursor: %s %v", body, err)
	}
	body, _ = h.request("POST", cleanupAPI+"/execute", map[string]any{}, 200, nil)
	retired := directoryDecode[httpcache.CleanupResult](t, body, "result")
	if retired.RetiredFiles != selected+1 || requests.Load()-baseline != selected {
		t.Fatal("cleanup truncated its execution or fetched upstream files", retired)
	}
	body, _ = h.request("GET", api+"/cache", nil, 200, nil)
	remaining := directoryDecode[[]httpcache.Row](t, body, "items")
	if len(remaining) != 1 || remaining[0].Path != "untouched.bin" {
		t.Fatal("cleanup changed files outside the frozen matcher", remaining)
	}
}

func TestHTTPCacheRefreshAPISingleOutcomesAndPreviewFences(t *testing.T) {
	var version atomic.Int32
	version.Store(1)
	var fail atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Cache-Control", "max-age=3600")
		tag := fmt.Sprintf(`"v%d"`, version.Load())
		w.Header().Set("ETag", tag)
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(304)
			return
		}
		fmt.Fprint(w, tag)
	}))
	defer upstream.Close()
	h, app, api := policyHTTPApp(t, upstream.URL)
	h.request("GET", "/"+app.Key+"/file.bin", nil, 200, nil)
	accessBefore := time.Now().Add(-48*time.Hour).Unix() / 60 * 60
	if _, err := h.server.store.DB.Exec(`UPDATE http_cache_generations SET last_access_bucket_s=? WHERE storage_id=?`, accessBefore, app.StorageID()); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"path": "/file.bin"}
	h.request("POST", api+"/cache/refresh", input, 403, map[string]string{"Origin": "https://foreign.example"})
	h.request("POST", api+"/cache/refresh?source_epoch=1", input, 400, nil)
	h.request("POST", api+"/cache/refresh", map[string]any{"path": "/file%252ebin"}, 400, nil)
	version.Store(2)
	body, _ := h.request("POST", api+"/cache/refresh", input, 200, nil)
	refreshed := directoryDecode[httpcache.RefreshItem](t, body, "item")
	if refreshed.Status != "refreshed" || refreshed.GenerationID == "" {
		t.Fatal("single refresh did not replace a fresh cached body", refreshed)
	}
	body, _ = h.request("POST", api+"/cache/refresh", input, 200, nil)
	unchanged := directoryDecode[httpcache.RefreshItem](t, body, "item")
	if unchanged.Status != "not_modified" || unchanged.GenerationID != refreshed.GenerationID {
		t.Fatal("conditional refresh response lost its unchanged outcome", unchanged)
	}
	var accessAfter int64
	if err := h.server.store.DB.QueryRow(`SELECT last_access_bucket_s FROM http_cache_generations WHERE storage_id=? AND is_current=1`, app.StorageID()).Scan(&accessAfter); err != nil || accessAfter > accessBefore {
		t.Fatalf("admin refresh fabricated client access: %d %v", accessAfter, err)
	}
	fail.Store(true)
	body, _ = h.request("POST", api+"/cache/refresh", input, 200, nil)
	stale := directoryDecode[httpcache.RefreshItem](t, body, "item")
	if stale.Status != "stale_fallback" || stale.GenerationID != refreshed.GenerationID {
		t.Fatal("refresh hid its stale fallback outcome", stale)
	}
	fail.Store(false)
	match := map[string]any{"match": pathmatch.Spec{Type: "glob", Pattern: "/"}}
	body, _ = h.request("POST", api+"/cache/refresh/preview", match, 200, nil)
	beforePolicy := directoryDecode[httpcache.MaintenancePreview](t, body, "job")
	saved := savePolicyAPI(t, h, api+"/cache/policy", app.Revision, map[string]any{"rules": []any{}, "auto_cleanup": []any{}, "stale_fallback": false})
	h.request("POST", api+"/cache/refresh/"+beforePolicy.ID+"/execute", map[string]any{}, 409, nil)
	fail.Store(true)
	body, _ = h.request("POST", api+"/cache/refresh", input, 200, nil)
	failed := directoryDecode[httpcache.RefreshItem](t, body, "item")
	if failed.Status != "failed" || failed.Reason != "refresh_failed" {
		t.Fatal("single upstream failure was reported as a successful refresh", failed)
	}
	fail.Store(false)
	body, _ = h.request("POST", api+"/cache/refresh/preview", match, 200, nil)
	beforeSource := directoryDecode[httpcache.MaintenancePreview](t, body, "job")
	h.request("PATCH", api+"/configuration", map[string]any{"set": map[string]any{"base_urls": []string{upstream.URL + "/replacement"}}}, 200, ifMatchHeader(saved.Revision))
	changed := h.adminApp(app.Key)
	h.request("POST", api+"/cache/refresh/"+beforeSource.ID+"/execute", map[string]any{}, 409, nil)
	h.request("POST", api+"/cache/refresh/"+beforeSource.ID+"/execute?source_epoch=1", map[string]any{}, 400, nil)
	h.request("PATCH", api, map[string]any{"enabled": false}, 200, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, changed.Revision)})
	h.request("POST", api+"/cache/refresh", input, 409, nil)
}
