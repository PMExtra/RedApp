package httpserver

import (
	"bytes"
	"encoding/json"
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
			s := Server{Proxy: p, AllowedHosts: []string{"internal:8080", "external.example"}}
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
	s, _, password := newTestServer(t, nil)
	s.Proxy, _ = NewProxy("10.0.0.0/8")
	s.AllowedHosts = []string{"one.example", "two.example"}
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
	public := "https://published.example"
	if _, err := s.PublicConfig.Set(&public, 0); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"password": password})
	if w := request("one.example", "/admin/api/login", "POST", string(body), "https://published.example"); w.Code != 403 {
		t.Fatal("public origin granted CSRF trust")
	}
	w := request("one.example", "/admin/api/login", "POST", string(body), "https://one.example")
	if w.Code != 200 || len(w.Result().Cookies()) != 1 || !w.Result().Cookies()[0].Secure {
		t.Fatalf("login %d %s", w.Code, w.Body)
	}
	if w = request("one.example", "/openai/codex/install.sh", "GET", "", ""); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(public+"/openai/codex")) {
		t.Fatal("public override not used", w.Code)
	}
	if w = request("published.example", "/openai/codex/install.sh", "GET", "", ""); w.Code != 400 {
		t.Fatal("public override expanded allowed hosts", w.Code)
	}
	if s.Auth.Secure {
		t.Fatal("request changed global cookie state")
	}
}
