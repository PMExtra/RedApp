package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/site"
)

func TestSiteSettingsAuthenticationCSRFAndPublicText(t *testing.T) {
	h := newHarness(t)
	token, session, err := h.server.auth.Login("127.0.0.1", h.password)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, authenticated, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://internal"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", "0")
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "redapp_session", Value: token})
		}
		if csrf {
			r.Header.Set("X-CSRF-Token", session.CSRF)
		}
		return h.serve(r)
	}
	settings := site.Defaults()
	settings.Title.EN = "<svg onload=alert(1)>"
	settings.Disclaimer.EN = "Custom notice"
	body, _ := json.Marshal(settings)
	for _, tc := range []struct {
		method     string
		auth, csrf bool
		status     int
	}{{"GET", false, false, 401}, {"PUT", false, false, 401}, {"PUT", true, false, 403}, {"PUT", true, true, 200}, {"GET", true, false, 200}} {
		w := request(tc.method, "/admin/api/settings/site", string(body), tc.auth, tc.csrf)
		if w.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body)
		}
	}
	for _, body := range []string{`{"title":{"en":""}}`, `{"unknown":true}`, `{"title":{"en":"x","zh-CN":"x"},"subtitle":{"en":false}}`} {
		if w := request("PUT", "/admin/api/settings/site", body, true, true); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	w := request("GET", "/api/bootstrap", "", false, false)
	var info struct {
		Site site.Settings `json:"site"`
	}
	if json.Unmarshal(w.Body.Bytes(), &info) != nil || info.Site != settings {
		t.Fatal(w.Body)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), "<svg") {
		t.Fatal("unsafe public settings response")
	}
}
