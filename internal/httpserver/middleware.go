package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/auth"
)

// requestInfo is the request-scoped metadata shared by the middleware, the
// error writer and the access log. It never carries business data.
type requestInfo struct {
	id        string
	origin    string // effective request origin (scheme://host)
	route     *route
	session   auth.Session
	errorCode errorCode
}

type requestKey struct{}

// requestState returns the request metadata installed by ServeHTTP. Handlers
// invoked outside ServeHTTP (never in production) get a fresh value.
func requestState(r *http.Request) *requestInfo {
	if info, ok := r.Context().Value(requestKey{}).(*requestInfo); ok {
		return info
	}
	return &requestInfo{id: "0000000000000000"}
}

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// statusWriter records the response status and size for the access log and
// the panic handler. Unwrap keeps http.ResponseController working.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}
func (w *statusWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ServeHTTP is the middleware chain shared by every route:
//
//  1. request ID (X-Request-Id, context, logs) and the access log
//  2. panic recovery (INTERNAL_ERROR, or an aborted stream after headers)
//  3. default security headers
//  4. request origin from TLS, Host and trusted forwarding headers (REQUEST_ORIGIN_INVALID)
//  5. canonical path (INVALID_PATH)
//  6. ServeMux routing (NOT_FOUND / METHOD_NOT_ALLOWED), then the per-route
//     chain in route.handler: admin Origin, session, CSRF, query allow-list,
//     body limit, handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	info := &requestInfo{id: newRequestID()}
	r = r.WithContext(context.WithValue(r.Context(), requestKey{}, info))
	sw := &statusWriter{ResponseWriter: w}
	defer s.accessLog(sw, r, info, started)
	defer s.recoverPanic(sw, r, info)

	h := sw.Header()
	h.Set("X-Request-Id", info.id)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Cache-Control", "no-store")

	origin, err := s.proxies.origin(r)
	if err != nil {
		s.fail(sw, r, codeRequestOriginInvalid, err, "Invalid Host or forwarding headers")
		return
	}
	info.origin = origin
	if !canonicalPath(r) {
		s.fail(sw, r, codeInvalidPath, nil, "Non-canonical request path")
		return
	}
	if _, pattern := s.mux.Handler(r); pattern == "" {
		s.routerError(sw, r)
		return
	}
	s.mux.ServeHTTP(sw, r)
}

// routerError converts ServeMux's own 404/405 responses into Error documents.
func (s *Server) routerError(w http.ResponseWriter, r *http.Request) {
	handler, _ := s.mux.Handler(r)
	probe := &routerProbe{header: http.Header{}}
	handler.ServeHTTP(probe, r)
	if probe.status == http.StatusMethodNotAllowed {
		if allow := probe.header.Get("Allow"); allow != "" {
			w.Header().Set("Allow", allow)
		}
		s.fail(w, r, codeMethodNotAllowed, nil, "Method not allowed for this path")
		return
	}
	s.fail(w, r, codeNotFound, nil, "No route matches this path")
}

// routerProbe captures what ServeMux would answer for an unmatched request.
type routerProbe struct {
	header http.Header
	status int
}

func (p *routerProbe) Header() http.Header { return p.header }
func (p *routerProbe) WriteHeader(status int) {
	if p.status == 0 {
		p.status = status
	}
}
func (p *routerProbe) Write(b []byte) (int, error) {
	p.WriteHeader(http.StatusOK)
	return len(b), nil
}

func (s *Server) recoverPanic(w *statusWriter, r *http.Request, info *requestInfo) {
	recovered := recover()
	if recovered == nil {
		return
	}
	if recovered == http.ErrAbortHandler {
		// Deliberate abort of a failed stream: net/http resets the connection.
		panic(recovered)
	}
	s.log.Error("handler panic", slog.String("request_id", info.id), slog.String("panic", fmt.Sprint(recovered)), slog.String("stack", string(debug.Stack())))
	if w.status != 0 {
		// Headers are out; truncating silently would look like success.
		panic(http.ErrAbortHandler)
	}
	s.fail(w, r, codeInternalError, nil, "Internal server error")
}

// accessLog writes one line per request. The query string is omitted because
// it can carry cursors and transfer IDs; credentials never appear in paths.
func (s *Server) accessLog(w *statusWriter, r *http.Request, info *requestInfo, started time.Time) {
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	attrs := []slog.Attr{
		slog.String("request_id", info.id),
		slog.String("method", r.Method),
		slog.String("path", r.URL.EscapedPath()),
		slog.Int("status", status),
		slog.Int64("bytes", w.bytes),
		slog.Int64("duration_ms", time.Since(started).Milliseconds()),
		slog.String("client", s.proxies.ClientIP(r)),
	}
	if info.route != nil {
		attrs = append(attrs, slog.String("operation", info.route.operation))
	}
	if info.errorCode != "" {
		attrs = append(attrs, slog.String("code", string(info.errorCode)))
	}
	if errors.Is(r.Context().Err(), context.Canceled) {
		attrs = append(attrs, slog.Bool("client_cancelled", true))
	}
	s.log.LogAttrs(context.Background(), slog.LevelInfo, "http request", attrs...)
}

// canonicalPath rejects dot segments, empty segments, backslashes, NUL and
// encoded separators. Other percent-escapes are allowed: file routes carry
// arbitrary escaped names.
func canonicalPath(r *http.Request) bool {
	p := r.URL.Path
	if p == "" || p[0] != '/' || strings.ContainsAny(p, "\\\x00") || strings.Contains(p, "//") {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	escaped := strings.ToLower(r.URL.EscapedPath())
	return !strings.Contains(escaped, "%2f") && !strings.Contains(escaped, "%5c")
}

// handler builds the per-route chain: admin Origin check, session, CSRF,
// query allow-list and body limit, in that order.
func (s *Server) handler(rt *route) http.Handler {
	admin := strings.HasPrefix(rt.path, "/admin/api/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := requestState(r)
		info.route = rt
		if admin {
			if origin := r.Header.Get("Origin"); origin != "" && origin != info.origin {
				s.fail(w, r, codeOriginRejected, nil, "Origin is not allowed")
				return
			}
		}
		if rt.auth == authAdmin {
			session, ok := s.auth.Session(r)
			if !ok {
				s.fail(w, r, codeAuthRequired, nil, "Sign in required")
				return
			}
			info.session = session
			if unsafeMethod(r.Method) && !s.auth.CSRF(r, session) {
				s.fail(w, r, codeCSRFRejected, nil, "CSRF validation failed")
				return
			}
		}
		if rt.legacy {
			// Pre-contract handlers validate their own queries and bodies.
			rt.serve(w, r)
			return
		}
		if !rt.handlerChecksQuery {
			if e := checkQuery(r, rt.query); e != nil {
				s.writeError(w, r, e)
				return
			}
		}
		if rt.maxBody > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, rt.maxBody)
		}
		rt.serve(w, r)
	})
}

func unsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}
