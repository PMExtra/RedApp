package httpserver

import (
	"bytes"
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/apps/codex"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicPagesBootstrapAndApplicationIsolation(t *testing.T) {
	s, _, _ := newTestServer(t, nil)
	s.Version = "test-build"
	for _, path := range []string{"/", "/openai/codex", "/anthropic/claude-code", "/admin/login", "/admin/overview", "/admin/settings/site", "/admin/vendors/anthropic/apps/claude-code/settings"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "http://internal"+path, nil))
		if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
			t.Fatalf("%s %d %s", path, w.Code, w.Body)
		}
	}
	for _, path := range []string{"/openai/missing", "/openai/codex/missing", "/admin/missing", "/admin/api/missing", "/api/info", "/apps/codex"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "http://internal"+path, nil))
		if w.Code == 200 {
			t.Fatalf("unknown path accepted %s", path)
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "http://internal/api/bootstrap", nil))
	var info map[string]json.RawMessage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &info) != nil || len(info) != 7 || string(info["version"]) != `"test-build"` {
		t.Fatal(w.Code, w.Body)
	}
	for _, secret := range []string{"password", "has_credentials", "upstream_proxy"} {
		if bytes.Contains(w.Body.Bytes(), []byte(secret)) {
			t.Fatal("private settings leaked")
		}
	}
	var apps []map[string]any
	json.Unmarshal(info["apps"], &apps)
	if len(apps) != 2 || apps[0]["id"] != "openai/codex" || apps[1]["id"] != "anthropic/claude-code" || apps[0]["origin"] != "http://internal/openai/codex" {
		t.Fatal(apps)
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "http://internal/openai/codex/icon.svg", nil))
	if w.Code != 200 || w.Body.String() != codex.OpenAISymbol() || w.Header().Get("Content-Security-Policy") != "sandbox; default-src 'none'" {
		t.Fatal("reviewed icon unavailable", w.Code)
	}
}
