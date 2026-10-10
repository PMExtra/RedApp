package httpserver

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/history"
	"github.com/PMExtra/RedApp/internal/httpcache"
	"github.com/PMExtra/RedApp/internal/store"
)

func TestGeneralHTTPRouteEncodingMethodsAndRanges(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/packages/资料/file name.bin" {
			t.Errorf("source received an incorrectly decoded/mapped path: %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Header().Set("ETag", `"source-v1"`)
		io.WriteString(w, "abcdefghij")
	}))
	defer upstream.Close()
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	h.createVendor("enterprise")
	app := h.createApp("enterprise", "files", application.HttpCache, map[string]any{"base_url": upstream.URL + "/packages"})
	path := "/enterprise/files/" + url.PathEscape("资料") + "/" + url.PathEscape("file name.bin")
	body, headers := h.request("GET", path, nil, 200, nil)
	if string(body) != "abcdefghij" || headers.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(headers.Get("Content-Disposition"), "attachment") || !strings.Contains(headers.Get("Content-Security-Policy"), "sandbox") {
		t.Fatal("public file route did not preserve attachment bytes and inert headers", string(body), headers)
	}
	assertCounters := func(requests, hits, downstream int64) {
		t.Helper()
		global, err := h.server.DB.Counters()
		if err != nil {
			t.Fatal(err)
		}
		owned, err := h.server.DB.CountersFor(app.MetricsID())
		if err != nil {
			t.Fatal(err)
		}
		if global["requests"] != requests || global["artifact_requests"] != requests || owned["artifact_requests"] != requests || owned["cache_hit_requests"] != hits || owned["miss_requests"] != 1 || owned["upstream_bytes"] != 10 || owned["downstream_bytes"] != downstream {
			t.Fatal("GeneralHttp request/traffic counters are missing, duplicated, or scoped incorrectly", global, owned)
		}
		for _, scope := range []string{app.Key, app.StorageID()} {
			wrong, err := h.server.DB.CountersFor(scope)
			if err != nil || wrong["artifact_requests"] != 0 {
				t.Fatal("GeneralHttp wrote counters outside the stable application scope", scope, wrong, err)
			}
		}
	}
	assertCounters(1, 0, 10)
	body, headers = h.request("HEAD", path, nil, 200, nil)
	if len(body) != 0 || headers.Get("Content-Length") != "10" {
		t.Fatal("public HEAD did not expose cached representation metadata", string(body), headers)
	}
	body, headers = h.request("GET", path, nil, 206, map[string]string{"Range": "bytes=2-4"})
	if string(body) != "cde" || headers.Get("Content-Range") != "bytes 2-4/10" {
		t.Fatal("public Range did not reach cached byte-range handling", string(body), headers)
	}
	if calls.Load() != 1 {
		t.Fatal("hot GET/HEAD/Range made additional source requests", calls.Load())
	}
	assertCounters(3, 2, 13)
	for _, endpoint := range []string{"/admin/api/status", "/admin/api/apps/" + app.Key + "/status"} {
		body, _ := h.request("GET", endpoint, nil, 200, nil)
		metrics := directoryDecode[[]history.Metric](t, body, "metrics")
		values := map[string]*float64{}
		for _, metric := range metrics {
			values[metric.Key] = metric.Value
		}
		for key, want := range map[string]float64{"counters.artifact_requests": 3, "resources.total": 1, "resources.complete": 1, "resources.current": 1} {
			if values[key] == nil || *values[key] != want {
				t.Fatalf("%s %s: missing/wrong GeneralHttp metric %v", endpoint, key, values[key])
			}
		}
		if endpoint != "/admin/api/status" && values["versions.total"] != nil {
			t.Fatal("GeneralHttp metrics fabricated a release-version count")
		}
	}
	for _, invalid := range []string{
		"a%2fb", "a%5cb", "%2e%2e/file", "a/%2e/file", "a%252fb", "a%252e%252e/file", "a%00b", "a//b", "file?v=1", "file?",
	} {
		h.request("GET", "/enterprise/files/"+invalid, nil, 400, nil)
	}
	h.request("POST", path, map[string]string{"url": upstream.URL}, 405, nil)
	if calls.Load() != 1 {
		t.Fatal("invalid public path/method reached the configured origin", calls.Load())
	}
	assertCounters(3, 2, 13)
}

