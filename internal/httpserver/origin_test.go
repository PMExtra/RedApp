package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
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
		// A valid trusted forwarded authority must not hide an invalid raw Host.
		r := httptest.NewRequest("GET", "http://internal:8080/health/ready", nil)
		r.Host, r.RemoteAddr = h, "10.0.0.1:8080"
		r.Header.Set("Forwarded", "host=external.example;proto=https")
		s := Server{Proxy: p}
		if _, err := s.origin(r); err == nil {
			t.Fatalf("forwarded header hid unsafe Host %q", h)
		}
	}
	for _, h := range []string{"localhost:8080", "example.com", "127.0.0.1", "[::1]:8080", "example.com."} {
		if !validHost(h) {
			t.Fatalf("rejected Host %q", h)
		}
	}
}

func TestAutomaticOriginHTTPIsolationAndSecurity(t *testing.T) {
	s, _, password := newTestServer(t, nil)
	s.Proxy, _ = NewProxy("10.0.0.0/8")
	request := func(host, path, method, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		r.RemoteAddr = "10.0.0.1:8080"
		r.Header.Set("Forwarded", "for=8.8.8.8;host="+host+";proto=https")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	var wg sync.WaitGroup
	for _, host := range []string{"one.example", "two.example"} {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			for _, path := range []string{"/openai/codex/install.sh", "/anthropic/claude-code/install.sh", "/api/bootstrap"} {
				w := request(host, path, "GET", "", "")
				if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("https://"+host)) || w.Header().Get("Cache-Control") != "no-store" {
					t.Errorf("%s %s: %d", host, path, w.Code)
				}
			}
		}(host)
	}
	wg.Wait()
	for _, host := range []string{"remote.example:9443", "192.0.2.25:8080", "[2001:db8::1]:8080"} {
		for _, path := range []string{"/health/ready", "/", "/admin/overview"} {
			if w := request(host, path, "GET", "", ""); w.Code != 200 {
				t.Fatalf("valid custom Host %s at %s: %d", host, path, w.Code)
			}
		}
		if w := request(host, "/admin/api/status", "GET", "", ""); w.Code != 401 {
			t.Fatalf("custom Host bypassed authentication: %d", w.Code)
		}
	}
	public := "https://published.example"
	if _, err := s.PublicConfig.Set(&public, 0); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"password": password})
	if w := request("one.example", "/admin/api/login", "POST", string(body), "https://published.example"); w.Code != 403 {
		t.Fatal("public origin granted CSRF trust")
	}
	w := request("one.example", "/admin/api/login", "POST", string(body), "https://one.example")
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("login %d %s", w.Code, w.Body)
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.Domain != "" || cookie.Path != "/admin" || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("login cookie protection changed")
	}
	if w = request("one.example", "/openai/codex/install.sh", "GET", "", ""); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(public+"/openai/codex")) {
		t.Fatal("public override not used", w.Code)
	}
	if w = request("published.example", "/openai/codex/install.sh", "GET", "", ""); w.Code != 200 {
		t.Fatal("valid publication Host rejected", w.Code)
	}
	if w = request("another.example", "/api/bootstrap", "GET", "", ""); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(public)) {
		t.Fatal("public override restricted the request Host", w.Code)
	}
	// Public settings never bypass Host syntax validation, even for health checks.
	r := httptest.NewRequest("GET", "http://internal/health/ready", nil)
	r.Host = "bad_host"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("invalid Host accepted with public override", w.Code)
	}
}

func TestLoginFailureCodesAndLogoutCookie(t *testing.T) {
	s, _, password := newTestServer(t, nil)
	login := func(ip, password string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"password": password})
		r := httptest.NewRequest("POST", "https://one.example/admin/api/login", strings.NewReader(string(body)))
		r.RemoteAddr = ip + ":443"
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	code := func(w *httptest.ResponseRecorder) string {
		var problem struct{ Error struct{ Code string } }
		json.Unmarshal(w.Body.Bytes(), &problem)
		return problem.Error.Code
	}
	for i := 0; i < 10; i++ {
		if w := login("192.0.2.1", "wrong"); w.Code != 401 || code(w) != "LOGIN_FAILED" || len(w.Result().Cookies()) != 0 {
			t.Fatal("wrong password", i, w.Code, w.Body)
		}
	}
	if w := login("192.0.2.1", password); w.Code != 429 || code(w) != "LOGIN_RATE_LIMITED" || len(w.Result().Cookies()) != 0 {
		t.Fatal("rate limit", w.Code, w.Body)
	}
	w := login("192.0.2.2", password)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var session map[string]string
	json.Unmarshal(w.Body.Bytes(), &session)
	r := httptest.NewRequest("POST", "https://one.example/admin/api/logout", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", session["csrf"])
	r.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if c := w.Result().Cookies(); w.Code != 200 || len(c) != 1 || c[0].Name != "redapp_session" || c[0].Value != "" || c[0].MaxAge >= 0 || !c[0].Secure {
		t.Fatal("logout did not expire the cookie", w.Code, c)
	}
}
