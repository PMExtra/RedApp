package httpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
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
	h := newHarness(t)
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
		global, err := h.server.store.Counters()
		if err != nil {
			t.Fatal(err)
		}
		owned, err := h.server.store.CountersFor(app.MetricsID())
		if err != nil {
			t.Fatal(err)
		}
		if global["requests"] != requests || global["artifact_requests"] != requests || owned["artifact_requests"] != requests || owned["cache_hit_requests"] != hits || owned["miss_requests"] != 1 || owned["upstream_bytes"] != 10 || owned["downstream_bytes"] != downstream {
			t.Fatal("GeneralHttp request/traffic counters are missing, duplicated, or scoped incorrectly", global, owned)
		}
		for _, scope := range []string{app.Key, app.StorageID()} {
			wrong, err := h.server.store.CountersFor(scope)
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
		metrics := directoryDecode[[]metricDTO](t, body, "metrics")
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
