package httpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouterErrorsUseTheErrorDocument(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		method, path string
		status       int
		code         errorCode
	}{
		{"POST", "/", 405, codeMethodNotAllowed},
		{"PUT", "/api/bootstrap", 405, codeMethodNotAllowed},
		{"GET", "/?unexpected=1", 400, codeInvalidQuery},
		{"GET", "/api/bootstrap?a=1", 400, codeInvalidQuery},
		{"GET", "/api/catalog?page=1&page=2", 400, codeInvalidQuery},
		{"GET", "/api/catalog?q=", 400, codeInvalidQuery},
		{"GET", "/admin/api/session", 401, codeAuthRequired},
		{"POST", "/health/live", 405, codeMethodNotAllowed},
		{"GET", "/api/info", 404, codeNotFound},
		{"GET", "/api/apps/openai", 404, codeNotFound},
		{"GET", "/assets/a/b/c", 404, codeNotFound},
		{"GET", "/openai//codex", 400, codeInvalidPath},
		{"GET", "/openai/codex/%2e%2e/x", 400, codeInvalidPath},
		{"GET", "/openai/codex/a%2Fb", 400, codeInvalidPath},
		{"GET", "/openai/codex/a%5Cb", 400, codeInvalidPath},
	} {
		code, body, header := h.raw(tc.method, tc.path, nil, "", nil)
		if code != tc.status || errorCodeOf(t, body) != string(tc.code) {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, code, body)
		}
		if tc.code == codeMethodNotAllowed && header.Get("Allow") == "" {
			t.Errorf("%s %s: 405 without Allow", tc.method, tc.path)
		}
	}
	if code, body, _ := h.raw("GET", "/admin/unknown", nil, "", nil); code != 404 || entryDocument(body) != "admin" {
		t.Fatalf("unknown admin page: %d %.80s", code, body)
	}
	if code, body, _ := h.raw("GET", "/unknown", nil, "", nil); code != 404 || entryDocument(body) != "public" {
		t.Fatalf("unknown vendor page: %d %.80s", code, body)
	}
}

func TestRequestIDIsSharedByHeaderErrorAndLogs(t *testing.T) {
	h := newHarness(t)
	code, body, header := h.raw("GET", "/api/vendors/missing", nil, "", nil)
	id := header.Get("X-Request-Id")
	var e errorBody
	if code != 404 || decodeJSONBody[errorBody](t, body).Error.RequestID != id || len(id) != 16 {
		t.Fatal(code, string(body), id, e)
	}
	_, _, second := h.raw("GET", "/health/live", nil, "", nil)
	if second.Get("X-Request-Id") == id {
		t.Fatal("request IDs repeat")
	}
	logs := h.logs.String()
	if !strings.Contains(logs, "request_id="+id) || !strings.Contains(logs, "code=VENDOR_NOT_FOUND") || !strings.Contains(logs, "operation=getPublicVendor") {
		t.Fatalf("access log lacks the request: %s", logs)
	}
}

func TestPanicsBecomeInternalErrors(t *testing.T) {
	h := newHarness(t)
	h.server.mux.HandleFunc("GET /panic-test", func(w http.ResponseWriter, r *http.Request) { panic("handler bug") })
	r := httptest.NewRequest("GET", "http://internal/panic-test", nil)
	w := httptest.NewRecorder()
	h.server.ServeHTTP(w, r)
	if w.Code != 500 || errorCodeOf(t, w.Body.Bytes()) != "INTERNAL_ERROR" || strings.Contains(w.Body.String(), "handler bug") {
		t.Fatal(w.Code, w.Body.String())
	}
	if !strings.Contains(h.logs.String(), "handler bug") {
		t.Fatal("panic not logged")
	}
}

