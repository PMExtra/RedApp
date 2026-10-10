package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
)

// apiError is one error response: an explicit catalog code, a user-facing
// English message and the underlying cause, which is logged but never sent.
type apiError struct {
	code    errorCode
	message string
	cause   error
}

func (e *apiError) Error() string {
	if e.cause != nil {
		return string(e.code) + ": " + e.message + ": " + e.cause.Error()
	}
	return string(e.code) + ": " + e.message
}
func (e *apiError) Unwrap() error { return e.cause }

// newError builds an error response value. message must not contain internal
// paths, SQL, upstream URLs or credentials; cause carries those details to logs.
func newError(code errorCode, cause error, message string) *apiError {
	return &apiError{code: code, message: message, cause: cause}
}

type errorBody struct {
	Error errorDetail `json:"error"`
}
type errorDetail struct {
	Code      errorCode `json:"code"`
	Message   string    `json:"message"`
	RequestID string    `json:"request_id"`
	Retryable bool      `json:"retryable"`
}

// writeError writes the standard Error document. Status and retryable come
// only from the catalog; an unknown code is a programming error and becomes
// INTERNAL_ERROR so the response stays within the contract.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, e *apiError) {
	class, ok := errorCatalog[e.code]
	if !ok {
		e = &apiError{code: codeInternalError, message: "Internal server error", cause: fmt.Errorf("unknown error code %q: %w", e.code, e)}
		class = errorCatalog[codeInternalError]
	}
	info := requestState(r)
	info.errorCode = e.code
	level := slog.LevelInfo
	if class.status >= 500 {
		level = slog.LevelError
	}
	if e.cause != nil || class.status >= 500 {
		attrs := []slog.Attr{slog.String("request_id", info.id), slog.String("code", string(e.code)), slog.Int("status", class.status)}
		if info.route != nil {
			attrs = append(attrs, slog.String("operation", info.route.operation))
		}
		if e.cause != nil {
			attrs = append(attrs, slog.String("error", redactError(e.cause)))
		}
		s.log.LogAttrs(r.Context(), level, "request failed", attrs...)
	}
	h := w.Header()
	// Error documents never inherit file headers set before the failure.
	for _, key := range []string{"Content-Disposition", "Content-Length", "Content-Range", "ETag", "Last-Modified", "Accept-Ranges", "X-Expected-SHA256", "Content-Security-Policy"} {
		h.Del(key)
	}
	h.Set("Cache-Control", "no-store")
	writeJSON(w, class.status, errorBody{Error: errorDetail{Code: e.code, Message: e.message, RequestID: info.id, Retryable: class.retryable}})
}

// fail is the short form for handlers: writeError(newError(...)).
func (s *Server) fail(w http.ResponseWriter, r *http.Request, code errorCode, cause error, message string) {
	s.writeError(w, r, newError(code, cause, message))
}

// failWith writes err when it is an *apiError and otherwise an internal error.
func (s *Server) failWith(w http.ResponseWriter, r *http.Request, err error) {
	var e *apiError
	if errors.As(err, &e) {
		s.writeError(w, r, e)
		return
	}
	s.fail(w, r, codeInternalError, err, "Internal server error")
}

// storageError is the response for a database or data directory failure.
func storageError(cause error) *apiError {
	return newError(codeStorageUnavailable, cause, "Local storage is unavailable; retry later")
}

var credentialURL = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/@\s]*@`)

// redactError removes URL user information (proxy and upstream credentials)
// from an error before it is logged.
func redactError(err error) string {
	return credentialURL.ReplaceAllString(err.Error(), "${1}****@")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		// Response values are server-built DTOs; a marshal failure is a bug.
		panic(fmt.Errorf("encode response: %w", err))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
