package auth

import (
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newAuth(t *testing.T) (*Auth, string) {
	t.Helper()
	db, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.DB.Close() })
	var password string
	a, e := New(db, func(p string) { password = p })
	if e != nil {
		t.Fatal(e)
	}
	return a, password
}

// fastAuth replaces bcrypt with a plain comparison and the wall clock with a manual one.
func fastAuth(t *testing.T) (*Auth, string, *time.Time) {
	a, password := newAuth(t)
	now := time.Unix(1_700_000_000, 0)
	a.now = func() time.Time { return now }
	a.compare = func(_, p []byte) error {
		if string(p) != password {
			return errors.New("mismatch")
		}
		return nil
	}
	return a, password, &now
}

func sessionRequest(t *testing.T, a *Auth, token string) *http.Request {
	t.Helper()
	w := httptest.NewRecorder()
	a.Cookie(w, token, true)
	r := httptest.NewRequest("POST", "https://example/admin", nil)
	r.AddCookie(w.Result().Cookies()[0])
	return r
}

func TestRateLimitCookieAndSession(t *testing.T) {
	a, password := newAuth(t)
	token, s, e := a.Login("192.0.2.1", password)
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	a.Cookie(w, token, true)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].MaxAge != 8*3600 {
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
	a.compare = func(_, p []byte) error { return errors.New("mismatch") }
	for i := 0; i < 10; i++ {
		if _, _, e = a.Login("192.0.2.2", "wrong"); !errors.Is(e, ErrLoginFailed) {
			t.Fatal("错误密码应报告登录失败", i, e)
		}
	}
	if _, _, e = a.Login("192.0.2.2", password); !errors.Is(e, ErrRateLimited) {
		t.Fatal("登录未限速", e)
	}
	a.Logout(r)
	if _, ok := a.Session(r); ok {
		t.Fatal("退出未移除会话")
	}
}

func TestLogoutCookieExpires(t *testing.T) {
	a, _ := newAuth(t)
	w := httptest.NewRecorder()
	a.Cookie(w, "", false)
	c := w.Result().Cookies()
	if len(c) != 1 || c[0].MaxAge >= 0 || c[0].Value != "" || c[0].Path != "/admin" || c[0].Secure {
		t.Fatal(c)
	}
}

func TestIPv6AttemptsAggregateByPrefix(t *testing.T) {
	a, password, _ := fastAuth(t)
	for i := 0; i < 10; i++ {
		a.Login(fmt.Sprintf("2001:db8:1:2::%x", i+1), "wrong")
	}
	if _, _, e := a.Login("2001:db8:1:2:ffff::1", password); !errors.Is(e, ErrRateLimited) {
		t.Fatal("同一 /64 内轮换地址绕过了限速", e)
	}
	if _, _, e := a.Login("2001:db8:1:3::1", password); e != nil {
		t.Fatal("相邻 /64 被误限速", e)
	}
	if clientKey("::ffff:192.0.2.7") != clientKey("192.0.2.7") || clientKey("192.0.2.7") == clientKey("192.0.2.8") {
		t.Fatal("IPv4 键不应聚合")
	}
}

func TestRotatingClientsCannotLockOutFreshClient(t *testing.T) {
	a, password, now := fastAuth(t)
	// Far more distinct /64 prefixes than the table holds, paced at the global refill rate.
	for i := 0; i < attemptEntries+500; i++ {
		*now = now.Add(globalRefill)
		if _, _, e := a.Login(fmt.Sprintf("2001:db8:%x:%x::1", i>>16, i&0xffff), "wrong"); !errors.Is(e, ErrLoginFailed) {
			t.Fatal(i, e)
		}
	}
	if len(a.attempts) > attemptEntries {
		t.Fatal("尝试表无上限", len(a.attempts))
	}
	*now = now.Add(globalRefill)
	if _, _, e := a.Login("198.51.100.9", password); e != nil {
		t.Fatal("新客户端被轮换地址锁定", e)
	}
	// A burst beyond the global budget is refused until tokens refill, regardless of key.
	*now = now.Add(time.Hour)
	for i := 0; i < globalBurst; i++ {
		if _, _, e := a.Login(fmt.Sprintf("203.0.113.%d", i), "wrong"); !errors.Is(e, ErrLoginFailed) {
			t.Fatal(i, e)
		}
	}
	if _, _, e := a.Login("198.51.100.10", password); !errors.Is(e, ErrRateLimited) {
		t.Fatal("全局预算未生效", e)
	}
	*now = now.Add(globalRefill)
	if _, _, e := a.Login("198.51.100.10", password); e != nil {
		t.Fatal("全局预算未恢复", e)
	}
	// Per-key limits expire after the window.
	for i := 0; i < attemptLimit; i++ {
		*now = now.Add(globalRefill)
		a.Login("198.51.100.11", "wrong")
	}
	if _, _, e := a.Login("198.51.100.11", password); !errors.Is(e, ErrRateLimited) {
		t.Fatal(e)
	}
	*now = now.Add(attemptWindow + time.Second)
	if _, _, e := a.Login("198.51.100.11", password); e != nil {
		t.Fatal("限速窗口未过期", e)
	}
}

