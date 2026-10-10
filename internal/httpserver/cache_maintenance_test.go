package httpserver

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/pathmatch"
)

// cacheFixture serves every path with its own path as body; requests counts
// upstream requests.
type cacheFixture struct {
	server   *httptest.Server
	requests atomic.Int32
}

func newCacheFixture(t *testing.T) *cacheFixture {
	t.Helper()
	f := &cacheFixture{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		w.Header().Set("Cache-Control", "max-age=3600")
		fmt.Fprint(w, r.URL.Path)
	}))
	t.Cleanup(f.server.Close)
	return f
}

// ageCache moves the fetch time of every cached file of storageID back so a
// time-based cleanup selects it without sleeping.
func ageCache(t *testing.T, h *harness, storageID string, age time.Duration) {
	t.Helper()
	if _, err := h.store.DB.Exec(`UPDATE http_cache_generations SET fetched_at_s=? WHERE storage_id=?`, time.Now().Add(-age).Unix(), storageID); err != nil {
		t.Fatal(err)
	}
}

func decodePreview(t *testing.T, body []byte) maintenancePreviewDTO {
	t.Helper()
	return decodeJSONBody[maintenancePreviewDTO](t, body)
}

// cleanupResult returns the cleanup receipt of an executed preview.
func cleanupResult(t *testing.T, body []byte) cleanupResultDTO {
	t.Helper()
	preview := decodeJSONBody[struct {
		State  string           `json:"state"`
		Result cleanupResultDTO `json:"result"`
	}](t, body)
	if preview.State != "done" || preview.Result.Kind != "cleanup" {
		t.Fatalf("cleanup was not executed: %s", body)
	}
	return preview.Result
}

