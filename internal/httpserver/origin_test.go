package httpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	app "github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/auth"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRequestOriginTrustBoundary(t *testing.T) {
	p, _ := NewProxy("10.0.0.0/8")
	for _, tc := range []struct {
		name, peer, fwd, xff, host, proto, want string
		invalid                                 bool
	}{
		{name: "direct", peer: "8.8.8.8:80", want: "http://internal:8080"},
		{name: "untrusted ignores headers", peer: "8.8.8.8:80", fwd: "bad", host: "attacker", proto: "https", want: "http://internal:8080"},
		{name: "forwarded", peer: "10.0.0.1:80", fwd: `for=8.8.8.8;host=external.example;proto=https`, want: "https://external.example"},
		{name: "single without for", peer: "10.0.0.1:80", fwd: `host=external.example;proto=https`, want: "https://external.example"},
		{name: "untrusted prefix", peer: "10.0.0.1:80", fwd: `for=9.9.9.9;host=attacker;proto=http,for=8.8.8.8;host=external.example;proto=https`, want: "https://external.example"},
		{name: "trusted outer hop", peer: "10.0.0.1:80", fwd: `for=8.8.8.8;host=external.example;proto=https,for=10.0.0.2;host=internal;proto=http`, want: "https://external.example"},
		{name: "xff single", peer: "10.0.0.1:80", xff: "8.8.8.8", host: "external.example", proto: "https", want: "https://external.example"},
		{name: "xff aligned", peer: "10.0.0.1:80", xff: "9.9.9.9,8.8.8.8", host: "attacker,external.example", proto: "http,https", want: "https://external.example"},
		{name: "no mixed fallback", peer: "10.0.0.1:80", fwd: "bad", host: "external.example", proto: "https", invalid: true},
		{name: "bad scheme", peer: "10.0.0.1:80", proto: "javascript", invalid: true},
		{name: "unaligned", peer: "10.0.0.1:80", xff: "8.8.8.8", host: "attacker,external.example", invalid: true},
		{name: "injection", peer: "10.0.0.1:80", host: "external.example'$(id)", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://internal:8080/install.sh", nil)
			r.RemoteAddr = tc.peer
			if tc.fwd != "" {
				r.Header.Set("Forwarded", tc.fwd)
			}
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.host != "" {
				r.Header.Set("X-Forwarded-Host", tc.host)
			}
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			s := Server{Proxy: p}
			got, err := s.origin(r)
			if (err != nil) != tc.invalid || !tc.invalid && got != tc.want {
				t.Fatalf("got=%q err=%v", got, err)
			}
		})
	}
	for _, h := range []string{"a:0", "a:65536", "a:", "a..b", "-a", "a_foo", "a'", "[not-ip]", "::1", "a%20b", "a/<x>"} {
		if validHost(h) {
			t.Fatalf("accepted unsafe Host %q", h)
		}
	}
	for _, h := range []string{"localhost:8080", "example.com", "127.0.0.1", "[::1]:8080", "example.com."} {
		if !validHost(h) {
			t.Fatalf("rejected Host %q", h)
		}
	}
}

func TestAutomaticOriginHTTPIsolationAndSecurity(t *testing.T) {
	hash := sha256.Sum256([]byte("archive"))
	var base string
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(app.Release{Tag: "rust-v0.159.2", Assets: []app.Asset{{Name: "archive.tgz", Digest: "sha256:" + hex.EncodeToString(hash[:]), URL: base + "/releases/0.159.2/archive.tgz"}}})
	}))
	base = c.Base.String()
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
	a, e := auth.New(db, false, func(p string) { password = p })
	if e != nil {
		t.Fatal(e)
	}
	p, _ := NewProxy("10.0.0.0/8")
	s := Server{DB: db, Catalog: app.New(db, c), Downloads: manager, Auth: a, Proxy: p, Dir: dir, Started: time.Now()}
	request := func(host, path, method, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		r.RemoteAddr = "10.0.0.1:8080"
		r.Header.Set("Forwarded", "for=8.8.8.8;host="+host+";proto=https")
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	var wg sync.WaitGroup
	for _, host := range []string{"one.example", "two.example"} {
		host := host
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, path := range []string{"/install.sh", "/install.ps1", "/channels/latest"} {
				w := request(host, path, "GET", "", "")
				if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("https://"+host)) || w.Header().Get("Cache-Control") != "no-store" {
					t.Errorf("%s %s: %d %s", host, path, w.Code, w.Body)
				}
			}
		}()
	}
	wg.Wait()
	body, _ := json.Marshal(map[string]string{"password": password})
	rejected := request("one.example", "/admin/api/login", "POST", string(body), "https://two.example")
	if rejected.Code != 403 {
		t.Fatal("cross-origin login accepted")
	}
	w := request("one.example", "/admin/api/login", "POST", string(body), "https://one.example")
	if w.Code != 200 || len(w.Result().Cookies()) != 1 || !w.Result().Cookies()[0].Secure {
		t.Fatalf("secure login: %d %s", w.Code, w.Body)
	}
	var session map[string]string
	json.Unmarshal(w.Body.Bytes(), &session)
	r := httptest.NewRequest("POST", "http://one.example/admin/api/settings", strings.NewReader(`{"latest_ttl_seconds":60}`))
	r.RemoteAddr = "10.0.0.1:8080"
	r.Header.Set("Forwarded", "for=8.8.8.8;host=one.example;proto=https")
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(w.Result().Cookies()[0])
	rejected = httptest.NewRecorder()
	s.ServeHTTP(rejected, r)
	if rejected.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	r.Method = "GET"
	r.URL.Path = "/admin/api/status"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var status map[string]any
	json.Unmarshal(w.Body.Bytes(), &status)
	if status["public_base_url"] != "https://one.example" {
		t.Fatalf("status origin %v", status["public_base_url"])
	}
	if s.Public != "" || a.Secure {
		t.Fatal("request mutated shared origin/auth")
	}
	s.Public = "https://fixed.example"
	r = httptest.NewRequest("GET", "http://one.example/install.sh", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("explicit Host mismatch accepted")
	}
	r = httptest.NewRequest("GET", "http://fixed.example/install.sh", nil)
	r.Header.Set("Forwarded", "host=attacker;proto=http")
	r.RemoteAddr = "10.0.0.1:80"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("https://fixed.example")) {
		t.Fatal("explicit origin overridden")
	}
}