func TestSessionNotBlockedDuringPasswordCompare(t *testing.T) {
	a, password := newAuth(t)
	token, _, e := a.Login("192.0.2.1", password)
	if e != nil {
		t.Fatal(e)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	a.compare = func(_, _ []byte) error { close(entered); <-release; return errors.New("mismatch") }
	done := make(chan error)
	go func() { _, _, e := a.Login("192.0.2.2", "slow"); done <- e }()
	<-entered
	checked := make(chan bool)
	go func() { _, ok := a.Session(sessionRequest(t, a, token)); checked <- ok }()
	select {
	case ok := <-checked:
		if !ok {
			t.Fatal("会话无效")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("密码比较期间会话检查被阻塞")
	}
	close(release)
	if e := <-done; !errors.Is(e, ErrLoginFailed) {
		t.Fatal(e)
	}
}

func TestPasswordChangeInvalidatesSessionsAndInFlightLogin(t *testing.T) {
	a, password := newAuth(t)
	token, _, e := a.Login("192.0.2.1", password)
	if e != nil {
		t.Fatal(e)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	compare := a.compare
	a.compare = func(h, p []byte) error {
		if string(p) == password+"#slow" {
			close(entered)
			<-release
			return nil
		}
		return compare(h, p)
	}
	done := make(chan error)
	go func() { _, _, e := a.Login("192.0.2.2", password+"#slow"); done <- e }()
	<-entered
	if e = a.Password("192.0.2.1", password, "a new password value"); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e = <-done; !errors.Is(e, ErrLoginFailed) {
		t.Fatal("改密期间进行中的旧密码登录未失效", e)
	}
	if _, ok := a.Session(sessionRequest(t, a, token)); ok {
		t.Fatal("改密后旧会话仍有效")
	}
	if _, _, e = a.Login("192.0.2.1", password); !errors.Is(e, ErrLoginFailed) {
		t.Fatal(e)
	}
	if _, _, e = a.Login("192.0.2.1", "a new password value"); e != nil {
		t.Fatal(e)
	}
}

func TestPasswordChangeRejectsWrongCurrentPasswordUnderTheLoginRateLimit(t *testing.T) {
	a, password, _ := fastAuth(t)
	if e := a.Password("192.0.2.9", password, "short"); !errors.Is(e, ErrPasswordInvalid) {
		t.Fatal("short new password accepted", e)
	}
	for i := 0; i < 10; i++ {
		if e := a.Password("192.0.2.9", "wrong current password", "a new password value"); !errors.Is(e, ErrCurrentPasswordIncorrect) {
			t.Fatal(i, e)
		}
	}
	if e := a.Password("192.0.2.9", password, "a new password value"); !errors.Is(e, ErrRateLimited) {
		t.Fatal("wrong current passwords did not count against the sign-in limit", e)
	}
	if _, _, e := a.Login("192.0.2.9", password); !errors.Is(e, ErrRateLimited) {
		t.Fatal("password attempts and sign-in attempts use separate limits", e)
	}
	if e := a.Password("192.0.2.10", password, "a new password value"); e != nil {
		t.Fatal("another client was limited", e)
	}
}