func TestDownloadCountersCSRFAndPasswordChange(t *testing.T) {
	data := []byte("official archive")
	h := newHarness(t)
	var served int
	h.upstreamProxy(codexRelease("0.159.2", map[string][]byte{"archive.tgz": data}, func(string) { served++ }))
	key := h.releaseApp("fixture", "codex", "codex")
	h.login("")
	csrf := h.csrf
	h.csrf = ""
	h.expectError("DELETE", "/admin/api/session", nil, 403, codeCSRFRejected, nil)
	h.expectError("POST", "/admin/api/password", map[string]string{"current_password": h.password, "new_password": "correct-horse-battery-new"}, 403, codeCSRFRejected, nil)
	h.csrf = csrf
	for range 2 {
		body, header := h.request("GET", "/"+key+"/releases/0.159.2/archive.tgz", nil, 200, nil)
		if !bytes.Equal(body, data) || header.Get("X-Expected-SHA256") == "" || header.Get("Accept-Ranges") != "none" {
			t.Fatal(string(body), header)
		}
	}
	if _, header := h.request("HEAD", "/"+key+"/releases/0.159.2/archive.tgz", nil, 200, nil); header.Get("X-Expected-SHA256") == "" {
		t.Fatal("HEAD lost artifact headers", header)
	}
	h.expectError("GET", "/"+key+"/releases/0.159.2/unlisted", nil, 404, codeFileNotFound, nil)
	h.expectError("GET", "/"+key+"/releases/0.159.2/archive.tgz?x=1", nil, 400, codeInvalidQuery, nil)
	if served != 1 {
		t.Fatal("artifact fetched more than once or HEAD downloaded", served)
	}
	counters, _ := h.store.Counters()
	if counters["artifact_requests"] != 2 || counters["miss_requests"] != 1 || counters["cache_hit_requests"] != 1 || counters["downstream_bytes"] != int64(2*len(data)) {
		t.Fatal(counters)
	}
	if _, ok := counters["reuse_requests"]; ok {
		t.Fatal("retired counter written")
	}
	h.expectError("POST", "/admin/api/password", map[string]string{"current_password": "wrong password", "new_password": "correct-horse-battery-new"}, 400, codeCurrentPasswordIncorrect, nil)
	h.expectError("POST", "/admin/api/password", map[string]string{"current_password": h.password, "new_password": "short"}, 400, codePasswordInvalid, nil)
	h.expectError("POST", "/admin/api/password", map[string]string{"old": h.password, "new": "correct-horse-battery-new"}, 400, codeInvalidRequest, nil)
	code, _, _ := h.raw("POST", "/admin/api/password", strings.NewReader(`{"current_password":"x","new_password":"y"}`), "text/plain", nil)
	if code != 415 {
		t.Fatal("non-JSON body accepted", code)
	}
	_, header := h.request("POST", "/admin/api/password", map[string]string{"current_password": h.password, "new_password": "correct-horse-battery-new"}, 204, nil)
	if !strings.Contains(header.Get("Set-Cookie"), "Max-Age=0") {
		t.Fatal("password change did not expire the cookie", header)
	}
	h.expectError("GET", "/admin/api/session", nil, 401, codeAuthRequired, nil)
	h.login("correct-horse-battery-new")
	if !strings.Contains(h.logs.String(), "operation=changePassword") || strings.Contains(h.logs.String(), "correct-horse-battery-new") || strings.Contains(h.logs.String(), h.csrf) {
		t.Fatal("logs miss the operation or contain secrets")
	}

}

func TestProxyTrustedMultiHopIPv6AndMalformed(t *testing.T) {
	p, e := ParseTrustedProxies([]string{"10.0.0.0/8", "fd00::/8"})
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
	if _, err := ParseTrustedProxies([]string{"not-a-cidr"}); err == nil {
		t.Fatal("invalid CIDR accepted")
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	if _, err := New(Deps{}); err == nil || !strings.Contains(err.Error(), "Store") || !strings.Contains(err.Error(), "Prewarmer") {
		t.Fatal(err)
	}
}
