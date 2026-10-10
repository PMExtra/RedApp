package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/PMExtra/RedApp/internal/store"
	"golang.org/x/crypto/bcrypt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Login rate limiting: each client key (IPv4 address or IPv6 /64 prefix) may make
// attemptLimit attempts per attemptWindow. Independently, a global token bucket
// bounds the total number of password comparisons so rotating keys cannot buy
// unbounded bcrypt work. The per-key table is bounded; when it is full the
// oldest entry is evicted instead of refusing every new client.
const (
	attemptLimit   = 10
	attemptWindow  = 5 * time.Minute
	attemptEntries = 4096
	globalBurst    = 20
	globalRefill   = time.Second
	sessionLimit   = 128
	sessionTTL     = 8 * time.Hour
)

var (
	ErrLoginFailed  = errors.New("Login failed")
	ErrRateLimited  = errors.New("Login rate limit exceeded")
	ErrSessionLimit = errors.New("Session limit exceeded")
)

type Session struct {
	CSRF     string
	Until    time.Time
	Revision int
}
type attempt struct {
	Start time.Time
	Count int
}
type Auth struct {
	mu       sync.Mutex
	db       *store.Store
	hash     []byte
	revision int
	sessions map[[32]byte]Session
	attempts map[string]attempt
	tokens   float64
	refilled time.Time
	// compare and now are replaceable by tests; production uses bcrypt and the wall clock.
	compare func(hash, password []byte) error
	now     func() time.Time
}

func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func New(db *store.Store, bootstrap func(string)) (*Auth, error) {
	a := &Auth{db: db, sessions: map[[32]byte]Session{}, attempts: map[string]attempt{}, tokens: globalBurst, compare: bcrypt.CompareHashAndPassword, now: time.Now}
	e := db.DB.QueryRow("SELECT hash,revision FROM admin WHERE id=1").Scan(&a.hash, &a.revision)
	if e == sql.ErrNoRows {
		password := token()
		hash, e := bcrypt.GenerateFromPassword([]byte(password), 12)
		if e != nil {
			return nil, e
		}
		if _, e = db.DB.Exec("INSERT INTO admin(id,hash,revision) VALUES(1,?,1)", hash); e != nil {
			return nil, e
		}
		a.hash = hash
		a.revision = 1
		bootstrap(password)
	} else if e != nil {
		return nil, e
	}
	return a, nil
}

// clientKey aggregates IPv6 clients by /64, the smallest prefix normally assigned to one site.
func clientKey(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip
	}
	if v4 := parsed.To4(); v4 != nil {
		return v4.String()
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// admit records one attempt for key; the caller holds a.mu.
func (a *Auth) admit(key string, now time.Time) bool {
	v, ok := a.attempts[key]
	if ok && now.Sub(v.Start) > attemptWindow {
		v, ok = attempt{}, false
	}
	if v.Count >= attemptLimit {
		return false
	}
	if a.refilled.IsZero() {
		a.refilled = now
	}
	if elapsed := now.Sub(a.refilled); elapsed > 0 {
		a.tokens += float64(elapsed) / float64(globalRefill)
		if a.tokens > globalBurst {
			a.tokens = globalBurst
		}
	}
	a.refilled = now
	if a.tokens < 1 {
		return false
	}
	a.tokens--
	if !ok {
		if _, exists := a.attempts[key]; !exists && len(a.attempts) >= attemptEntries {
			a.evict(now)
		}
		v.Start = now
	}
	v.Count++
	a.attempts[key] = v
	return true
}

// evict drops expired entries, or the oldest entry when none has expired.
func (a *Auth) evict(now time.Time) {
	oldest, oldestStart := "", time.Time{}
	for k, v := range a.attempts {
		if now.Sub(v.Start) > attemptWindow {
			delete(a.attempts, k)
		} else if oldest == "" || v.Start.Before(oldestStart) {
			oldest, oldestStart = k, v.Start
		}
	}
	if len(a.attempts) >= attemptEntries {
		delete(a.attempts, oldest)
	}
}

// Login checks the password without holding a.mu so Session checks are never
// blocked behind bcrypt. A password change that lands during the comparison
// invalidates the attempt.
func (a *Auth) Login(ip, password string) (string, Session, error) {
	key := clientKey(ip)
	a.mu.Lock()
	if !a.admit(key, a.now()) {
		a.mu.Unlock()
		return "", Session{}, ErrRateLimited
	}
	hash, revision := a.hash, a.revision
	a.mu.Unlock()
	if len(password) > 72 || a.compare(hash, []byte(password)) != nil {
		return "", Session{}, ErrLoginFailed
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if revision != a.revision {
		return "", Session{}, ErrLoginFailed
	}
	now := a.now()
	for k, s := range a.sessions {
		if now.After(s.Until) {
			delete(a.sessions, k)
		}
	}
	if len(a.sessions) >= sessionLimit {
		return "", Session{}, ErrSessionLimit
	}
	delete(a.attempts, key)
	t := token()
	s := Session{token(), now.Add(sessionTTL), a.revision}
	a.sessions[sha256.Sum256([]byte(t))] = s
	return t, s, nil
}
func (a *Auth) Session(r *http.Request) (Session, bool) {
	c, e := r.Cookie("redapp_session")
	if e != nil || len(c.Value) != 64 {
		return Session{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := sha256.Sum256([]byte(c.Value))
	s, ok := a.sessions[key]
	if !ok || a.now().After(s.Until) || s.Revision != a.revision {
		delete(a.sessions, key)
		return Session{}, false
	}
	return s, true
}
func (a *Auth) CSRF(r *http.Request, s Session) bool {
	return len(r.Header.Get("X-CSRF-Token")) == 64 && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRF)) == 1
}

// Password hashes outside a.mu; a concurrent change detected by revision fails this one.
func (a *Auth) Password(old, next string) error {
	if len(next) < 12 || len(next) > 72 {
		return errors.New("Password must be between 12 and 72 bytes")
	}
	a.mu.Lock()
	current, revision := a.hash, a.revision
	a.mu.Unlock()
	if a.compare(current, []byte(old)) != nil {
		return errors.New("Invalid password")
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(next), 12)
	if e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if revision != a.revision {
		return errors.New("Password changed concurrently")
	}
	if _, e = a.db.DB.Exec("UPDATE admin SET hash=?,revision=? WHERE id=1", hash, revision+1); e != nil {
		return e
	}
	a.hash = hash
	a.revision = revision + 1
	a.sessions = map[[32]byte]Session{}
	return nil
}
func (a *Auth) Logout(r *http.Request) {
	if c, e := r.Cookie("redapp_session"); e == nil {
		a.mu.Lock()
		delete(a.sessions, sha256.Sum256([]byte(c.Value)))
		a.mu.Unlock()
	}
}

// Cookie issues the session cookie, or expires it when t is empty.
func (a *Auth) Cookie(w http.ResponseWriter, t string, secure bool) {
	maxAge := int(sessionTTL / time.Second)
	if t == "" {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{Name: "redapp_session", Value: t, Path: "/admin", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secure, MaxAge: maxAge})
}
