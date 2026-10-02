package httpserver

import (
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/auth"
	"github.com/PMExtra/RedApp/internal/site"
	"github.com/PMExtra/RedApp/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSiteSettingsAuthenticationCSRFAndPublicText(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	var password string
	a, err := auth.New(db, false, func(p string) { password = p })
	if err != nil {
		t.Fatal(err)
	}
	token, session, err := a.Login("127.0.0.1", password)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: db, Auth: a}
	request := func(method, path, body string, authenticated, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://internal"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "redapp_session", Value: token})
		}
		if csrf {
			r.Header.Set("X-CSRF-Token", session.CSRF)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	settings := site.Defaults()
	settings.Title.EN = "<svg onload=alert(1)>"
	settings.Disclaimer.EN = "Custom notice"
	body, _ := json.Marshal(settings)
	for _, tc := range []struct {
		method     string
		auth, csrf bool
		status     int
	}{{"GET", false, false, 401}, {"POST", false, false, 401}, {"POST", true, false, 403}, {"POST", true, true, 200}, {"GET", true, false, 200}} {
		w := request(tc.method, "/admin/api/site", string(body), tc.auth, tc.csrf)
		if w.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body)
		}
	}
	for _, body := range []string{`{"title":{"en":""}}`, `{"unknown":true}`, `{"title":{"en":"x","zh-CN":"x"},"subtitle":{"en":false}}`} {
		if w := request("POST", "/admin/api/site", body, true, true); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	w := request("GET", "/api/info", "", false, false)
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