func TestCacheCleanupFreezesSelectionPerSourceEpoch(t *testing.T) {
	upstream := newCacheFixture(t)
	h, app, api := policyHTTPApp(t, upstream.server.URL)
	fetch := func(path string) { t.Helper(); h.request("GET", "/"+app.Key+path, nil, 200, nil) }
	fetch("/remove/first.bin")
	fetch("/keep.bin")
	ageCache(t, h, app.StorageID(), 2*time.Hour)
	before := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	match := pathmatch.Spec{Type: "glob", Pattern: "/remove/"}
	h.expectError("POST", api+"/cache/cleanup/preview", map[string]any{"match": match, "basis": "fetched_at", "before": before}, 403, codeCSRFRejected, map[string]string{"X-CSRF-Token": ""})
	body, _ := h.request("POST", api+"/cache/cleanup/preview", map[string]any{"match": match, "basis": "fetched_at", "before": before}, 201, nil)
	job := decodePreview(t, body)
	if job.Kind != "cleanup" || job.State != "ready" || job.Match != match || job.SelectedFiles != 1 || job.SourceEpoch != 1 || *job.Basis != "fetched_at" || !job.Before.Equal(before) || job.Result != nil {
		t.Fatal("cleanup preview lost match, selection or criteria", string(body))
	}
	body, _ = h.request("GET", api+"/cache/cleanup/"+job.ID, nil, 200, nil)
	if reloaded := decodePreview(t, body); reloaded.Match != match || reloaded.SelectedFiles != 1 || reloaded.State != "ready" {
		t.Fatal("reloading the preview lost its frozen matcher or selection", string(body))
	}
	body, _ = h.request("GET", api+"/cache/cleanup/"+job.ID+"/items", nil, 200, nil)
	items := decodeJSONBody[maintenanceItemPageDTO](t, body)
	if len(items.Items) != 1 || items.Items[0].Path != "/remove/first.bin" || items.Items[0].ResultStatus != "pending" || items.NextCursor != nil || items.TotalFiles != 1 {
		t.Fatal("cleanup items", string(body))
	}
	// A file cached after the preview is outside its frozen selection.
	fetch("/remove/later.bin")
	ageCache(t, h, app.StorageID(), 2*time.Hour)
	body, _ = h.request("POST", api+"/cache/cleanup/"+job.ID+"/execute", nil, 200, nil)
	if result := cleanupResult(t, body); result.SelectedFiles != 1 || result.RetiredFiles != 1 || result.RetiredBytes != int64(len("/remove/first.bin")) {
		t.Fatal("execution expanded the frozen preview", string(body))
	}
	if entries := cacheEntries(t, h, api, ""); len(entries) != 2 {
		t.Fatal("unselected or later cached file was removed", entries)
	}
	// Executing again returns the receipt.
	again, _ := h.request("POST", api+"/cache/cleanup/"+job.ID+"/execute", nil, 200, nil)
	if result := cleanupResult(t, again); result.RetiredFiles != 1 {
		t.Fatal("receipt changed", string(again))
	}
	body, _ = h.request("GET", api+"/cache/cleanup/"+job.ID+"/items", nil, 200, nil)
	if items := decodeJSONBody[maintenanceItemPageDTO](t, body); items.State != "done" || items.Items[0].ResultStatus != "retired" {
		t.Fatal("per-file result missing", string(body))
	}
	// Without match the whole epoch is selected.
	body, _ = h.request("POST", api+"/cache/cleanup/preview", map[string]any{"basis": "fetched_at", "before": before}, 201, nil)
	all := decodePreview(t, body)
	if all.Match != (pathmatch.Spec{Type: "glob", Pattern: "/"}) || all.SelectedFiles != 2 {
		t.Fatal("cleanup without match did not default to all paths", string(body))
	}
	// A policy change since the preview makes it stale; nothing is removed.
	setCachePolicy(t, h, app.Key, cachepolicy.Config{Rules: []cachepolicy.CacheRule{}, AutoCleanup: []cachepolicy.CleanupRule{}, StaleFallback: false})
	h.expectError("POST", api+"/cache/cleanup/"+all.ID+"/execute", nil, 409, codePreviewStale, nil)
	if entries := cacheEntries(t, h, api, ""); len(entries) != 2 {
		t.Fatal("stale preview removed files", entries)
	}
	// A historical epoch is cleaned by building the preview for it; the preview
	// ID alone selects that epoch afterwards.
	h.patchApp(app.Key, map[string]any{"base_urls": []string{upstream.server.URL + "/new-source"}})
	fetch("/remove/current.bin")
	body, _ = h.request("POST", api+"/cache/cleanup/preview", map[string]any{"match": match, "basis": "fetched_at", "before": before, "source_epoch": 1}, 201, nil)
	historical := decodePreview(t, body)
	if historical.SourceEpoch != 1 || historical.SelectedFiles != 1 {
		t.Fatal("historical preview used the current source", string(body))
	}
	body, _ = h.request("GET", api+"/cache/cleanup/"+historical.ID, nil, 200, nil)
	if decodePreview(t, body).SourceEpoch != 1 {
		t.Fatal("preview lost its epoch", string(body))
	}
	body, _ = h.request("POST", api+"/cache/cleanup/"+historical.ID+"/execute", nil, 200, nil)
	if result := cleanupResult(t, body); result.RetiredFiles != 1 {
		t.Fatal("historical cleanup", string(body))
	}
	if old, current := cacheEntries(t, h, api, "?source_epoch=1"), cacheEntries(t, h, api, ""); len(old) != 1 || len(current) != 1 || current[0].Path != "/remove/current.bin" {
		t.Fatal("cleanup crossed source epochs", old, current)
	}
	// The removed source_epoch follow-up parameter is rejected.
	h.expectError("POST", api+"/cache/cleanup/"+historical.ID+"/execute?source_epoch=1", nil, 400, codeInvalidQuery, nil)
	// Disabled applications keep cache management.
	h.setAppEnabled(app.Key, false)
	h.request("GET", "/"+app.Key+"/remove/current.bin", nil, 404, nil)
	h.request("POST", api+"/cache/cleanup/preview", map[string]any{"basis": "fetched_at", "before": time.Now().UTC()}, 201, nil)
}

