package httpserver

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONBodiesAreDecodedStrictly(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
		code                    errorCode
	}{
		{"wrong media type", `{"password":"x"}`, "text/plain", 415, codeUnsupportedMediaType},
		{"missing media type", `{"password":"x"}`, "", 415, codeUnsupportedMediaType},
		{"foreign charset", `{"password":"x"}`, "application/json; charset=latin1", 415, codeUnsupportedMediaType},
		{"duplicate key", `{"password":"x","password":"y"}`, "application/json", 400, codeInvalidRequest},
		{"case-folded duplicate", `{"password":"x","PASSWORD":"y"}`, "application/json", 400, codeInvalidRequest},
		{"unknown field", `{"password":"x","remember":true}`, "application/json", 400, codeInvalidRequest},
		{"null", `{"password":null}`, "application/json", 400, codeInvalidRequest},
		{"wrong type", `{"password":1}`, "application/json", 400, codeInvalidRequest},
		{"invalid UTF-8", "{\"password\":\"\xff\"}", "application/json", 400, codeInvalidRequest},
		{"trailing data", `{"password":"x"} {}`, "application/json", 400, codeInvalidRequest},
		{"empty password", `{"password":""}`, "application/json", 400, codeInvalidRequest},
		{"too large", `{"password":"` + strings.Repeat("x", 9000) + `"}`, "application/json; charset=utf-8", 413, codePayloadTooLarge},
	} {
		code, body, _ := h.raw("POST", "/admin/api/session", strings.NewReader(tc.body), tc.contentType, nil)
		if code != tc.status || errorCodeOf(t, body) != string(tc.code) {
			t.Errorf("%s: %d %s", tc.name, code, body)
		}
	}
}

func TestAdminOriginCheckAppliesToEveryAdminRequest(t *testing.T) {
	h := newHarness(t)
	h.login("")
	h.expectError("GET", "/admin/api/session", nil, 403, codeOriginRejected, map[string]string{"Origin": "https://foreign.example"})
	h.request("GET", "/admin/api/session", nil, 200, map[string]string{"Origin": h.http.URL})
	// Public routes do not check Origin.
	h.request("GET", "/api/bootstrap", nil, 200, map[string]string{"Origin": "https://foreign.example"})
}

func TestIfMatchAcceptsOnlyOneQuotedPositiveRevision(t *testing.T) {
	for _, tc := range []struct {
		values []string
		want   int64
	}{
		{[]string{`"7"`}, 7},
		{[]string{`"123456789012345678"`}, 123456789012345678},
		{nil, 0},
		{[]string{`7`}, 0},
		{[]string{`"0"`}, 0},
		{[]string{`"07"`}, 0},
		{[]string{`W/"7"`}, 0},
		{[]string{`*`}, 0},
		{[]string{`"7", "8"`}, 0},
		{[]string{`"7"`, `"8"`}, 0},
		{[]string{`"1234567890123456789"`}, 0},
	} {
		r := httptest.NewRequest("PUT", "http://internal/x", nil)
		for _, v := range tc.values {
			r.Header.Add("If-Match", v)
		}
		got, e := ifMatch(r)
		if tc.want == 0 && (e == nil || e.code != codeIfMatchRequired) || tc.want != 0 && (e != nil || got != tc.want) {
			t.Errorf("%q: %d %v", tc.values, got, e)
		}
	}
}

