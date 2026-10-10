package httpserver

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PMExtra/RedApp/installers"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/auth"
	"github.com/PMExtra/RedApp/internal/catalog"
	"github.com/PMExtra/RedApp/internal/config"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/history"
	"github.com/PMExtra/RedApp/internal/hosted"
	"github.com/PMExtra/RedApp/internal/httpcache"
	"github.com/PMExtra/RedApp/internal/jsoncheck"
	"github.com/PMExtra/RedApp/internal/media"
	"github.com/PMExtra/RedApp/internal/prewarm"
	"github.com/PMExtra/RedApp/internal/releasemaintenance"
	"github.com/PMExtra/RedApp/internal/site"
	"github.com/PMExtra/RedApp/internal/store"
)

//go:embed web/*
var web embed.FS

type Server struct {
	Version   string
	DB        *store.Store
	Registry  *application.Registry
	Catalog   *catalog.Service
	Downloads *download.Manager
	HTTPCache *httpcache.Service
	Hosted    *hosted.Service
	Auth      *auth.Auth
	Proxy     Proxy
	Upstream  interface {
		Proxy() distributor.ProxyView
		SetProxy(distributor.ProxyUpdate, int64) error
	}
	Pool                     *distributor.Pool
	Icons                    *media.Store
	directoryMu              sync.Mutex
	configurationOnce        sync.Once
	exchangeMu               sync.Mutex
	exchangePreviews         map[string]exchangePreview
	retention                atomic.Pointer[releasemaintenance.Service]
	prewarmOnce              sync.Once
	prewarmer                *prewarm.Service
	prewarmErr               error
	testConfigurationPrepare func(store.DirectorySnapshot) error
	deleteWait               time.Duration
	History                  *history.History
	PublicConfig             *config.PublicSettings
	Dir                      string
	Started                  time.Time
}