func TestCachePreviewErrors(t *testing.T) {
	upstream := newCacheFixture(t)
	h, app, api := policyHTTPApp(t, upstream.server.URL)
	h.request("GET", "/"+app.Key+"/file.bin", nil, 200, nil)
	other := h.createApp("policy-test", "other", application.HttpCache, map[string]any{"base_url": upstream.server.URL})
	otherAPI := "/admin/api/apps/" + other.Key
	glob := pathmatch.Spec{Type: "glob", Pattern: "/"}
	now := time.Now().UTC()
	// Invalid selections.
	for _, body := range []map[string]any{
		{"basis": "created", "before": now},
		{"basis": "fetched_at", "before": now.Add(time.Hour)},
		{"basis": "fetched_at", "before": now, "match": pathmatch.Spec{Type: "re2", Pattern: "["}},
		{"basis": "fetched_at", "before": now, "source_epoch": 0},
	} {
		h.expectError("POST", api+"/cache/cleanup/preview", body, 400, codeValidationFailed, nil)
	}
	for _, body := range []map[string]any{{"basis": "fetched_at"}, {"before": now}, {"basis": "fetched_at", "before": "yesterday"}, {"basis": "fetched_at", "before": now, "unknown": 1}} {
		h.expectError("POST", api+"/cache/cleanup/preview", body, 400, codeInvalidRequest, nil)
	}
	h.expectError("POST", api+"/cache/cleanup/preview", map[string]any{"basis": "fetched_at", "before": now, "source_epoch": 9}, 404, codeSourceNotFound, nil)
	h.expectError("POST", api+"/cache/refresh/preview", map[string]any{"match": pathmatch.Spec{Type: "re2", Pattern: "["}}, 400, codeValidationFailed, nil)
	h.expectError("POST", api+"/cache/refresh/preview", map[string]any{}, 400, codeInvalidRequest, nil)
	h.expectError("POST", "/admin/api/apps/openai/codex/cache/refresh/preview", map[string]any{"match": glob}, 404, codeCapabilityUnsupported, nil)
	// Unknown, malformed, foreign and other-kind previews.
	body, _ := h.request("POST", api+"/cache/refresh/preview", map[string]any{"match": glob}, 201, nil)
	refresh := decodePreview(t, body)
	unknown := strings.Repeat("0", 32)
	for _, path := range []string{api + "/cache/refresh/" + unknown, api + "/cache/cleanup/" + refresh.ID, otherAPI + "/cache/refresh/" + refresh.ID, api + "/cache/refresh/" + unknown + "/items"} {
		h.expectError("GET", path, nil, 404, codePreviewNotFound, nil)
	}
	h.expectError("POST", otherAPI+"/cache/refresh/"+refresh.ID+"/execute", nil, 404, codePreviewNotFound, nil)
	h.expectError("GET", api+"/cache/refresh/not-an-id", nil, 400, codeInvalidPath, nil)
	// Cursors are bound to their preview.
	body, _ = h.request("POST", api+"/cache/cleanup/preview", map[string]any{"basis": "fetched_at", "before": now}, 201, nil)
	cleanup := decodePreview(t, body)
	h.request("GET", "/"+app.Key+"/second.bin", nil, 200, nil)
	body, _ = h.request("POST", api+"/cache/refresh/preview", map[string]any{"match": glob}, 201, nil)
	twoFiles := decodePreview(t, body)
	body, _ = h.request("GET", api+"/cache/refresh/"+twoFiles.ID+"/items?limit=1", nil, 200, nil)
	page := decodeJSONBody[maintenanceItemPageDTO](t, body)
	if page.NextCursor == nil || len(page.Items) != 1 || page.TotalFiles != 2 {
		t.Fatal("refresh items page", string(body))
	}
	h.request("GET", api+"/cache/refresh/"+twoFiles.ID+"/items?cursor="+*page.NextCursor, nil, 200, nil)
	for _, path := range []string{api + "/cache/refresh/" + refresh.ID + "/items?cursor=" + *page.NextCursor, api + "/cache/cleanup/" + cleanup.ID + "/items?cursor=" + *page.NextCursor, api + "/cache/refresh/" + twoFiles.ID + "/items?cursor=e30"} {
		h.expectError("GET", path, nil, 400, codeInvalidCursor, nil)
	}
	h.expectError("GET", api+"/cache/refresh/"+twoFiles.ID+"/items?limit=101", nil, 400, codeInvalidQuery, nil)
	// Expired previews are gone.
	if _, err := h.store.DB.Exec(`UPDATE http_cleanup_previews SET expires_at_s=? WHERE id=?`, time.Now().Add(-time.Second).Unix(), cleanup.ID); err != nil {
		t.Fatal(err)
	}
	h.expectError("GET", api+"/cache/cleanup/"+cleanup.ID, nil, 404, codePreviewNotFound, nil)
	h.expectError("POST", api+"/cache/cleanup/"+cleanup.ID+"/execute", nil, 404, codePreviewNotFound, nil)
	// A disabled application has no active source to refresh.
	h.setAppEnabled(app.Key, false)
	h.expectError("POST", api+"/cache/refresh/preview", map[string]any{"match": glob}, 409, codeSourceChanged, nil)
	h.expectError("POST", api+"/cache/refresh", map[string]any{"path": "/file.bin"}, 409, codeSourceChanged, nil)
	h.expectError("POST", api+"/cache/refresh/"+twoFiles.ID+"/execute", nil, 409, codePreviewStale, nil)
	// Deleted applications are read-only but still listed.
	row, _ := h.store.Application(app.Key)
	if err := h.store.DeleteApplication(app.Key, row.Revision); err != nil {
		t.Fatal(err)
	}
	h.expectError("POST", api+"/cache/cleanup/preview", map[string]any{"basis": "fetched_at", "before": now}, 409, codeEntityDeleted, nil)
	h.expectError("POST", api+"/cache/refresh/preview", map[string]any{"match": glob}, 409, codeEntityDeleted, nil)
	h.expectError("POST", api+"/cache/refresh", map[string]any{"path": "/file.bin"}, 409, codeEntityDeleted, nil)
	h.request("GET", api+"/cache/refresh/"+twoFiles.ID, nil, 200, nil)
}

