package httpserver

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/auth"
	"github.com/PMExtra/RedApp/internal/catalog"
	"github.com/PMExtra/RedApp/internal/config"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/history"
	"github.com/PMExtra/RedApp/internal/hosted"
	"github.com/PMExtra/RedApp/internal/httpcache"
	"github.com/PMExtra/RedApp/internal/media"
	"github.com/PMExtra/RedApp/internal/prewarm"
	"github.com/PMExtra/RedApp/internal/releasemaintenance"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/store/storetest"
	"golang.org/x/crypto/bcrypt"
)

// harness is the shared HTTP test fixture: a real store and real services in
// a temporary data directory, the server built with New and an httptest
// listener. Every response read through h.client is validated against
// api/openapi.yaml (see contractTransport).
type harness struct {
	t        *testing.T
	server   *Server
	store    *store.Store
	dir      string
	http     *httptest.Server
	client   *http.Client
	password string
	csrf     string
	logs     *logBuffer
	close    func()
	rawDB    *sql.DB
}

// sql returns a separate connection to the harness database for fixtures and
// fault injection that have no store API (see storetest.Open).
func (h *harness) sql() *sql.DB {
	if h.rawDB == nil {
		h.rawDB = storetest.Open(h.t, h.dir)
	}
	return h.rawDB
}

type harnessConfig struct {
	dir     string
	options []Option
	proxies TrustedProxies
	// embeddedFrontend serves the committed build instead of frontendFixture.
	embeddedFrontend bool
}

type harnessOption func(*harnessConfig)

// withDir reuses a data directory, for example to restart over the same data.
func withDir(dir string) harnessOption { return func(c *harnessConfig) { c.dir = dir } }

// withOptions passes construction options to New.
func withOptions(options ...Option) harnessOption {
	return func(c *harnessConfig) { c.options = append(c.options, options...) }
}

// withTrustedProxies trusts forwarding headers from the given CIDRs.
func withTrustedProxies(t *testing.T, cidrs ...string) harnessOption {
	p, err := ParseTrustedProxies(cidrs)
	if err != nil {
		t.Fatal(err)
	}
	return func(c *harnessConfig) { c.proxies = p }
}

// withEmbeddedFrontend serves the committed frontend build from internal/httpserver/web.
func withEmbeddedFrontend() harnessOption {
	return func(c *harnessConfig) { c.embeddedFrontend = true }
}

// harnessPassword is the administrator password of fixtures created without
// an account; it is hashed at bcrypt.MinCost so tests do not pay the
// production work factor (the auth package covers that).
const harnessPassword = "isolated-http-test-password"