func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, code, message string) {
	var id [8]byte
	_, _ = rand.Read(id[:])
	reply(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "request_id": hex.EncodeToString(id[:]), "retryable": status >= 500 || code == "DIRECTORY_DELETE_PENDING"}})
}
func fail(w http.ResponseWriter, status int, message string) {
	code := map[int]string{400: "INVALID_REQUEST", 401: "AUTH_REQUIRED", 403: "CSRF_REJECTED", 404: "RESOURCE_NOT_FOUND", 405: "METHOD_NOT_ALLOWED", 409: "SETTINGS_REVISION_CONFLICT", 413: "PAYLOAD_TOO_LARGE", 502: "UPSTREAM_UNAVAILABLE", 503: "LOCAL_STORAGE_UNAVAILABLE"}[status]
	problem(w, status, code, message)
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	return decodeLimit(w, r, v, 8192)
}
func decodeLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("JSON request required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if err = jsoncheck.Unique(b); err != nil {
		return err
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		return err
	}
	var tail any
	if err = d.Decode(&tail); err != io.EOF {
		return errors.New("Unexpected trailing request data")
	}
	return nil
}
func canonicalPath(r *http.Request) bool {
	if r.URL.RawPath != "" || strings.ContainsAny(r.URL.Path, "\\\x00") || strings.Contains(r.URL.Path, "//") {
		return false
	}
	for _, segment := range strings.Split(r.URL.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
func queryAllowed(r *http.Request, allowed ...string) bool {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return false
	}
	for key, values := range q {
		found := false
		for _, k := range allowed {
			if key == k {
				found = true
				break
			}
		}
		if !found || len(values) != 1 {
			return false
		}
	}
	return true
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.ConfigurePublication()
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-store")
	requestOrigin, err := s.origin(r)
	if err != nil {
		problem(w, 400, "ORIGIN_REJECTED", err.Error())
		return
	}
	if s.hostedFile(w, r) {
		return
	}
	if s.generalFile(w, r) {
		return
	}
	if !canonicalPath(r) {
		problem(w, 400, "INVALID_PATH", "Noncanonical request path")
		return
	}
	publicView := config.PublicView{EffectiveURL: requestOrigin, Source: "request"}
	if s.PublicConfig != nil {
		publicView = s.PublicConfig.View(requestOrigin)
	}
	public := publicView.EffectiveURL
	path := r.URL.Path
	if path == "/health/live" || path == "/health/ready" {
		if r.Method != "GET" {
			fail(w, 405, "Method not allowed")
			return
		}
		if !queryAllowed(r) {
			fail(w, 400, "Invalid query")
			return
		}
		if path == "/health/ready" {
			if s.DB == nil || s.DB.DB.PingContext(r.Context()) != nil {
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
	if strings.HasPrefix(path, "/admin/api/") {
		s.admin(w, r, requestOrigin, public)
		return
	}
	if strings.HasPrefix(path, "/api/") {
		s.publicAPI(w, r, publicView)
		return
	}
	if strings.HasPrefix(path, "/assets/") {
		if r.Method != "GET" && !(r.Method == "HEAD" && (strings.HasPrefix(path, "/assets/icons/") || strings.HasPrefix(path, "/assets/builtin/") || strings.HasPrefix(path, "/assets/presets/"))) {
			fail(w, 405, "Method not allowed")
			return
		}
		if !queryAllowed(r) {
			fail(w, 400, "Invalid query")
			return
		}
		if strings.HasPrefix(path, "/assets/icons/") {
			s.icon(w, r)
		} else {
			s.asset(w, r)
		}
		return
	}
	if path == "/admin" {
		if r.Method != "GET" {
			fail(w, 405, "Method not allowed")
			return
		}
		if !queryAllowed(r) {
			fail(w, 400, "Invalid query")
			return
		}
		http.Redirect(w, r, "/admin/overview", http.StatusTemporaryRedirect)
		return
	}
	if path == "/all" || path == "/" || strings.HasPrefix(path, "/admin/") {
		if r.Method != "GET" {
			fail(w, 405, "Method not allowed")
			return
		}
		if !s.validUI(path) {
			s.page(w, 404)
			return
		}
		if !queryAllowed(r, "returnTo", "q", "state", "page") {
			fail(w, 400, "Invalid query")
			return
		}
		s.page(w, 200)
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) < 2 {
		v, err := s.DB.Vendor(parts[0])
		if err == nil && v.Enabled && v.DeletedAt == nil {
			if r.Method != "GET" {
				fail(w, 405, "Method not allowed")
			} else if !queryAllowed(r, "q", "page") {
				fail(w, 400, "Invalid query")
			} else {
				s.page(w, 200)
			}
			return
		}
		fail(w, 404, "Route not found")
		return
	}
	id := parts[0] + "/" + parts[1]
	if _, err := application.ParseKey(id); err != nil {
		problem(w, 400, "INVALID_PATH", "Invalid application identity")
		return
	}
	entry, ok := s.Registry.Lookup(id)
	if !ok {
		problem(w, 404, "APPLICATION_NOT_FOUND", "Application not found")
		return
	}
	if !queryAllowed(r) {
		problem(w, 400, "INVALID_QUERY", "Unexpected query parameters")
		return
	}
	if r.Method != "GET" {
		fail(w, 405, "Method not allowed")
		return
	}
	if len(parts) == 2 {
		s.page(w, 200)
		return
	}
	if len(parts) == 3 && parts[2] == "" {
		http.Redirect(w, r, "/"+id, http.StatusPermanentRedirect)
		return
	}
	op, err := entry.ParsePath(strings.Join(parts[2:], "/"))
	if err != nil {
		fail(w, 404, "Resource not found")
		return
	}
	if err = s.DB.Add("requests", 1); err != nil {
		fail(w, 503, "Failed to record request")
		return
	}
	r, finish, err := s.applicationResponse(w, r, entry.StorageID())
	if err != nil {
		problem(w, 404, "APPLICATION_NOT_FOUND", "Application not found")
		return
	}
	defer finish()
	appRoot := public + "/" + id
	switch op.Kind {
	case application.InstallerOperation:
		template := entry.TemplateID
		if template == "" {
			template = id
		}
		body, e := installers.Render(template, id, op.Name, appRoot)
		if e != nil {
			fail(w, 503, "Installer verification failed")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(body)
	case application.StaticOperation:
		if asset, found := entry.PublicAsset(op.Name); found {
			w.Header().Set("Content-Type", asset.ContentType)
			w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
			w.Write(asset.Body)
			return
		}
		template := entry.TemplateID
		if template == "" {
			template = id
		}
		body, e := installers.PublicAsset(template, op.Name)
		if e != nil {
			fail(w, 404, "Public asset not found")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(body)
	case application.ChannelOperation, application.MetadataOperation:
		rep, e := s.Catalog.Represent(r.Context(), id, op, appRoot)
		if e != nil {
			s.catalogError(w, e)
			return
		}
		w.Header().Set("Content-Type", rep.ContentType)
		w.Write(rep.Body)
	case application.ArtifactOperation:
		resource, e := s.Catalog.Authorize(r.Context(), id, op.Target, op.Resource)
		if e != nil {
			s.catalogError(w, e)
			return
		}
		receipt := &downloadReceipt{ResponseWriter: w}
		s.serveResource(receipt, r, resource)
		s.finishDownload(receipt, r, entry.UID)
	default:
		fail(w, 404, "Resource not found")
	}
}
func (s *Server) catalogError(w http.ResponseWriter, err error) {
	if errors.Is(err, application.ErrNotFound) {
		fail(w, 404, "Resource not found")
	} else if errors.Is(err, application.ErrBusy) {
		problem(w, 503, "DOWNLOAD_CAPACITY_EXCEEDED", "Metadata capacity exceeded")
	} else {
		problem(w, 502, "METADATA_UNTRUSTED", "Failed to fetch trusted metadata")
	}
}
func (s *Server) validUI(path string) bool {
	switch path {
	case "/all", "/", "/admin/login", "/admin/overview", "/admin/events", "/admin/settings/site", "/admin/settings/proxy", "/admin/vendors", "/admin/vendors/new", "/admin/categories":
		return true
	}
	if strings.HasPrefix(path, "/admin/vendors/") {
		p := strings.Split(strings.TrimPrefix(path, "/admin/vendors/"), "/")
		if len(p) == 1 || len(p) == 2 && (p[1] == "settings" || p[1] == "apps" || p[1] == "admin-notes") || len(p) == 3 && p[1] == "apps" && p[2] == "new" {
			_, err := s.DB.Vendor(p[0])
			return err == nil
		}
		if len(p) == 4 && p[1] == "apps" {
			e, ok := s.Registry.LookupAny(p[0] + "/" + p[2])
			return ok && (p[3] == "settings" || p[3] == "admin-notes" || p[3] == "files" && e.Provider == application.Hosted || p[3] == "cache" && e.Provider != application.Info && e.Provider != application.Hosted || p[3] == "versions" && e.Protocol != nil)
		}
	}
	return false
}
func (s *Server) page(w http.ResponseWriter, status int) {
	body, err := web.ReadFile("web/index.html")
	if err != nil {
		fail(w, 503, "Page unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.WriteHeader(status)
	w.Write(body)
}
func (s *Server) asset(w http.ResponseWriter, r *http.Request) {
	if asset, ok := builtin.BrandAsset(r.URL.Path); ok {
		w.Header().Set("Content-Type", asset.ContentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		http.ServeContent(w, r, "icon.svg", time.Time{}, bytes.NewReader(asset.Body))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	name := strings.TrimPrefix(path, "assets/")
	if strings.Contains(name, "/") || (filepath.Ext(name) != ".js" && filepath.Ext(name) != ".css" && filepath.Ext(name) != ".woff2" && name != "JetBrainsMono-OFL-v2.304.txt") {
		fail(w, 404, "Asset not found")
		return
	}
	assets, _ := fs.Sub(web, "web")
	b, err := fs.ReadFile(assets, path)
	if err != nil {
		fail(w, 404, "Asset not found")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if strings.HasSuffix(name, ".js") {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	} else if strings.HasSuffix(name, ".woff2") {
		w.Header().Set("Content-Type", "font/woff2")
		// Fonts are CORS fetches; sandboxed instruction documents have an opaque origin.
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else if strings.HasSuffix(name, ".txt") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	w.Write(b)
}
func (s *Server) serveResource(w http.ResponseWriter, r *http.Request, resource download.Resource) {
	r, finish, err := s.applicationResponse(w, r, resource.Application)
	if err != nil {
		fail(w, 404, "Application unavailable")
		return
	}
	defer finish()
	defer s.DB.SettleCounters() // Persist this transfer's counters promptly; failures are reported, never fatal.
	app := resource.Application
	metricApp := resource.MetricScope()
	if err := s.DB.AddFor(metricApp, "artifact_requests", 1); err != nil {
		fail(w, 503, "Failed to record request")
		return
	}
	if err := s.DB.AddVersion(app, resource.Version, 1, 0); err != nil {
		fail(w, 503, "Failed to record request")
		return
	}
	rd, _, err := s.Downloads.Acquire(r.Context(), resource)
	if err != nil {
		if errors.Is(err, download.ErrArtifactLimit) {
			problem(w, 413, "ARTIFACT_TOO_LARGE", "Artifact exceeds the configured size limit")
			return
		}
		problem(w, 503, "DOWNLOAD_CAPACITY_EXCEEDED", "Download capacity exceeded or local storage unavailable")
		return
	}
	defer rd.Close()
	if err = s.DB.AddFor(metricApp, rd.Kind+"_requests", 1); err != nil {
		fail(w, 503, "Failed to record acquisition")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Expected-SHA256", resource.Hash)
	buf := make([]byte, 32<<10)
	for {
		n, re := rd.Read(buf)
		if n > 0 {
			written, we := w.Write(buf[:n])
			// Counters are buffered in memory; accounting never aborts a client transfer.
			_ = s.DB.AddFor(metricApp, "downstream_bytes", int64(written))
			_ = s.DB.AddVersion(app, resource.Version, 0, int64(written))
			if we != nil {
				s.DB.AddFor(metricApp, "download_errors", 1)
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if re != nil {
			if re == io.EOF {
				s.DB.AddFor(metricApp, "download_success", 1)
				if receipt, ok := w.(*downloadReceipt); ok && receipt.status == 0 {
					receipt.WriteHeader(200)
				}
				return
			}
			s.DB.AddFor(metricApp, "download_errors", 1)
			panic(http.ErrAbortHandler)
		}
	}
}
func expectedRevision(r *http.Request) (int64, error) {
	s := r.Header.Get("If-Match")
	if s == "" {
		return 0, errors.New("If-Match revision is required")
	}
	v, err := strconv.ParseInt(strings.Trim(s, "\""), 10, 64)
	if err != nil || v < 0 {
		return 0, errors.New("Invalid If-Match revision")
	}
	return v, nil
}
func revisionReply(w http.ResponseWriter, revision int64, value any) {
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(revision, 10)))
	reply(w, 200, value)
}
func settingsError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrConflict) {
		problem(w, 409, "SETTINGS_REVISION_CONFLICT", "Settings changed; reload before saving")
	} else {
		fail(w, 503, "Unable to persist settings")
	}
}
func (s *Server) admin(w http.ResponseWriter, r *http.Request, requestOrigin, public string) {
	if r.Header.Get("Origin") != "" && r.Header.Get("Origin") != requestOrigin {
		problem(w, 403, "ORIGIN_REJECTED", "Origin not allowed")
		return
	}
	path := r.URL.Path
	if path == "/admin/api/login" {
		if r.Method != "POST" {
			fail(w, 405, "Method not allowed")
			return
		}
		if !queryAllowed(r) {
			fail(w, 400, "Invalid query")
			return
		}
		var input struct {
			Password string `json:"password"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid request")
			return
		}
		token, session, err := s.Auth.Login(s.Proxy.ClientIP(r), input.Password)
		if errors.Is(err, auth.ErrRateLimited) {
			problem(w, 429, "LOGIN_RATE_LIMITED", "Too many login attempts; try again later")
			return
		} else if errors.Is(err, auth.ErrSessionLimit) {
			problem(w, 503, "SESSION_LIMIT_EXCEEDED", "Too many active sessions; try again later")
			return
		} else if err != nil {
			problem(w, 401, "LOGIN_FAILED", "Login failed")
			return
		}
		s.Auth.Cookie(w, token, strings.HasPrefix(requestOrigin, "https://"))
		reply(w, 200, map[string]string{"csrf": session.CSRF})
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
	if s.exchangeAPI(w, r, session) {
		return
	}
	if s.categoriesAPI(w, r) {
		return
	}
	if s.directoryAPI(w, r) {
		return
	}
	app := ""
	endpoint := strings.TrimPrefix(path, "/admin/api/")
	if strings.HasPrefix(endpoint, "apps/") {
		p := strings.SplitN(strings.TrimPrefix(endpoint, "apps/"), "/", 3)
		if len(p) != 3 {
			fail(w, 404, "API endpoint not found")
			return
		}
		app = p[0] + "/" + p[1]
		if _, exists := s.Registry.LookupAny(app); !exists {
			problem(w, 404, "APPLICATION_NOT_FOUND", "Application not found")
			return
		}
		endpoint = p[2]
		if s.hostedAPI(w, r, app, endpoint) {
			return
		}
		entry, _ := s.Registry.LookupAny(app)
		if entry.Provider == application.Info || entry.Provider == application.Hosted {
			fail(w, 404, "Content applications do not provide distribution endpoints")
			return
		}
	}
	if endpoint == "history" {
		if !queryAllowed(r, "scope", "metric", "range") {
			fail(w, 400, "Invalid query")
			return
		}
		if app != "" && r.URL.Query().Has("scope") {
			fail(w, 400, "Application history does not accept scope")
			return
		}
		if app == "" && r.URL.Query().Get("scope") != "global" {
			fail(w, 400, "Global history requires scope=global")
			return
		}
	} else if r.Method == "GET" && (endpoint == "versions" || endpoint == "resources" || endpoint == "events") {
		allowed := []string{"limit", "cursor"}
		if endpoint != "events" {
			allowed = append(allowed, "page")
		}
		if endpoint == "resources" {
			allowed = append(allowed, "version")
		}
		if !queryAllowed(r, allowed...) {
			fail(w, 400, "Invalid query")
			return
		}
	} else if app != "" && (endpoint == "cache" || strings.HasPrefix(endpoint, "cache/") || strings.HasPrefix(endpoint, "cleanup/")) {
		allowed := []string{"source_epoch"}
		if r.Method == http.MethodGet && strings.HasSuffix(endpoint, "/items") {
			allowed = append(allowed, "cursor", "limit")
		}
		if !queryAllowed(r, allowed...) || r.URL.Query().Has("source_epoch") && r.URL.Query().Get("source_epoch") == "" {
			fail(w, 400, "Invalid source selection")
			return
		}
	} else if app != "" && r.Method == http.MethodGet && (strings.HasPrefix(endpoint, "retention/") || strings.HasPrefix(endpoint, "prewarm/")) && strings.HasSuffix(endpoint, "/items") {
		if !queryAllowed(r, "page", "limit") {
			fail(w, 400, "Invalid query")
			return
		}
	} else if !queryAllowed(r) {
		fail(w, 400, "Invalid query")
		return
	}
	if s.prewarmAPI(w, r, app, endpoint) {
		return
	}
	if s.retentionAPI(w, r, app, endpoint) {
		return
	}
	if s.cacheAPI(w, r, app, endpoint) {
		return
	}
	if r.Method == "GET" {
		switch endpoint {
		case "session":
			if app != "" {
				break
			}
			reply(w, 200, map[string]string{"csrf": session.CSRF})
			return
		case "status":
			var result map[string]any
			var err error
			if app == "" {
				result, err = s.status(public)
			} else {
				result, err = s.appStatus(app, public)
			}
			if err != nil {
				fail(w, 503, "Failed to read status")
				return
			}
			for _, key := range []string{"resources", "events", "versions", "version_stats", "application_versions"} {
				delete(result, key)
			}
			reply(w, 200, result)
			return
		case "versions", "resources":
			if app == "" {
				break
			}
			s.applicationList(w, r, app, endpoint)
			return
		case "events":
			s.eventList(w, r, app)
			return
		case "history":
			if s.History == nil {
				fail(w, 503, "History unavailable")
				return
			}
			key, window := r.URL.Query().Get("metric"), r.URL.Query().Get("range")
			validMetric := false
			if app == "" {
				_, validMetric = history.Find(key)
			} else {
				for _, definition := range history.AppDefinitions() {
					if definition.Key == key {
						validMetric = true
						break
					}
				}
			}
			if !validMetric || (window != "24h" && window != "7d" && window != "30d") {
				fail(w, 400, "Invalid history metric or range")
				return
			}

			var series history.Series
			var err error
			if app == "" {
				series, err = s.History.Query(key, window, time.Now().UTC())
			} else {
				entry, _ := s.Registry.LookupAny(app)
				series, err = s.History.QueryFor(entry.MetricsID(), key, window, time.Now().UTC())
			}
			if err != nil {
				fail(w, 503, "Failed to read metric history")
				return
			}
			reply(w, 200, series)
			return
		case "settings":
			if app == "" {
				break
			}
			entry, _ := s.Registry.LookupAny(app)
			if entry.Protocol == nil {
				break
			}
			ttl, rev := entry.Descriptor.DefaultChannelTTLSeconds, entry.Revision
			revisionReply(w, rev, map[string]any{"channel_ttl_seconds": ttl, "revision": rev})
			return
		case "settings/site":
			if app != "" {
				break
			}
			v, err := site.LoadSnapshot(s.DB)
			if err != nil {
				fail(w, 503, "Site settings unavailable")
				return
			}
			revisionReply(w, v.Revision, v)
			return
		case "settings/proxy":
			if app != "" || s.Upstream == nil {
				break
			}
			v := s.Upstream.Proxy().Redacted()
			revisionReply(w, v.Revision, v)
			return
		case "settings/public-url":
			if app != "" || s.PublicConfig == nil {
				break
			}
			v := s.PublicConfig.View(requestOrigin)
			revisionReply(w, v.Revision, v)
			return
		}
		fail(w, 404, "API endpoint not found")
		return
	}
	if r.Method == "PUT" {
		rev, err := expectedRevision(r)
		if err != nil {
			problem(w, 400, "INVALID_REQUEST", err.Error())
			return
		}
		switch endpoint {
		case "settings":
			if app == "" {
				break
			}
			entry, _ := s.Registry.LookupAny(app)
			if entry.Protocol == nil {
				break
			}
			var input struct {
				TTL int `json:"channel_ttl_seconds"`
			}
			if decode(w, r, &input) != nil {
				fail(w, 400, "Invalid settings")
				return
			}
			if input.TTL < 1 || input.TTL > 86400 {
				fail(w, 400, "Channel TTL must be between 1 and 86400 seconds")
				return
			}
			next, e := s.setDirectoryTTL(app, rev, input.TTL)
			if e != nil {
				settingsError(w, e)
				return
			}
			revisionReply(w, next, map[string]any{"channel_ttl_seconds": input.TTL, "revision": next})
			return
		case "settings/site":
			if app != "" {
				break
			}
			var input site.Settings
			if decode(w, r, &input) != nil {
				fail(w, 400, "Invalid site settings")
				return
			}
			if input.Validate() != nil {
				fail(w, 400, "Invalid site settings")
				return
			}
			v, e := site.SaveCAS(s.DB, input, rev)
			if e != nil {
				settingsError(w, e)
				return
			}
			revisionReply(w, v.Revision, v)
			return
		case "settings/proxy":
			if app != "" || s.Upstream == nil {
				break
			}
			var input distributor.ProxyUpdate
			if decode(w, r, &input) != nil {
				fail(w, 400, "Invalid proxy settings")
				return
			}
			if e := s.Upstream.SetProxy(input, rev); e != nil {
				if errors.Is(e, distributor.ErrInvalidProxySettings) {
					fail(w, 400, "Invalid proxy settings")
					return
				}
				settingsError(w, e)
				return
			}
			v := s.Upstream.Proxy().Redacted()
			revisionReply(w, v.Revision, v)
			return
		case "settings/public-url":
			if app != "" || s.PublicConfig == nil {
				break
			}
			var input map[string]json.RawMessage
			if decode(w, r, &input) != nil || len(input) != 1 || input["override_url"] == nil {
				fail(w, 400, "Expected override_url")
				return
			}
			var value *string
			if json.Unmarshal(input["override_url"], &value) != nil {
				fail(w, 400, "Invalid public URL")
				return
			}
			if value != nil {
				clean, e := config.PublicURL(*value)
				if e != nil || clean == "" {
					fail(w, 400, "Invalid public URL")
					return
				}
			}
			_, e := s.PublicConfig.Set(value, rev)
			if e != nil {
				settingsError(w, e)
				return
			}
			v := s.PublicConfig.View(requestOrigin)
			revisionReply(w, v.Revision, v)
			return
		}
		fail(w, 404, "API endpoint not found")
		return
	}
	if r.Method != "POST" {
		fail(w, 405, "Method not allowed")
		return
	}
	switch endpoint {
	case "logout":
		if app != "" {
			break
		}
		s.Auth.Logout(r)
		s.Auth.Cookie(w, "", strings.HasPrefix(requestOrigin, "https://"))
		reply(w, 200, map[string]bool{"ok": true})
		return
	case "password":
		if app != "" {
			break
		}
		var input struct {
			Old string `json:"old"`
			New string `json:"new"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid request")
			return
		}
		if s.Auth.Password(input.Old, input.New) != nil {
			fail(w, 400, "Password change failed; check the current password and new password length")
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
		return
	case "cleanup/preview":
		if app == "" {
			break
		}
		var input struct {
			Minimum string `json:"minimum_version"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid request")
			return
		}
		entry, e := s.sourceEntry(app, r)
		if e != nil {
			directoryError(w, e)
			return
		}
		views := s.Downloads.Snapshot()
		ids, unknown, e := s.Catalog.CandidatesForSource(app, entry.StorageID(), input.Minimum, views)
		if e != nil {
			fail(w, 400, "Invalid minimum version")
			return
		}
		job, e := s.Downloads.Preview(entry.StorageID(), ids)
		if e != nil {
			fail(w, 503, "Failed to persist cleanup preview")
			return
		}
		reply(w, 200, map[string]any{"job": job, "logical_bytes": job.LogicalBytes, "reclaimable_blob_bytes": job.ReclaimableBlobBytes, "active": job.ActiveGenerations, "unknown_versions": unknown})
		return
	}
	if app != "" && strings.HasPrefix(endpoint, "cleanup/") && strings.HasSuffix(endpoint, "/execute") {
		p := strings.Split(endpoint, "/")
		if len(p) == 3 {
			entry, e := s.sourceEntry(app, r)
			if e != nil {
				directoryError(w, e)
				return
			}
			if e := s.Downloads.Cleanup(entry.StorageID(), p[1]); e != nil {
				problem(w, 409, "CLEANUP_INVALID", "Cleanup preview is expired, has a different application, or execution failed")
				return
			}
			reply(w, 200, map[string]bool{"ok": true})
			return
		}
	}
	fail(w, 404, "API endpoint not found")
}
