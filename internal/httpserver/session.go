package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/auth"
)

type sessionDTO struct {
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func sessionDocument(s auth.Session) sessionDTO {
	return sessionDTO{CSRFToken: s.CSRF, ExpiresAt: s.Until.UTC().Truncate(time.Second)}
}

// secureCookie reports whether the session cookie must carry Secure: the
// verified request origin is HTTPS.
func secureCookie(r *http.Request) bool {
	return strings.HasPrefix(requestState(r).origin, "https://")
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password string `json:"password"`
	}
	if e := decodeJSON(r, &input); e != nil {
		s.writeError(w, r, e)
		return
	}
	if input.Password == "" {
		s.fail(w, r, codeInvalidRequest, nil, "password is required")
		return
	}
	token, session, err := s.auth.Login(s.proxies.ClientIP(r), input.Password)
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		s.fail(w, r, codeLoginRateLimited, nil, "Too many sign-in attempts; try again later")
	case errors.Is(err, auth.ErrSessionLimit):
		s.fail(w, r, codeSessionLimitExceeded, nil, "Too many active sessions; try again later")
	case errors.Is(err, auth.ErrLoginFailed):
		s.fail(w, r, codeLoginFailed, nil, "Wrong administrator password")
	case err != nil:
		s.writeError(w, r, storageError(err))
	default:
		s.auth.Cookie(w, token, secureCookie(r))
		writeCreated(w, "", 0, sessionDocument(session))
	}
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	writeOK(w, sessionDocument(requestState(r).session))
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	s.auth.Logout(r)
	s.auth.Cookie(w, "", secureCookie(r))
	writeNoContent(w)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if e := decodeJSON(r, &input); e != nil {
		s.writeError(w, r, e)
		return
	}
	if input.Current == "" {
		s.fail(w, r, codeInvalidRequest, nil, "current_password is required")
		return
	}
	err := s.auth.Password(s.proxies.ClientIP(r), input.Current, input.New)
	switch {
	case errors.Is(err, auth.ErrPasswordInvalid):
		s.fail(w, r, codePasswordInvalid, nil, "new_password must be 12 to 72 bytes long")
	case errors.Is(err, auth.ErrCurrentPasswordIncorrect):
		s.fail(w, r, codeCurrentPasswordIncorrect, nil, "The current password is wrong")
	case errors.Is(err, auth.ErrRateLimited):
		s.fail(w, r, codeLoginRateLimited, nil, "Too many password attempts; try again later")
	case err != nil:
		s.writeError(w, r, storageError(err))
	default:
		// Every session, including this one, is revoked.
		s.auth.Cookie(w, "", secureCookie(r))
		writeNoContent(w)
	}
}
