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
	"net/http"
	"sync"
	"time"
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
	Secure   bool
}

func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func New(db *store.Store, secure bool, bootstrap func(string)) (*Auth, error) {
	a := &Auth{db: db, Secure: secure, sessions: map[[32]byte]Session{}, attempts: map[string]attempt{}}
	e := db.DB.QueryRow("SELECT hash,revision FROM admin WHERE id=1").Scan(&a.hash, &a.revision)
	if e == sql.ErrNoRows {
		password := token()
		hash, e := bcrypt.GenerateFromPassword([]byte(password), 12)
		if e != nil {
			return nil, e
		}
		if _, e = db.DB.Exec("INSERT INTO admin VALUES(1,?,1)", hash); e != nil {
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
func (a *Auth) Login(ip, password string) (string, Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, v := range a.attempts {
		if now.Sub(v.Start) > 5*time.Minute {
			delete(a.attempts, k)
		}
	}
	v := a.attempts[ip]
	if v.Start.IsZero() {
		if len(a.attempts) >= 4096 {
			return "", Session{}, errors.New("Login rate limit exceeded")
		}
		v.Start = now
	}
	v.Count++
	a.attempts[ip] = v
	if v.Count > 10 || len(password) > 72 {
		return "", Session{}, errors.New("Login rate limit exceeded")
	}
	if e := bcrypt.CompareHashAndPassword(a.hash, []byte(password)); e != nil {
		return "", Session{}, errors.New("Login failed")
	}
	for k, s := range a.sessions {
		if now.After(s.Until) {
			delete(a.sessions, k)
		}
	}
	if len(a.sessions) >= 128 {
		return "", Session{}, errors.New("Session limit exceeded")
	}
	t := token()
	s := Session{token(), now.Add(8 * time.Hour), a.revision}
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
	if !ok || time.Now().After(s.Until) || s.Revision != a.revision {
		delete(a.sessions, key)
		return Session{}, false
	}
	return s, true
}
func (a *Auth) CSRF(r *http.Request, s Session) bool {
	return len(r.Header.Get("X-CSRF-Token")) == 64 && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRF)) == 1
}
func (a *Auth) Password(old, next string) error {
	if len(next) < 12 || len(next) > 72 {
		return errors.New("Password must be between 12 and 72 bytes")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if bcrypt.CompareHashAndPassword(a.hash, []byte(old)) != nil {
		return errors.New("Invalid password")
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(next), 12)
	if e != nil {
		return e
	}
	revision := a.revision + 1
	if _, e = a.db.DB.Exec("UPDATE admin SET hash=?,revision=? WHERE id=1", hash, revision); e != nil {
		return e
	}
	a.hash = hash
	a.revision = revision
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
func (a *Auth) Cookie(w http.ResponseWriter, t string, requestSecure ...bool) {
	secure := a.Secure
	if len(requestSecure) > 0 {
		secure = requestSecure[0]
	}
	http.SetCookie(w, &http.Cookie{Name: "redapp_session", Value: t, Path: "/admin", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secure, MaxAge: 8 * 3600})
}