func TestGeneralHTTPCacheAdminHistoricalSourceCleanup(t *testing.T) {
	newSource := func(body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/files/object.bin" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "max-age=3600")
			io.WriteString(w, body)
		}))
	}
	oldSource := newSource("old source file")
	defer oldSource.Close()
	newUpstream := newSource("new source file")
	defer newUpstream.Close()
	h := newDirectoryHarness(t, t.TempDir())
	h.request("GET", "/admin/api/apps/openai/codex/sources", nil, 401, nil)
	h.login(h.password)
	h.createVendor("enterprise")
	app := h.createApp("enterprise", "files", application.HttpCache, map[string]any{"base_url": oldSource.URL + "/files"})
	api := "/admin/api/apps/" + app.Key
	publicPath := "/" + app.Key + "/object.bin"
	body, _ := h.request("GET", publicPath, nil, 200, nil)
	if string(body) != "old source file" {
		t.Fatal("initial source file missing")
	}
	body, _ = h.request("PATCH", api, map[string]any{"revision": app.Revision, "base_url": newUpstream.URL + "/files"}, 200, nil)
	rebound := directoryDecode[store.Application](t, body, "app")
	body, _ = h.request("GET", publicPath, nil, 200, nil)
	if string(body) != "new source file" || rebound.SourceEpoch != 2 {
		t.Fatal("source edit served the old namespace")
	}
	body, _ = h.request("GET", api+"/sources", nil, 200, nil)
	sources := directoryDecode[[]struct {
		Epoch   int64 `json:"epoch"`
		Current bool  `json:"current"`
		Active  bool  `json:"active"`
	}](t, body, "sources")
	if len(sources) != 2 || sources[0].Epoch != 1 || sources[0].Current || sources[0].Active || sources[1].Epoch != 2 || !sources[1].Current || !sources[1].Active {
		t.Fatal("source history/current eligibility was lost", string(body))
	}
	list := func(query string) []httpcache.Row {
		t.Helper()
		body, _ := h.request("GET", api+"/cache"+query, nil, 200, nil)
		return directoryDecode[[]httpcache.Row](t, body, "items")
	}
	oldRows, currentRows := list("?source_epoch=1"), list("")
	if len(oldRows) != 1 || len(currentRows) != 1 || oldRows[0].GenerationID == currentRows[0].GenerationID || oldRows[0].SHA256 == currentRows[0].SHA256 {
		t.Fatal("cache listing merged historical and current source data", oldRows, currentRows)
	}
	for _, query := range []string{"?source_epoch=01", "?source_epoch=0", "?source_epoch=3", "?source_epoch=1&source_epoch=2", "?unexpected=1"} {
		status := 400
		if query == "?source_epoch=3" {
			status = 404
		}
		h.request("GET", api+"/cache"+query, nil, status, nil)
	}
	// Make the already fetched historical row old enough for the real time-based
	// preview without sleeping or changing the HTTP-cache service's clock.
	if _, err := h.server.DB.DB.Exec("UPDATE http_cache_generations SET fetched_at_s=? WHERE id=?", time.Now().Add(-time.Hour).Unix(), oldRows[0].GenerationID); err != nil {
		t.Fatal(err)
	}
	selection := map[string]any{"basis": "fetched_at", "before": time.Now().UTC().Add(-time.Minute)}
	h.request("POST", api+"/cache/cleanup/preview?source_epoch=1", selection, 403, map[string]string{"X-CSRF-Token": ""})
	body, _ = h.request("POST", api+"/cache/cleanup/preview?source_epoch=1", selection, 200, nil)
	preview := directoryDecode[httpcache.MaintenancePreview](t, body, "job")
	if preview.SelectedFiles != 1 || preview.SelectedBytes != int64(len("old source file")) {
		t.Fatal("historical cleanup did not snapshot only its selected source", preview)
	}
	execute := api + "/cache/cleanup/" + preview.ID + "/execute"
	h.request("POST", execute, map[string]any{}, 409, nil)
	other := h.createApp("enterprise", "other", application.HttpCache, map[string]any{"base_url": oldSource.URL + "/files"})
	h.request("POST", "/admin/api/apps/"+other.Key+"/cache/cleanup/"+preview.ID+"/execute?source_epoch=1", map[string]any{}, 409, nil)
	body, _ = h.request("POST", execute+"?source_epoch=1", map[string]any{}, 200, nil)
	result := directoryDecode[httpcache.CleanupResult](t, body, "result")
	if result.RetiredFiles != 1 || len(list("?source_epoch=1")) != 0 || len(list("")) != 1 {
		t.Fatal("cleanup removed data outside its historical source", result)
	}
	h.request("POST", execute+"?source_epoch=1", map[string]any{}, 200, nil)
	// Disabling the public app retains cache and source management routes.
	h.request("PATCH", api, map[string]any{"revision": rebound.Revision, "enabled": false}, 200, nil)
	h.request("GET", publicPath, nil, 404, nil)
	h.request("HEAD", publicPath, nil, 404, nil)
	if rows := list(""); len(rows) != 1 || rows[0].GenerationID != currentRows[0].GenerationID {
		t.Fatal("disabled app lost administrative cache visibility")
	}
	body, _ = h.request("GET", api+"/sources", nil, 200, nil)
	if strings.Contains(string(body), `"active":true`) {
		t.Fatal("disabled source remained eligible", string(body))
	}
	h.request("GET", fmt.Sprintf("%s/cache?source_epoch=%d", api, rebound.SourceEpoch), nil, 200, nil)
}