func TestCacheRefreshPreviewExecutesCompleteFrozenSetInBackground(t *testing.T) {
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
	h.expectError("POST", api+"/cache/refresh/preview", previewInput, 403, codeCSRFRejected, map[string]string{"X-CSRF-Token": ""})
	body, _ := h.request("POST", api+"/cache/refresh/preview", map[string]any{"match": pathmatch.Spec{Type: "glob", Pattern: "/not-cached/**"}}, 201, nil)
	if job := decodePreview(t, body); job.SelectedFiles != 0 || requests.Load() != baseline {
		t.Fatal("preview of unknown files crawled or downloaded an origin", string(body))
	}
	body, _ = h.request("POST", api+"/cache/refresh/preview", previewInput, 201, nil)
	job := decodePreview(t, body)
	if job.SelectedFiles != selected || job.State != "ready" || job.Kind != "refresh" || job.Basis != nil || job.Before != nil {
		t.Fatal("refresh preview truncated its selection", string(body))
	}
	jobAPI := api + "/cache/refresh/" + job.ID
	body, _ = h.request("GET", jobAPI+"/items?limit=2", nil, 200, nil)
	page := decodeJSONBody[maintenanceItemPageDTO](t, body)
	if len(page.Items) != 2 || page.NextCursor == nil || page.TotalFiles != selected || page.Items[0].Path != "/selected/file-000.bin" {
		t.Fatalf("preview pagination missing: %s", body)
	}
	body, _ = h.request("GET", jobAPI+"/items?limit=2&cursor="+*page.NextCursor, nil, 200, nil)
	if next := decodeJSONBody[maintenanceItemPageDTO](t, body); len(next.Items) != 2 || next.Items[0].Ordinal <= page.Items[1].Ordinal {
		t.Fatalf("preview pagination repeated its first page: %s", body)
	}
	h.expectError("POST", jobAPI+"/execute", nil, 403, codeCSRFRejected, map[string]string{"X-CSRF-Token": ""})
	// A file fetched after the preview is outside the frozen generation set.
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
		r, err := http.NewRequest("POST", h.http.URL+jobAPI+"/execute", nil)
		if err != nil {
			response <- startResult{err: err}
			return
		}
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
		if result.err != nil || result.code != 202 {
			t.Fatalf("refresh start failed: %d %s %v", result.code, result.body, result.err)
		}
		if running := decodePreview(t, result.body); running.State != "running" {
			t.Fatal("execute did not return a running job", string(result.body))
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
	h.expectError("POST", jobAPI+"/execute", nil, 409, codeOperationInProgress, nil)
	unblock()
	deadline := time.Now().Add(10 * time.Second)
	for {
		body, _ = h.request("GET", jobAPI, nil, 200, nil)
		finished := decodePreview(t, body)
		if finished.State == "done" {
			summary, ok := finished.Result.(map[string]any)
			if finished.CompletedFiles != selected || finished.FailedFiles != 0 || !ok || summary["kind"] != "refresh" || summary["not_modified"] != float64(selected) {
				t.Fatal("refresh processed only a displayed page", string(body))
			}
			break
		}
		if finished.State == "failed" || time.Now().After(deadline) {
			t.Fatal("refresh job failed or failed to finish", string(body))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if requests.Load()-baseline != selected {
		t.Fatal("refresh expanded its selection or skipped undisplayed files", requests.Load()-baseline)
	}
	body, _ = h.request("GET", jobAPI+"/items?limit=2", nil, 200, nil)
	for _, item := range decodeJSONBody[maintenanceItemPageDTO](t, body).Items {
		if item.ResultStatus != "not_modified" {
			t.Fatal("refresh item outcome missing", item)
		}
	}
	body, _ = h.request("POST", jobAPI+"/execute", nil, 202, nil)
	if decodePreview(t, body).State != "done" || requests.Load()-baseline != selected {
		t.Fatal("executing a finished refresh repeated upstream work", string(body))
	}
	// Cleanup uses the same frozen, paginated collection and executes beyond
	// the displayed page without requesting any upstream files.
	ageCache(t, h, app.StorageID(), 2*time.Hour)
	body, _ = h.request("POST", api+"/cache/cleanup/preview", map[string]any{"match": pathmatch.Spec{Type: "glob", Pattern: "/selected/"}, "basis": "fetched_at", "before": time.Now().Add(-time.Hour).UTC()}, 201, nil)
	cleanup := decodePreview(t, body)
	if cleanup.SelectedFiles != selected+1 {
		t.Fatal("cleanup preview truncated its full selection", string(body))
	}
	body, _ = h.request("POST", api+"/cache/cleanup/"+cleanup.ID+"/execute", nil, 200, nil)
	if retired := cleanupResult(t, body); retired.RetiredFiles != selected+1 || requests.Load()-baseline != selected {
		t.Fatal("cleanup truncated its execution or fetched upstream files", string(body))
	}
	if remaining := cacheEntries(t, h, api, ""); len(remaining) != 1 || remaining[0].Path != "/untouched.bin" {
		t.Fatal("cleanup changed files outside the frozen matcher", remaining)
	}
}

func TestCacheRefreshSingleOutcomesAndPreviewFences(t *testing.T) {
	var version atomic.Int32
	version.Store(1)
	var fail, private atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Cache-Control", "max-age=3600")
		if private.Load() {
			w.Header().Set("Cache-Control", "no-store")
		}
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
	if _, err := h.store.DB.Exec(`UPDATE http_cache_generations SET last_access_bucket_s=? WHERE storage_id=?`, accessBefore, app.StorageID()); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"path": "/file.bin"}
	refresh := func() cacheRefreshItemDTO {
		t.Helper()
		body, _ := h.request("POST", api+"/cache/refresh", input, 200, nil)
		return decodeJSONBody[cacheRefreshItemDTO](t, body)
	}
	h.expectError("POST", api+"/cache/refresh", input, 403, codeOriginRejected, map[string]string{"Origin": "https://foreign.example"})
	h.expectError("POST", api+"/cache/refresh?source_epoch=1", input, 400, codeInvalidQuery, nil)
	for _, path := range []string{"/file%252ebin", "/", "relative", "/a/../b"} {
		h.expectError("POST", api+"/cache/refresh", map[string]any{"path": path}, 400, codeValidationFailed, nil)
	}
	h.expectError("POST", api+"/cache/refresh", map[string]any{"path": "/uncached.bin"}, 404, codeCacheEntryNotFound, nil)
	version.Store(2)
	refreshed := refresh()
	if refreshed.Status != "refreshed" || refreshed.GenerationID == nil || refreshed.Reason != nil || refreshed.Path != "/file.bin" {
		t.Fatal("single refresh did not replace a fresh cached body", refreshed)
	}
	if unchanged := refresh(); unchanged.Status != "not_modified" || *unchanged.GenerationID != *refreshed.GenerationID {
		t.Fatal("conditional refresh lost its unchanged outcome", unchanged)
	}
	var accessAfter int64
	if err := h.store.DB.QueryRow(`SELECT last_access_bucket_s FROM http_cache_generations WHERE storage_id=? AND is_current=1`, app.StorageID()).Scan(&accessAfter); err != nil || accessAfter > accessBefore {
		t.Fatalf("admin refresh fabricated client access: %d %v", accessAfter, err)
	}
	fail.Store(true)
	if stale := refresh(); stale.Status != "stale_fallback" || *stale.GenerationID != *refreshed.GenerationID {
		t.Fatal("refresh hid its stale fallback outcome", stale)
	}
	fail.Store(false)
	glob := map[string]any{"match": pathmatch.Spec{Type: "glob", Pattern: "/"}}
	body, _ := h.request("POST", api+"/cache/refresh/preview", glob, 201, nil)
	beforePolicy := decodePreview(t, body)
	setCachePolicy(t, h, app.Key, cachepolicy.Config{Rules: []cachepolicy.CacheRule{}, AutoCleanup: []cachepolicy.CleanupRule{}, StaleFallback: false})
	h.expectError("POST", api+"/cache/refresh/"+beforePolicy.ID+"/execute", nil, 409, codePreviewStale, nil)
	fail.Store(true)
	if failed := refresh(); failed.Status != "failed" || failed.Reason == nil || *failed.Reason != "refresh_failed" {
		t.Fatal("single upstream failure was reported as a successful refresh", failed)
	}
	fail.Store(false)
	private.Store(true)
	version.Store(3)
	if skipped := refresh(); skipped.Status != "skipped" || skipped.Reason == nil {
		t.Fatal("uncacheable refresh response", skipped)
	}
	private.Store(false)
	body, _ = h.request("POST", api+"/cache/refresh/preview", glob, 201, nil)
	beforeSource := decodePreview(t, body)
	h.patchApp(app.Key, map[string]any{"base_urls": []string{upstream.URL + "/replacement"}})
	h.expectError("POST", api+"/cache/refresh/"+beforeSource.ID+"/execute", nil, 409, codePreviewStale, nil)
	h.expectError("POST", api+"/cache/refresh", input, 404, codeCacheEntryNotFound, nil)
}