func TestErrorResponsesNeverCarryInternalErrorText(t *testing.T) {
	h := newHarness(t)
	r := httptest.NewRequest("GET", "http://internal/x", nil)
	w := httptest.NewRecorder()
	h.server.writeError(w, r, storageError(io.ErrUnexpectedEOF))
	if w.Code != 503 || strings.Contains(w.Body.String(), "unexpected EOF") || errorCodeOf(t, w.Body.Bytes()) != "STORAGE_UNAVAILABLE" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.server.writeError(w, r, newError("NOT_A_CATALOG_CODE", nil, "x"))
	if w.Code != 500 || errorCodeOf(t, w.Body.Bytes()) != "INTERNAL_ERROR" {
		t.Fatal("unknown code escaped the catalog", w.Code, w.Body.String())
	}
	if got := redactError(io.ErrUnexpectedEOF); got != "unexpected EOF" {
		t.Fatal(got)
	}
	if got := redactError(errorString("proxyconnect tcp: http://ops:secret@proxy.internal:3128 refused")); strings.Contains(got, "secret") || !strings.Contains(got, "http://****@proxy.internal") {
		t.Fatal(got)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

func TestHealthProbes(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/health/live", "/health/ready"} {
		if body, _ := h.request("GET", path, nil, 200, nil); string(body) != "{\"ok\":true}\n" {
			t.Fatal(path, string(body))
		}
		h.expectError("GET", path+"?verbose=1", nil, 400, codeInvalidQuery, nil)
	}
	if err := h.store.Close(); err != nil {
		t.Fatal(err)
	}
	h.expectError("GET", "/health/ready", nil, 503, codeNotReady, nil)
	h.request("GET", "/health/live", nil, 200, nil)
}

func TestBuildAssetsAndUploadedIcons(t *testing.T) {
	h := newHarness(t)
	script := "public-fixture.js"
	_, header := h.request("GET", "/assets/"+script, nil, 200, nil)
	if header.Get("Content-Type") != "text/javascript; charset=utf-8" || header.Get("Cache-Control") != immutableCache {
		t.Fatal(header)
	}
	for _, path := range []string{"/assets/missing.js", "/assets/index.html", "/assets/" + script + ".map"} {
		h.expectError("GET", path, nil, 404, codeFileNotFound, nil)
	}
	icon, err := h.server.icons.Put(bytes.NewReader([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><rect width="1" height="1"/></svg>`)))
	if err != nil {
		t.Fatal(err)
	}
	body, header := h.request("GET", icon, nil, 200, nil)
	if !bytes.Contains(body, []byte("<svg")) || header.Get("Content-Security-Policy") != sandboxPolicy || header.Get("Cache-Control") != immutableCache || header.Get("ETag") == "" {
		t.Fatal(header)
	}
	h.request("GET", icon, nil, 304, map[string]string{"If-None-Match": header.Get("ETag")})
	if body, _ = h.request("HEAD", icon, nil, 200, nil); len(body) != 0 {
		t.Fatal("HEAD body")
	}
	h.expectError("GET", "/assets/icons/"+strings.Repeat("a", 64)+".svg", nil, 404, codeFileNotFound, nil)
	h.expectError("GET", "/assets/icons/not-an-icon.svg", nil, 404, codeFileNotFound, nil)
	h.expectError("DELETE", icon, nil, 405, codeMethodNotAllowed, nil)
}

func TestHTTPCacheFileErrorsUseTheContract(t *testing.T) {
	h := newHarness(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/files/present.bin":
			w.Header().Set("Cache-Control", "max-age=60")
			_, _ = w.Write([]byte("present"))
		case "/files/forbidden.bin":
			http.Error(w, "no", http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()
	h.createVendor("mirror")
	h.createApp("mirror", "files", "http-cache", map[string]any{"base_url": origin.URL + "/files"})
	h.expectError("GET", "/mirror/files/absent.bin", nil, 404, codeFileNotFound, nil)
	h.expectError("GET", "/mirror/files/forbidden.bin", nil, 502, codeUpstreamUnavailable, nil)
	h.expectError("GET", "/mirror/files/present.bin", nil, 504, codeCacheMiss, map[string]string{"Cache-Control": "only-if-cached"})
	h.request("GET", "/mirror/files/present.bin", nil, 200, nil)
	if body, _ := h.request("GET", "/mirror/files/present.bin", nil, 200, map[string]string{"Cache-Control": "only-if-cached"}); string(body) != "present" {
		t.Fatal(string(body))
	}
	h.expectError("GET", "/mirror/files/present.bin?x=1", nil, 400, codeInvalidPath, nil)
	if !strings.Contains(h.logs.String(), "upstream returned HTTP 403") {
		t.Fatal("upstream failure cause not logged")
	}
}
