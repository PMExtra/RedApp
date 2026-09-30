package auth

import (
	"github.com/PMExtra/RedApp/internal/store"
	"net/http/httptest"
	"testing"
)

func TestRateLimitCookieAndSession(t *testing.T) {
	db, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	var password string
	a, e := New(db, true, func(p string) { password = p })
	if e != nil {
		t.Fatal(e)
	}
	token, s, e := a.Login("test-ip", password)
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	a.Cookie(w, token)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal(cookies)
	}
	r := httptest.NewRequest("POST", "https://example/admin", nil)
	r.AddCookie(cookies[0])
	if _, ok := a.Session(r); !ok {
		t.Fatal("会话无效")
	}
	r.Header.Set("X-CSRF-Token", s.CSRF)
	if !a.CSRF(r, s) {
		t.Fatal("CSRF token 不可用")
	}
	for i := 0; i < 10; i++ {
		a.Login("limited-ip", "wrong")
	}
	if _, _, e = a.Login("limited-ip", password); e == nil {
		t.Fatal("登录未限速")
	}
	a.Logout(r)
	if _, ok := a.Session(r); ok {
		t.Fatal("退出未移除会话")
	}
}
