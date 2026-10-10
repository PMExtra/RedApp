package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/store"
)

func TestPublicDirectoryPinsTemplatesAndInstructionDocuments(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	h.createVendor("acme")
	for i := 0; i < 7; i++ {
		h.createApp("acme", fmt.Sprintf("tool-%d", i), "info", map[string]any{"name": store.LocalizedText{En: fmt.Sprintf("Tool %d", i), ZhCN: fmt.Sprintf("工具%d", i)}})
	}
	for _, path := range []string{"/all?q=tool&page=2", "/acme?page=2", "/acme/tool-6"} {
		_, headers := h.request("GET", path, nil, 200, nil)
		if !strings.Contains(headers.Get("Content-Security-Policy"), "script-src 'self'") {
			t.Fatal("SPA CSP relaxed")
		}
	}
	data, _ := h.request("GET", "/api/catalog?vendor=acme&q=tool-6&limit=1", nil, 200, nil)
	var page store.Page[map[string]any]
	json.Unmarshal(data, &page)
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0]["key"] != "acme/tool-6" {
		t.Fatal("search after limit", string(data))
	}
	data, _ = h.request("GET", "/api/search?q=acme", nil, 200, nil)
	var suggestions struct {
		Items []map[string]any `json:"items"`
	}
	json.Unmarshal(data, &suggestions)
	if len(suggestions.Items) != 7 || suggestions.Items[0]["kind"] != "vendor" {
		t.Fatal("unbounded or untyped suggestions", string(data))
	}
	h.request("POST", "/admin/api/vendors", map[string]any{"id": "all", "name": store.LocalizedText{En: "All", ZhCN: "全部"}}, 400, nil)
	pins, err := h.store.HomepagePins()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.store.SaveHomepagePins(store.HomepagePins{Keys: []string{"acme/tool-6", "openai/codex"}, Revision: pins.Revision}); err != nil {
		t.Fatal(err)
	}
	data, _ = h.request("GET", "/api/home", nil, 200, nil)
	if !bytes.Contains(data, []byte("acme/tool-6")) {
		t.Fatal("pin missing", string(data))
	}
	h.setEnabled("apps/acme/tool-6", false)
	for _, path := range []string{"/api/home", "/api/catalog?q=tool-6", "/api/search?q=tool-6"} {
		data, _ = h.request("GET", path, nil, 200, nil)
		if bytes.Contains(data, []byte("acme/tool-6")) {
			t.Fatal("disabled app leaked", path, string(data))
		}
	}
	h.request("GET", "/acme/tool-6", nil, 404, nil)
	// Grouped template reset was replaced by field-level configuration unsets.
	for _, path := range []string{"/admin/api/apps/openai/codex/template", "/admin/api/vendors/openai/template"} {
		h.request("GET", path, nil, 404, nil)
	}
	if err := h.server.pool.SetProxy(distributor.ProxyUpdate{Mode: "url", URL: "http://fake-user:fake-secret@proxy.example:8080"}, h.server.pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	source := "# Fixture\n\n<script>window.fixtureInline=1</script>\n<script src=\"https://cdn.example/fixture.js\"></script>\n<img src=\"https://cdn.example/fixture.png\">\n\n{{app_name}} {{public_origin}} {{unknown}}"
	h.patchConfiguration("apps/acme/tool-0", map[string]any{"set": map[string]any{"instructions.en": source, "instructions.zh-CN": "<b>中文</b>"}}, 200)
	data, headers := h.request("GET", "/api/apps/acme/tool-0/instructions/document?lang=en", nil, 200, nil)
	for _, want := range []string{"<h1>Fixture</h1>", "<script>window.fixtureInline=1</script>", "https://cdn.example/fixture.js", "Tool 0", "{{unknown}}"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatal("document lost HTML, JS, or controlled placeholder", want, string(data))
		}
	}
	if bytes.Contains(data, []byte("fake-secret")) || !strings.Contains(headers.Get("Content-Security-Policy"), "sandbox allow-scripts ") {
		t.Fatal("private data leaked or JS not isolated")
	}
	h.patchConfiguration("apps/acme/tool-0", map[string]any{"set": map[string]any{"instructions.en": "", "instructions.zh-CN": ""}}, 200)
	data, _ = h.request("GET", "/api/apps/acme/tool-0/instructions/document?lang=en", nil, 200, nil)
	if bytes.Contains(data, []byte("Fixture")) {
		t.Fatal("explicit blank not preserved")
	}
	for _, path := range []string{"/api/bootstrap", "/api/home", "/api/vendors/acme"} {
		data, _ = h.request("GET", path, nil, 200, nil)
		if bytes.Contains(data, []byte("fake-secret")) {
			t.Fatal("proxy secret leaked", path)
		}
	}
}
func TestDownloadRankingCountsOnlySuccessfulPublicTransfers(t *testing.T) {
	h := newHarness(t, withTrustedProxies(t, "192.0.2.0/24"))
	h.login(h.password)
	if _, err := h.server.store.DB.Exec(`UPDATE catalog_state SET ranking_salt=zeroblob(32)`); err != nil {
		t.Fatal(err)
	}
	h.createVendor("content")
	a := h.createApp("content", "files", "hosted", nil)
	entry, _ := h.server.registry.Lookup(a.Key)
	_, err := h.server.hosted.Put(context.Background(), entry, "file.bin", "", "cccccccccccccccccccccccccccccccc", func(context.Context) (io.ReadCloser, int64, error) {
		return io.NopCloser(strings.NewReader("fixture bytes")), 13, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ranking := func(want int64) {
		t.Helper()
		rows, err := h.server.store.DownloadRanking(time.Now(), 20)
		for deadline := time.Now().Add(time.Second); want > 0 && (len(rows) != 1 || rows[0].Clients != want) && time.Now().Before(deadline); {
			time.Sleep(time.Millisecond)
			rows, err = h.server.store.DownloadRanking(time.Now(), 20)
		}
		if err != nil {
			t.Fatal(err)
		}
		if want == 0 {
			if len(rows) != 0 {
				t.Fatal("non-download counted", rows)
			}
		} else if len(rows) != 1 || rows[0].UID != a.UID || rows[0].Clients != want {
			t.Fatal(rows)
		}
	}
	ranking(0)
	_, headers := h.request("HEAD", "/content/files/file.bin", nil, 200, nil)
	h.request("GET", "/content/files/file.bin", nil, 304, map[string]string{"If-None-Match": headers.Get("ETag")})
	h.request("GET", "/content/files/missing.bin", nil, 404, nil)
	h.request("GET", "/api/apps/content/files/files", nil, 200, nil)
	ranking(0)
	for i := 0; i < 3; i++ {
		h.request("GET", "/content/files/file.bin", nil, 200, map[string]string{"X-Forwarded-For": fmt.Sprintf("192.0.2.%d", i+1)})
	}
	ranking(1) // untrusted spoofed headers ignored
	r := httptest.NewRequest("GET", "/content/files/file.bin", nil)
	r.RemoteAddr = "192.0.2.1:4321"
	r.Header.Set("Range", "bytes=1-3")
	r.Header.Set("X-Forwarded-For", "198.51.100.88")
	w := httptest.NewRecorder()
	h.server.ServeHTTP(w, r)
	if w.Code != 206 {
		t.Fatal(w.Code)
	}
	ranking(2)
	lease, err := h.server.downloads.AcquireHTTPWriter()
	if err != nil {
		t.Fatal(err)
	}
	defer lease()
	// Unrelated global capacity no longer blocks deleting this application.
	h.deleteApp(a.Key, a.UID, a.Revision, 200)

	ranking(0)
}
func TestCacheHitsRankAndReceiptRejectsFailedWrites(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		io.WriteString(w, "cached bytes")
	}))
	defer upstream.Close()
	h := newHarness(t)
	h.login(h.password)
	h.createVendor("cache")
	a := h.createApp("cache", "files", application.HttpCache, map[string]any{"base_url": upstream.URL})
	for i := 0; i < 2; i++ {
		h.request("GET", "/cache/files/tool.bin", nil, 200, nil)
	}
	scores, _ := h.server.store.DownloadRanking(time.Now(), 20)
	if len(scores) != 1 || scores[0].Clients != 1 {
		t.Fatal(scores)
	}
	counts, _ := h.server.store.CountersFor(a.MetricsID())
	if counts["cache_hit_requests"] != 1 {
		t.Fatal("fixture did not exercise a cache hit", counts)
	}
	for _, receipt := range []*downloadReceipt{{ResponseWriter: httptest.NewRecorder(), status: 200, failed: true}, {ResponseWriter: httptest.NewRecorder(), status: 304}, {ResponseWriter: httptest.NewRecorder(), status: 500}} {
		r := httptest.NewRequest("GET", "/cache/files/tool.bin", nil)
		r.RemoteAddr = "192.0.2.55:1234"
		h.server.finishDownload(receipt, r, a.UID)
	}
	scores, _ = h.server.store.DownloadRanking(time.Now(), 20)
	if scores[0].Clients != 1 {
		t.Fatal("failed receipt counted", scores)
	}
}