func newHarness(t *testing.T, opts ...harnessOption) *harness {
	t.Helper()
	cfg := harnessConfig{}
	for _, apply := range opts {
		apply(&cfg)
	}
	if cfg.dir == "" {
		cfg.dir = t.TempDir()
	}
	db, err := store.Open(cfg.dir)
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	// A directory that already has vendors was initialized by an earlier
	// harness. The embedded templates start disabled as in production; the
	// fixture enables them for route coverage.
	existing, err := db.Vendors(true)
	must(err)
	must(db.EnsureEntityTemplates())
	if len(existing) == 0 {
		vendors, err := db.Vendors(false)
		must(err)
		for _, v := range vendors {
			if !v.Enabled {
				enabled := true
				_, err = db.PatchVendorFields(v.ID, v.Revision, nil, &enabled)
				must(err)
			}
		}
		apps, err := db.Applications(false)
		must(err)
		for _, a := range apps {
			if !a.Enabled {
				enabled := true
				_, err = db.PatchApplicationFields(a.Key, a.Revision, nil, &enabled)
				must(err)
			}
		}
	}
	h := &harness{t: t, store: db, dir: cfg.dir, logs: &logBuffer{}}
	if _, err = db.AdminPassword(); errors.Is(err, store.ErrNotFound) {
		hash, err := bcrypt.GenerateFromPassword([]byte(harnessPassword), bcrypt.MinCost)
		must(err)
		_, err = db.CreateAdminPassword(hash)
		must(err)
		h.password = harnessPassword
	} else {
		must(err)
	}
	pool := distributor.NewPool()
	must(pool.LoadProxy(db))
	snapshot, err := db.DirectoryConfigurationSnapshot()
	must(err)
	entries, err := builtin.EntriesFromConfiguration(snapshot, pool)
	must(err)
	registry, err := application.NewRegistry(entries)
	must(err)
	clients := map[string]*distributor.Client{}
	for _, source := range snapshot.Sources {
		client, err := builtin.NewScopedSourceClient(source.Provider, source.BaseURL, snapshot.ProviderDefaults[source.Provider], source.AppUID, snapshot.ProxyScopes[source.AppUID].VendorUID, pool)
		must(err)
		clients[source.StorageID()] = client
	}
	manager, err := download.NewApplications(cfg.dir, db, clients)
	must(err)
	icons, err := media.New(cfg.dir)
	must(err)
	httpCache, err := httpcache.New(cfg.dir, db, manager)
	must(err)
	hostedFiles, err := hosted.New(cfg.dir, db, manager)
	must(err)
	a, err := auth.New(db, func(password string) { h.password = password })
	must(err)
	metricHistory, err := history.Open(db)
	must(err)
	public, err := config.LoadPublicSettings(db, "")
	must(err)
	catalogService := catalog.New(db, registry)
	prewarmer, err := prewarm.New(db, registry, catalogService, manager, httpCache)
	must(err)
	maintenance := &releasemaintenance.Service{DB: db, Registry: registry, Catalog: catalogService, Downloads: manager, AutomaticPrewarm: prewarmer.Automatic}
	var frontend fs.FS = frontendFixture
	if cfg.embeddedFrontend {
		frontend = nil
	}
	h.server, err = New(Deps{
		Frontend: frontend,
		Version:  "test", Store: db, Registry: registry, Catalog: catalogService, Downloads: manager,
		HTTPCache: httpCache, Hosted: hostedFiles, Auth: a, TrustedProxies: cfg.proxies, Pool: pool, Icons: icons,
		History: metricHistory, PublicSettings: public, Prewarmer: prewarmer, Maintenance: maintenance,
		DataDir: cfg.dir, Started: time.Now(), Logger: slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}, cfg.options...)
	must(err)
	h.http = httptest.NewServer(h.server)
	jar, err := cookiejar.New(nil)
	must(err)
	h.client = &http.Client{Jar: jar, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &contractTransport{t: t, spec: openAPISpec(t), base: h.http.Client().Transport}}
	var once sync.Once
	h.close = func() {
		once.Do(func() {
			h.http.Close()
			prewarmer.Close()
			hostedFiles.Close()
			httpCache.Close()
			manager.Close()
			icons.Close()
			pool.CloseIdleConnections()
			db.Close()
		})
	}
	t.Cleanup(h.close)
	return h
}

// logBuffer collects the server's structured logs for assertions.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// raw sends one request; the session CSRF token is added when signed in.
func (h *harness) raw(method, path string, body io.Reader, contentType string, headers map[string]string) (int, []byte, http.Header) {
	h.t.Helper()
	r, err := http.NewRequest(method, h.http.URL+path, body)
	if err != nil {
		h.t.Fatal(err)
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if h.csrf != "" {
		r.Header.Set("X-CSRF-Token", h.csrf)
	}
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	response, err := h.client.Do(r)
	if err != nil {
		h.t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		h.t.Fatal(err)
	}
	return response.StatusCode, data, response.Header
}

// request sends body as JSON and requires status.
func (h *harness) request(method, path string, body any, status int, headers map[string]string) ([]byte, http.Header) {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	code, data, responseHeaders := h.raw(method, path, reader, "application/json", headers)
	if code != status {
		h.t.Fatalf("%s %s: got %d, want %d: %s", method, path, code, status, data)
	}
	return data, responseHeaders
}

// errorCode returns error.code of an Error document.
func errorCodeOf(t *testing.T, data []byte) string {
	t.Helper()
	var e errorBody
	if err := json.Unmarshal(data, &e); err != nil || e.Error.Code == "" {
		t.Fatalf("not an Error document: %s", data)
	}
	return string(e.Error.Code)
}

// expectError requires an Error document with status and code.
func (h *harness) expectError(method, path string, body any, status int, code errorCode, headers map[string]string) {
	h.t.Helper()
	data, _ := h.request(method, path, body, status, headers)
	if got := errorCodeOf(h.t, data); got != string(code) {
		h.t.Fatalf("%s %s: error code %s, want %s", method, path, got, code)
	}
}

// login signs in with password (h.password when empty) and keeps the CSRF token.
func (h *harness) login(password string) {
	h.t.Helper()
	if password == "" {
		password = h.password
	}
	h.csrf = ""
	data, _ := h.request("POST", "/admin/api/session", map[string]string{"password": password}, 201, nil)
	var session sessionDTO
	if err := json.Unmarshal(data, &session); err != nil || session.CSRFToken == "" {
		h.t.Fatal("sign-in did not return a CSRF token", err)
	}
	h.csrf = session.CSRFToken
}

func directoryDecode[T any](t *testing.T, data []byte, field string) T {
	t.Helper()
	var result map[string]json.RawMessage
	var value T
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(result[field], &value); err != nil {
		t.Fatalf("missing/invalid %s in %s: %v", field, data, err)
	}
	return value
}

func decodeJSONBody[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("invalid JSON %s: %v", data, err)
	}
	return value
}

