package httpserver

import (
	"encoding/json"
	app "github.com/PMExtra/RedApp/internal/apps/codex"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestPublicOpenAISymbol(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "http://internal/apps/codex/icon.svg", nil))
	if w.Code != 200 || w.Body.String() != app.OpenAISymbol() || w.Header().Get("Content-Type") != "image/svg+xml; charset=utf-8" || w.Header().Get("Content-Security-Policy") != "sandbox; default-src 'none'" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unsafe or changed SVG response: %d %v", w.Code, w.Header())
	}
	for _, tc := range []struct {
		method, path string
		want         int
	}{{"POST", "/apps/codex/icon.svg", 405}, {"GET", "/apps/codex/icon.svg/", 404}, {"GET", "/apps/codex/icon.svg?other=1", 400}} {
		w = httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(tc.method, "http://internal"+tc.path, nil))
		if w.Code != tc.want {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
	}
}

func TestPublicPagesAndApplicationIsolation(t *testing.T) {
	// Nil database/catalog/auth prove that these routes never need privileged state or upstream I/O.
	s := &Server{}
	for _, path := range []string{"/", "/apps/codex", "/apps/claude-code", "/api/apps"} {
		r := httptest.NewRequest("GET", "http://internal:8080"+path, nil)
		r.Header.Set("Forwarded", "host=attacker.example;proto=https")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Set-Cookie") != "" {
			t.Fatalf("%s: %d headers=%v", path, w.Code, w.Header())
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
			t.Fatal("public page weakened CSP")
		}
		if path == "/api/apps" {
			var apps []map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &apps); err != nil || len(apps) != 2 || len(apps[0]) != 5 || len(apps[1]) != 5 || apps[1]["id"] != "claude-code" || apps[0]["id"] != "codex" || apps[0]["icon"] != "/apps/codex/icon.svg" || apps[0]["origin"] != "http://internal:8080" {
				t.Fatalf("public app data leaked state or trusted spoofed origin: %s %v", w.Body.String(), err)
			}
		} else if !strings.Contains(w.Body.String(), "/admin/assets/") || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
			t.Fatal("missing embedded entrypoint")
		}
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"POST", "/", 405}, {"POST", "/api/apps", 405}, {"GET", "/apps/other", 404}, {"GET", "/api/admin", 404}, {"GET", "/apps/codex/", 404}, {"GET", "/api/apps?origin=https://attacker.example", 400},
	} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(tc.method, "http://internal:8080"+tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
	}
}

func TestPublicApplicationTrustedAndFixedOrigin(t *testing.T) {
	proxy, _ := NewProxy("10.0.0.0/8")
	for _, tc := range []struct{ fixed, want string }{{"", "https://external.example:8443"}, {"http://internal:8080", "http://internal:8080"}} {
		s := Server{Proxy: proxy, Public: tc.fixed}
		r := httptest.NewRequest(http.MethodGet, "http://internal:8080/api/apps", nil)
		r.RemoteAddr = "10.0.0.1:80"
		r.Header.Set("Forwarded", "for=8.8.8.8;host=external.example:8443;proto=https")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	for _, header := range []string{"host=evil'$(id);proto=https", "host=external.example;proto=javascript"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "http://internal:8080/api/apps", nil)
		r.RemoteAddr = "10.0.0.1:80"
		r.Header.Set("Forwarded", header)
		(&Server{Proxy: proxy}).ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("accepted unsafe origin: %d", w.Code)
		}
	}
}

func TestPublicBuildInfo(t *testing.T) {
	for _, version := range []string{"", "0.4.0"} {
		s := Server{Version: version}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "http://internal/api/info", nil))
		var info map[string]any
		want := version
		if want == "" {
			want = "dev"
		}
		if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil || w.Code != 200 || len(info) != 4 || info["version"] != want || info["os"] != runtime.GOOS || info["arch"] != runtime.GOARCH {
			t.Fatalf("unexpected public info: %d %s", w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Set-Cookie") != "" {
			t.Fatal("public info must remain anonymous and uncached")
		}
	}
}
