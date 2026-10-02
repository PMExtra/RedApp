package httpserver

import (
	"embed"
	"encoding/json"
	"errors"
	"github.com/PMExtra/RedApp/installers/codex"
	claude "github.com/PMExtra/RedApp/internal/apps/claude"
	app "github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/auth"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/history"
	"github.com/PMExtra/RedApp/internal/site"
	"github.com/PMExtra/RedApp/internal/store"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

//go:embed web/*
var web embed.FS

type Server struct {
	Version   string
	DB        *store.Store
	Catalog   *app.Catalog
	Claude    *claude.Catalog
	Downloads *download.Manager
	Auth      *auth.Auth
	Proxy     Proxy
	Upstream  *distributor.Client
	History   *history.History
	Public    string
	Dir       string
	Started   time.Time
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	reply(w, status, map[string]string{"error": msg})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("JSON request required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var tail any
	if e := d.Decode(&tail); e != io.EOF {
		return errors.New("Unexpected trailing request data")
	}
	return nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	public, err := s.origin(r)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if r.URL.RawPath != "" || r.URL.RawQuery != "" || strings.Contains(r.URL.Path, "\\") || strings.Contains(r.URL.Path, "//") {
		fail(w, 400, "Noncanonical request path")
		return
	}
	if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
		if r.Method != "GET" {
			fail(w, 405, "Method not allowed")
			return
		}
		if r.URL.Path == "/health/ready" {
			if e := s.DB.DB.PingContext(r.Context()); e != nil {
				fail(w, 503, "Local storage is not ready")
				return
			}
			f, e := os.CreateTemp(s.Dir, ".health-")
			if e != nil {
				fail(w, 503, "Data directory is not writable")
				return
			}
			name := f.Name()
			f.Close()
			os.Remove(name)
		}
		reply(w, 200, map[string]bool{"ok": true})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/admin") {
		s.admin(w, r, public)
		return
	}
	if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/apps/") || strings.HasPrefix(r.URL.Path, "/api/") {
		s.publicPage(w, r, public)
		return
	}
	s.DB.Add("requests", 1)
	if r.Method != "GET" {
		fail(w, 405, "Method not allowed")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/claude-code/") {
		s.claudeDownload(w, r, public)
		return
	}
	if r.URL.Path == "/install.sh" || r.URL.Path == "/install.ps1" {
		body, e := codex.Installer(strings.TrimPrefix(r.URL.Path, "/"), public)
		if e != nil {
			fail(w, 503, "Installer generation verification has not passed")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(body)
		return
	}
	if r.URL.Path == "/licenses/LICENSE" || r.URL.Path == "/licenses/NOTICE" {
		b, e := codex.License(strings.TrimPrefix(r.URL.Path, "/licenses/"))
		if e != nil {
			fail(w, 404, "License file not found")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(b)
		return
	}
	v := "latest"
	isMetadata := r.URL.Path == "/channels/latest"
	name := ""
	if !isMetadata {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) != 4 || parts[1] != "releases" {
			fail(w, 404, "Route not found")
			return
		}
		var e error
		v, e = app.Normalize(parts[2])
		if e != nil || v != parts[2] {
			fail(w, 400, "Version must use canonical form")
			return
		}
		name = parts[3]
		isMetadata = name == "release.json"
	}
	if isMetadata {
		m, e := s.Catalog.Get(r.Context(), v)
		if e != nil {
			fail(w, 502, "Failed to fetch trusted metadata")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		reply(w, 200, m.Public(public))
		return
	}
	resource, e := s.Catalog.Authorize(r.Context(), v, name)
	if e != nil {
		fail(w, 404, "Resource is not authorized or metadata is unavailable")
		return
	}
	s.serveResource(w, r, resource)
}
func (s *Server) serveResource(w http.ResponseWriter, r *http.Request, resource download.Resource) {
	counterPrefix := "version:" + resource.Labels["version"]
	if resource.Labels["app"] != "codex" {
		counterPrefix = "app:" + resource.Labels["app"] + ":" + counterPrefix
	}
	s.DB.Add("artifact_requests", 1)
	s.DB.Add(counterPrefix+":requests", 1)
	rd, hit, e := s.Downloads.Acquire(r.Context(), resource)
	if e != nil {
		fail(w, 503, "Download capacity exceeded or local storage unavailable")
		return
	}
	defer rd.Close()
	s.DB.Add(rd.Kind+"_requests", 1)
	if hit {
		s.DB.Add("reuse_requests", 1)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Expected-SHA256", resource.Hash)
	buf := make([]byte, 32<<10)
	for {
		n, re := rd.Read(buf)
		if n > 0 {
			written, we := w.Write(buf[:n])
			s.DB.Add("downstream_bytes", int64(written))
			s.DB.Add(counterPrefix+":downstream_bytes", int64(written))
			if we != nil {
				s.DB.Add("download_errors", 1)
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if re != nil {
			if re == io.EOF {
				s.DB.Add("download_success", 1)
				return
			}
			s.DB.Add("download_errors", 1)
			panic(http.ErrAbortHandler)
		}
	}
}
func (s *Server) sameOrigin(r *http.Request, public string) bool {
	return r.Header.Get("Origin") == "" || r.Header.Get("Origin") == public
}
func (s *Server) admin(w http.ResponseWriter, r *http.Request, public string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	if !s.sameOrigin(r, public) {
		fail(w, 403, "Origin not allowed")
		return
	}
	if r.URL.Path == "/admin/api/login" {
		if r.Method != "POST" {
			fail(w, 405, "Method not allowed")
			return
		}
		var input struct {
			Password string `json:"password"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid request")
			return
		}
		token, session, e := s.Auth.Login(s.Proxy.ClientIP(r), input.Password)
		if e != nil {
			fail(w, 429, "Login failed or rate limit exceeded")
			return
		}
		s.Auth.Cookie(w, token, strings.HasPrefix(public, "https://"))
		reply(w, 200, map[string]string{"csrf": session.CSRF})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/admin/api/") {
		if r.Method != "GET" {
			fail(w, 405, "Method not allowed")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/admin/")
		if path == "/admin" || path == "" {
			path = "index.html"
		}
		asset := strings.TrimPrefix(path, "assets/")
		bundled := strings.HasPrefix(path, "assets/") && !strings.Contains(asset, "/") && (strings.HasSuffix(asset, ".js") || strings.HasSuffix(asset, ".css"))
		if path != "index.html" && !bundled {
			fail(w, 404, "Page not found")
			return
		}
		assets, _ := fs.Sub(web, "web")
		b, e := fs.ReadFile(assets, path)
		if e != nil {
			fail(w, 404, "Page not found")
			return
		}
		contentType := map[string]string{".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8"}
		w.Header().Set("Content-Type", contentType[filepath.Ext(path)])
		w.Write(b)
		return
	}
	session, ok := s.Auth.Session(r)
	if !ok {
		fail(w, 401, "Sign in required")
		return
	}
	if r.Method != "GET" && !s.Auth.CSRF(r, session) {
		fail(w, 403, "CSRF validation failed")
		return
	}
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/admin/api/site":
			settings, e := site.Load(s.DB)
			if e != nil {
				fail(w, 503, "Site settings are unavailable")
				return
			}
			reply(w, 200, settings)
		case "/admin/api/settings":
			application, ok := s.application(r.Header.Get("X-RedApp-Application"))
			if !ok {
				fail(w, 400, "Unknown application")
				return
			}
			ttl := s.Catalog.LatestTTLSeconds()
			if application == claude.ID {
				ttl = s.Claude.LatestTTLSeconds()
			}
			reply(w, 200, map[string]int{"latest_ttl_seconds": ttl})
		case "/admin/api/session":
			reply(w, 200, map[string]string{"csrf": session.CSRF})
		case "/admin/api/history":
			if s.History == nil {
				fail(w, 503, "Metric history is unavailable")
				return
			}
			metric, window := r.Header.Get("X-History-Metric"), r.Header.Get("X-History-Range")
			series, err := s.History.Query(metric, window, time.Now().UTC())
			if err != nil {
				if _, ok := history.Find(metric); !ok || (window != "24h" && window != "7d" && window != "30d") {
					fail(w, 400, "Invalid history metric or range")
				} else {
					fail(w, 503, "Failed to read metric history")
				}
				return
			}
			reply(w, 200, series)
		case "/admin/api/proxy":
			if s.Upstream == nil {
				fail(w, 503, "Upstream proxy settings are unavailable")
				return
			}
			reply(w, 200, s.Upstream.Proxy())
		case "/admin/api/status":
			status, e := s.status(public)
			if e != nil {
				fail(w, 503, "Failed to read status")
				return
			}
			reply(w, 200, status)
		default:
			fail(w, 404, "API endpoint not found")
		}
		return
	}
	if r.Method != "POST" {
		fail(w, 405, "Method not allowed")
		return
	}
	switch r.URL.Path {
	case "/admin/api/logout":
		s.Auth.Logout(r)
		s.Auth.Cookie(w, "", strings.HasPrefix(public, "https://"))
		reply(w, 200, map[string]bool{"ok": true})
	case "/admin/api/password":
		var input struct {
			Old string `json:"old"`
			New string `json:"new"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid request")
			return
		}
		if e := s.Auth.Password(input.Old, input.New); e != nil {
			fail(w, 400, "Password change failed; check the current password and new password length")
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	case "/admin/api/proxy":
		if s.Upstream == nil {
			fail(w, 503, "Upstream proxy settings are unavailable")
			return
		}
		var input distributor.ProxyUpdate
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid request")
			return
		}
		if err := s.Upstream.SetProxy(input); err != nil {
			fail(w, 400, err.Error())
			return
		}
		reply(w, 200, s.Upstream.Proxy())
	case "/admin/api/site":
		var input site.Settings
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid site settings")
			return
		}
		if err := input.Validate(); err != nil {
			fail(w, 400, "Invalid site settings")
			return
		}
		if err := site.Save(s.DB, input); err != nil {
			fail(w, 503, "Unable to save site settings")
			return
		}
		reply(w, 200, input)
	case "/admin/api/settings":
		var input struct {
			TTL int `json:"latest_ttl_seconds"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid request")
			return
		}
		application, ok := s.application(r.Header.Get("X-RedApp-Application"))
		if !ok {
			fail(w, 400, "Unknown application")
			return
		}
		var e error
		if application == claude.ID {
			e = s.Claude.SetTTL(input.TTL)
		} else {
			e = s.Catalog.SetTTL(input.TTL)
		}
		if e != nil {
			fail(w, 400, "TTL must be between 1 and 86400 seconds")
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	case "/admin/api/cleanup/preview":
		var input struct {
			Minimum string `json:"minimum_version"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid request")
			return
		}
		views := s.Downloads.Snapshot()
		application, ok := s.application(r.Header.Get("X-RedApp-Application"))
		if !ok {
			fail(w, 400, "Unknown application")
			return
		}
		var ids map[string]bool
		var unknown []string
		var e error
		if application == claude.ID {
			ids, unknown, e = s.Claude.Candidates(input.Minimum, views)
		} else {
			ids, unknown, e = s.Catalog.Candidates(input.Minimum, views)
		}
		if e != nil {
			fail(w, 400, "Invalid minimum version")
			return
		}
		job, e := s.Downloads.Preview(ids)
		if e != nil {
			fail(w, 503, "Failed to persist cleanup preview")
			return
		}
		var size int64
		active := 0
		for _, v := range views {
			for _, selected := range job.Selected {
				if v.ID == selected.Generation {
					size += v.Bytes
					if v.State != "complete" {
						active++
					}
				}
			}
		}
		reply(w, 200, map[string]any{"job": job, "logical_bytes": size, "active": active, "unknown_versions": unknown})
	case "/admin/api/cleanup/execute":
		var input struct {
			ID string `json:"cleanup_id"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid request")
			return
		}
		if e := s.Downloads.Cleanup(input.ID); e != nil {
			fail(w, 409, "Cleanup task is invalid or execution failed")
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	default:
		fail(w, 404, "API endpoint not found")
	}
}
func (s *Server) status(public string) (map[string]any, error) {
	views := s.Downloads.Snapshot()
	var complete, temp, pending, total, logical, allocatedCache, allocatedTemp, allocatedPending int64
	classes := map[string]string{}
	for _, v := range views {
		if v.Retired {
			classes[v.Path] = "pending"
			pending += v.Bytes
		} else if v.State == "complete" {
			classes[v.Path] = "cache"
			complete += v.Bytes
		} else {
			classes[v.Path] = "temporary"
			temp += v.Bytes
		}
	}
	e := filepath.WalkDir(s.Dir, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		allocated := info.Size()
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			allocated = st.Blocks * 512
		}
		total += allocated
		if !d.IsDir() {
			logical += info.Size()
		}
		switch classes[path] {
		case "cache":
			allocatedCache += allocated
		case "temporary":
			allocatedTemp += allocated
		case "pending":
			allocatedPending += allocated
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	var disk syscall.Statfs_t
	if e = syscall.Statfs(s.Dir, &disk); e != nil {
		return nil, e
	}
	versions, e := s.DB.Versions()
	if e != nil {
		return nil, e
	}
	events, e := s.DB.Events()
	if e != nil {
		return nil, e
	}
	counters, e := s.DB.Counters()
	if e != nil {
		return nil, e
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	status := map[string]any{"name": "RedApp", "started": s.Started, "os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version(), "goroutines": runtime.NumGoroutine(), "memory_bytes": mem.Alloc, "sampled_at": time.Now().UTC(), "resources": views, "versions": versions, "events": events, "counters": counters, "disk": map[string]any{"used_bytes": total, "logical_bytes": logical, "allocated_cache_bytes": allocatedCache, "allocated_temporary_bytes": allocatedTemp, "allocated_pending_bytes": allocatedPending, "cache_bytes": complete, "temporary_bytes": temp, "pending_bytes": pending, "other_bytes": total - allocatedCache - allocatedTemp - allocatedPending, "free_bytes": disk.Bavail * uint64(disk.Bsize)}, "public_base_url": public, "rates": s.DB.Rates(), "client_runtime_update_policy": "The enterprise installer suppresses the automatic-update marker; the CLI binary is unchanged. Control runtime public update checks through enterprise egress policy."}
	claudeVersions, e := s.DB.VersionsFor(claude.ID)
	if e != nil {
		return nil, e
	}
	status["application_versions"] = map[string]map[string]string{"codex": versions, claude.ID: claudeVersions}
	status["metrics"] = globalMetrics(status, s.Started)
	return status, nil
}