// createVendor creates an enabled vendor through the store, independent of the
// administrator API under test.
func (h *harness) createVendor(id string) store.Vendor {
	h.t.Helper()
	v, err := h.store.CreateVendor(store.VendorInput{ID: id, Name: store.LocalizedText{En: "Enterprise", ZhCN: "企业"}, Description: store.LocalizedText{En: "Software downloads", ZhCN: "软件下载"}, Enabled: true})
	if err != nil {
		h.t.Fatal(err)
	}
	return v
}

// createApp creates an application through the store. options use the
// configuration field names (base_url, base_urls, source_strategy,
// cache_ttl_seconds, enabled, icon, categories, tags); applications are
// enabled unless options say otherwise.
func (h *harness) createApp(vendor, id, provider string, options map[string]any) store.Application {
	h.t.Helper()
	definition, ok := application.ProviderDefinition(provider)
	if !ok {
		h.t.Fatalf("unknown provider %s", provider)
	}
	input := store.ApplicationInput{ID: id, Name: store.LocalizedText{En: id, ZhCN: id}, Provider: provider, CacheTTLSeconds: definition.DefaultCacheTTLSeconds, Enabled: true}
	if len(options) > 0 {
		raw, _ := json.Marshal(options)
		if err := json.Unmarshal(raw, &input); err != nil {
			h.t.Fatal(err)
		}
	}
	config, err := application.NormalizeConfig(provider, application.ProviderConfig{BaseURL: input.BaseURL, BaseURLs: input.BaseURLs, SourceStrategy: input.SourceStrategy, CacheTTLSeconds: input.CacheTTLSeconds})
	if err != nil {
		h.t.Fatal(err)
	}
	input.BaseURL, input.BaseURLs, input.SourceStrategy, input.CacheTTLSeconds = config.BaseURL, config.BaseURLs, config.SourceStrategy, config.CacheTTLSeconds
	a, err := h.store.CreateApplication(vendor, input)
	if err != nil {
		h.t.Fatal(err)
	}
	return a
}

// publicCatalog reads every published application from listCatalog. The
// digest changes whenever any public application document changes.
func (h *harness) publicCatalog() (string, map[string]publicAppDTO) {
	h.t.Helper()
	data, _ := h.request("GET", "/api/catalog?limit=100", nil, 200, nil)
	page := decodeJSONBody[catalogPageDTO](h.t, data)
	result := make(map[string]publicAppDTO, len(page.Items))
	for _, app := range page.Items {
		result[app.Key] = app
	}
	for _, private := range []string{`"base_url"`, `"source_epoch"`, `"uid"`, `"password"`, `"upstream_proxy"`} {
		if bytes.Contains(data, []byte(private)) {
			h.t.Fatalf("public catalog exposed private configuration %s", private)
		}
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), result
}

// contractTransport validates every response against api/openapi.yaml.
// JSON bodies are buffered for schema validation; other bodies stream.
type contractTransport struct {
	t    *testing.T
	spec *contractSpec
	base http.RoundTripper
}

func (c *contractTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := c.base.RoundTrip(r)
	if err != nil {
		return response, err
	}
	var body []byte
	media, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if media == "application/json" && r.Method != http.MethodHead {
		body, err = io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return nil, err
		}
		response.Body = io.NopCloser(bytes.NewReader(body))
	}
	c.check(r, response.StatusCode, response.Header, body, media == "application/json")
	return response, nil
}

