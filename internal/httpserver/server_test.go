package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	app "github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/history"
	"github.com/PMExtra/RedApp/internal/testutil"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminHTTPDownloadMetricsAndCSRF(t *testing.T) {
	data := []byte("official archive")
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])
	var base string
	upstream, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "latest") || strings.HasSuffix(r.URL.Path, "release.json") {
			json.NewEncoder(w).Encode(app.Release{Tag: "rust-v0.159.2", Assets: []app.Asset{{Name: "archive.tgz", Digest: "sha256:" + hash, URL: base + "/releases/0.159.2/archive.tgz"}}})
		} else {
			w.Write(data)
		}
	}))
	base = upstream.Base.String()
	handler, db, password := newTestServer(t, upstream)
	server := startTestServer(t, handler)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	csrf := ""
	request := func(method, path string, body any, secure bool, revision string) (int, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			reader = bytes.NewReader(b)
		}
		r, _ := http.NewRequest(method, server.URL+path, reader)
		r.Header.Set("Content-Type", "application/json")
		if secure {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		if revision != "" {
			r.Header.Set("If-Match", revision)
		}
		response, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		b, e := io.ReadAll(response.Body)
		if e != nil {
			t.Fatal(e)
		}
		return response.StatusCode, b
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/unknown", 404}, {"POST", "/", 405}, {"GET", "/?unexpected=1", 400}, {"GET", "/admin/unknown", 404}, {"GET", "/admin/api/status", 401}, {"POST", "/health/live", 405}, {"GET", "/install.sh", 404}, {"GET", "/api/info", 404}, {"GET", "/apps/codex", 404}} {
		if status, body := request(tc.method, tc.path, nil, false, ""); status != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, status, body)
		}
	}
	if status, body := request("POST", "/admin/api/login", map[string]string{"password": password}, false, ""); status != 200 {
		t.Fatal(status, string(body))
	} else {
		var session map[string]string
		json.Unmarshal(body, &session)
		csrf = session["csrf"]
	}
	endpoint := "/admin/api/apps/openai/codex/settings"
	if code, _ := request("PUT", endpoint, map[string]int{"channel_ttl_seconds": 120}, false, "0"); code != 403 {
		t.Fatal("CSRF not enforced", code)
	}
	if code, body := request("PUT", endpoint, map[string]int{"channel_ttl_seconds": 120}, true, "0"); code != 200 {
		t.Fatal(code, string(body))
	}
	if code, _ := request("PUT", endpoint, map[string]int{"channel_ttl_seconds": 10}, true, "0"); code != 409 {
		t.Fatal("stale revision accepted", code)
	}
	if code, body := request("GET", "/admin/api/apps/anthropic/claude-code/settings", nil, false, ""); code != 200 || !bytes.Contains(body, []byte(`"channel_ttl_seconds":60`)) {
		t.Fatal("TTL crossed apps", code, string(body))
	}
	for range 2 {
		if code, body := request("GET", "/openai/codex/releases/0.159.2/archive.tgz", nil, false, ""); code != 200 || !bytes.Equal(body, data) {
			t.Fatal(code, string(body))
		}
	}
	if code, _ := request("GET", "/openai/codex/releases/0.159.2/unlisted", nil, false, ""); code != 404 {
		t.Fatal("unauthorized resource", code)
	}
	counters, _ := db.Counters()
	owned, _ := db.CountersFor("openai/codex")
	if counters["artifact_requests"] != 2 || counters["miss_requests"] != 1 || counters["cache_hit_requests"] != 1 || counters["downstream_bytes"] != int64(2*len(data)) || owned["downstream_bytes"] != counters["downstream_bytes"] {
		t.Fatal(counters, owned)
	}
	if _, ok := counters["reuse_requests"]; ok {
		t.Fatal("retired counter written")
	}
	if err := db.SeenFor("anthropic/claude-code", "0.159.2"); err != nil {
		t.Fatal(err)
	}
	status, err := handler.status(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range status["metrics"].([]history.Metric) {
		if metric.Key == "versions.total" && *metric.Value != 2 {
			t.Fatal("global versions lost application", *metric.Value)
		}
	}
	for _, path := range []string{"/admin/api/status", "/admin/api/apps/openai/codex/status"} {
		code, body := request("GET", path, nil, false, "")
		var summary map[string]json.RawMessage
		if code != 200 || json.Unmarshal(body, &summary) != nil {
			t.Fatal("status summary unavailable", code, string(body))
		}
		for _, key := range []string{"resources", "events", "versions", "version_stats", "application_versions"} {
			if _, present := summary[key]; present {
				t.Fatal("status summary contains an unbounded list", path, key)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	handler.SampleHistory(ctx, func(e error) { t.Fatal(e) })
	if code, body := request("GET", "/admin/api/history?scope=global&metric=versions.total&range=24h", nil, false, ""); code != 200 {
		t.Fatal(code, string(body))
	}
	if code, body := request("GET", "/admin/api/apps/openai/codex/history?metric=versions.total&range=24h", nil, false, ""); code != 200 {
		t.Fatal(code, string(body))
	}
	code, body := request("POST", "/admin/api/apps/openai/codex/cleanup/preview", map[string]string{"minimum_version": "0.160.0"}, true, "")
	if code != 200 {
		t.Fatal(code, string(body))
	}
	var preview struct {
		Job                  struct{ ID string }
		LogicalBytes         int64 `json:"logical_bytes"`
		ReclaimableBlobBytes int64 `json:"reclaimable_blob_bytes"`
		Active               int   `json:"active"`
	}
	json.Unmarshal(body, &preview)
	if preview.LogicalBytes != int64(len(data)) || preview.ReclaimableBlobBytes != int64(len(data)) || preview.Active != 0 {
		t.Fatal("cleanup preview lost frozen byte and activity counts", string(body))
	}
	if code, _ = request("POST", "/admin/api/apps/anthropic/claude-code/cleanup/"+preview.Job.ID+"/execute", map[string]any{}, true, ""); code != 409 {
		t.Fatal("cross-app cleanup accepted", code)
	}
	for range 2 {
		if code, body = request("POST", "/admin/api/apps/openai/codex/cleanup/"+preview.Job.ID+"/execute", map[string]any{}, true, ""); code != 200 {
			t.Fatal(code, string(body))
		}
	}
	if code, body = request("POST", "/admin/api/password", map[string]string{"old": password, "new": "correct-horse-battery-new"}, true, ""); code != 200 {
		t.Fatal(code, string(body))
	}
	if code, _ = request("GET", "/admin/api/session", nil, false, ""); code != 401 {
		t.Fatal("password did not invalidate session", code)
	}
}
func TestProxyTrustedMultiHopIPv6AndMalformed(t *testing.T) {
	p, e := NewProxy("10.0.0.0/8,fd00::/8")
	if e != nil {
		t.Fatal(e)
	}
	cases := []struct{ peer, forwarded, xff, want string }{{"198.51.100.2:10", "for=1.1.1.1", "", "198.51.100.2"}, {"10.0.0.3:10", "for=192.0.2.1, for=10.0.0.2", "", "192.0.2.1"}, {"10.0.0.3:10", "for=1.1.1.1, for=198.51.100.7", "", "198.51.100.7"}, {"[fd00::1]:10", "for=\"[2001:db8::1]:4711\";proto=https;host=enterprise.example", "", "2001:db8::1"}, {"10.0.0.3:10", "for=192.0.2.1", "1.1.1.1", "192.0.2.1"}, {"10.0.0.3:10", "for=\"broken", "1.1.1.1", "10.0.0.3"}, {"10.0.0.3:10", "", "192.0.2.1, 10.0.0.2", "192.0.2.1"}, {"10.0.0.3:10", "for=192.0.2.1;host=evil/@x", "", "10.0.0.3"}}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "https://enterprise.example", nil)
		r.RemoteAddr = c.peer
		if c.forwarded != "" {
			r.Header.Set("Forwarded", c.forwarded)
		}
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := p.ClientIP(r); got != c.want {
			t.Errorf("%+v: %s", c, got)
		}
	}
}
func TestPublicURLRejectsInjectionAndSubpaths(t *testing.T) {
	for _, s := range []string{"https://good.example/evil", "https://user:secret@good.example", "https://good.example?q=1", "https://good.example/'$(id)'", "javascript://evil"} {
		if _, e := PublicURL(s); e == nil {
			t.Fatal(s)
		}
	}
}
