package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	app "github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/auth"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminHTTPDownloadMetricsAndCSRF(t *testing.T) {
	data := []byte("official archive")
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])
	var upstreamBase string
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "latest") || strings.HasSuffix(r.URL.Path, "release.json") {
			json.NewEncoder(w).Encode(app.Release{Tag: "rust-v0.159.2", Assets: []app.Asset{{Name: "archive.tgz", Digest: "sha256:" + hash, URL: upstreamBase + "/releases/0.159.2/archive.tgz"}}})
		} else {
			w.Write(data)
		}
	}))
	upstreamBase = c.Base.String()
	dir := t.TempDir()
	db, e := store.Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	manager, e := download.New(dir, db, c)
	if e != nil {
		t.Fatal(e)
	}
	defer manager.Close()
	var password string
	bootstraps := 0
	a, e := auth.New(db, false, func(p string) { password = p; bootstraps++ })
	if e != nil {
		t.Fatal(e)
	}
	if _, e = auth.New(db, false, func(p string) { bootstraps++ }); e != nil || bootstraps != 1 {
		t.Fatal("重启重复输出密码", e)
	}
	var stored []byte
	db.DB.QueryRow("SELECT hash FROM admin").Scan(&stored)
	if bytes.Contains(stored, []byte(password)) || !strings.HasPrefix(string(stored), "$2a$") {
		t.Fatal("未安全保存密码")
	}
	handler := &Server{DB: db, Catalog: app.New(db, c), Downloads: manager, Auth: a, Dir: dir, Started: time.Now()}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	handler.Public = httpServer.URL
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	csrf := ""
	request := func(method, path string, body any, withCSRF bool) (int, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			reader = bytes.NewReader(b)
		}
		r, e := http.NewRequest(method, httpServer.URL+path, reader)
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Content-Type", "application/json")
		if withCSRF {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		resp, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		b, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, b
	}
	if code, _ := request("GET", "/admin/api/status", nil, false); code != 401 {
		t.Fatal(code)
	}
	if code, _ := request("GET", "/health/ready", nil, false); code != 200 {
		t.Fatal(code)
	}
	if code, b := request("POST", "/admin/api/login", map[string]string{"password": password}, false); code != 200 {
		t.Fatal(code, string(b))
	} else {
		var session map[string]string
		json.Unmarshal(b, &session)
		csrf = session["csrf"]
	}
	if code, _ := request("POST", "/admin/api/settings", map[string]int{"latest_ttl_seconds": 120}, false); code != 403 {
		t.Fatal("CSRF 未阻止", code)
	}
	if code, _ := request("POST", "/admin/api/settings", map[string]int{"latest_ttl_seconds": 120}, true); code != 200 {
		t.Fatal(code)
	}
	for i := 0; i < 2; i++ {
		code, b := request("GET", "/releases/0.159.2/archive.tgz", nil, false)
		if code != 200 || !bytes.Equal(b, data) {
			t.Fatal(code, string(b))
		}
	}
	if code, _ := request("GET", "/releases/0.159.2/unlisted", nil, false); code != 404 {
		t.Fatal("开放代理", code)
	}
	code, b := request("GET", "/admin/api/status", nil, false)
	if code != 200 {
		t.Fatal(code)
	}
	var status struct {
		Counters map[string]int64
		Disk     map[string]int64
		Versions map[string]string
	}
	if e = json.Unmarshal(b, &status); e != nil {
		t.Fatal(e)
	}
	if status.Counters["upstream_bytes"] != int64(len(data)) || status.Counters["downstream_bytes"] != int64(2*len(data)) || status.Counters["reuse_requests"] != 1 {
		t.Fatal(status.Counters)
	}
	if status.Disk["used_bytes"] < status.Disk["cache_bytes"] || status.Versions["0.159.2"] == "" {
		t.Fatal(status)
	}
	code, b = request("POST", "/admin/api/cleanup/preview", map[string]string{"minimum_version": "0.160.0"}, true)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	var preview struct{ Job download.Cleanup }
	json.Unmarshal(b, &preview)
	if code, _ = request("POST", "/admin/api/cleanup/execute", map[string]string{"cleanup_id": preview.Job.ID}, true); code != 200 {
		t.Fatal(code)
	}
	if code, _ = request("POST", "/admin/api/password", map[string]string{"old": password, "new": "new-password-for-test-only"}, true); code != 200 {
		t.Fatal(code)
	}
	if code, _ = request("GET", "/admin/api/status", nil, false); code != 401 {
		t.Fatal("改密未注销旧会话")
	}
	if code, b = request("GET", "/install.sh", nil, false); code != 200 || bytes.Contains(b, []byte("https://github.com")) || !bytes.Contains(b, []byte(httpServer.URL)) {
		t.Fatal("企业安装器地址不正确")
	}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", httpServer.URL+"/channels/latest", nil)
	req.Host = "attacker.example"
	resp, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatal("Host 注入未拒绝")
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