func (c *contractTransport) check(r *http.Request, status int, header http.Header, body []byte, bodyRead bool) {
	if status == http.StatusPermanentRedirect && strings.HasSuffix(r.URL.Path, "/") {
		// getAppPage documents the trailing-slash redirect in its description.
		return
	}
	op := c.spec.match(r.Method, r.URL.Path)
	if problems := c.spec.validateResponse(op, r.Method, status, header, body, bodyRead); len(problems) > 0 {
		operation := "no operation"
		if op != nil {
			operation = op.id
		}
		c.t.Errorf("contract violation: %s %s -> %d (%s):\n  %s", r.Method, r.URL.RequestURI(), status, operation, strings.Join(problems, "\n  "))
	}
}

// serve runs one request in process (for requests that need a chosen
// RemoteAddr or Host) and validates the response like contractTransport.
func (h *harness) serve(r *http.Request) *httptest.ResponseRecorder {
	h.t.Helper()
	w := httptest.NewRecorder()
	h.server.ServeHTTP(w, r)
	c := &contractTransport{t: h.t, spec: openAPISpec(h.t)}
	c.check(r, w.Code, w.Header(), w.Body.Bytes(), true)
	return w
}

// fixtureUpstream is the source host of fixture release applications; the
// harness's global proxy (upstreamProxy) answers every request for it.
const fixtureUpstream = "http://upstream.example"

// upstreamProxy installs handler as the global upstream HTTP proxy. Requests
// for http:// sources (fixtureUpstream) arrive in absolute form; handlers
// route on r.URL.Path.
func (h *harness) upstreamProxy(handler http.Handler) {
	h.t.Helper()
	proxy := httptest.NewServer(handler)
	h.t.Cleanup(proxy.Close)
	if err := h.server.pool.SetProxy(distributor.ProxyUpdate{Mode: "url", URL: proxy.URL}, h.server.pool.Proxy().Revision); err != nil {
		h.t.Fatal(err)
	}
}

// releaseApp creates vendor/app with a release provider sourced from fixtureUpstream.
func (h *harness) releaseApp(vendor, app, provider string) string {
	h.t.Helper()
	if _, err := h.store.Vendor(vendor); err != nil {
		h.createVendor(vendor)
	}
	return h.createApp(vendor, app, provider, map[string]any{"base_url": fixtureUpstream, "cache_ttl_seconds": 60}).Key
}

// codexRelease serves one Codex release whose assets are the given files,
// with channels/latest pointing at it. served counts artifact requests. Asset
// URLs are the official ones, which every configured upstream (base path)
// accepts and rebinds to itself.
func codexRelease(version string, files map[string][]byte, served func(name string)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/channels/latest") || strings.HasSuffix(r.URL.Path, "/release.json") {
			release := codex.Release{Tag: "rust-v" + version}
			for name, body := range files {
				digest := sha256.Sum256(body)
				release.Assets = append(release.Assets, codex.Asset{Name: name, Digest: "sha256:" + hex.EncodeToString(digest[:]), URL: "https://releases.openai.com/codex/releases/" + version + "/" + name})
			}
			_ = json.NewEncoder(w).Encode(release)
			return
		}
		name := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		body, ok := files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if served != nil {
			served(name)
		}
		_, _ = w.Write(body)
	})
}

// frontendFixture is the frontend build used by the harness: the two entry
// documents carry distinct markers and assets cover every served type, so
// tests do not depend on the committed bundle.
var frontendFixture = fstest.MapFS{
	"index.html":                                {Data: []byte(`<!doctype html><html><head><script type="module" src="/assets/public-fixture.js"></script></head><body data-entry="public"></body></html>`)},
	"admin.html":                                {Data: []byte(`<!doctype html><html><head><script type="module" src="/assets/admin-fixture.js"></script></head><body data-entry="admin"></body></html>`)},
	"assets/public-fixture.js":                  {Data: []byte("export {}\n")},
	"assets/admin-fixture.js":                   {Data: []byte("export {}\n")},
	"assets/style-fixture.css":                  {Data: []byte("body{}\n")},
	"assets/JetBrainsMono-Regular-v2.304.woff2": {Data: []byte("wOF2")},
	"assets/JetBrainsMono-OFL-v2.304.txt":       {Data: []byte("SIL Open Font License\n")},
}

// entryDocument reports which SPA entry a response body is ("public", "admin" or "").
func entryDocument(body []byte) string {
	for _, entry := range []string{"public", "admin"} {
		if bytes.Contains(body, []byte(`data-entry="`+entry+`"`)) {
			return entry
		}
	}
	return ""
}
