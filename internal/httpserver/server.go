package httpserver

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
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
)

//go:embed web/*
var web embed.FS

// Deps are the services the HTTP layer is built on. Every field except
// Version, Started and Logger is required; New validates them once.
type Deps struct {
	Version        string // build version; "" reports "dev"
	Store          *store.Store
	Registry       *application.Registry
	Catalog        *catalog.Service
	Downloads      *download.Manager
	HTTPCache      *httpcache.Service
	Hosted         *hosted.Service
	Auth           *auth.Auth
	TrustedProxies TrustedProxies // reverse proxies whose forwarding headers are trusted
	Pool           *distributor.Pool
	Icons          *media.Store
	History        *history.History
	PublicSettings *config.PublicSettings
	Prewarmer      *prewarm.Service
	Maintenance    *releasemaintenance.Service
	DataDir        string
	Started        time.Time    // process start; zero means now
	Logger         *slog.Logger // nil discards the logs
	// Frontend is the frontend build (index.html, admin.html, assets/); nil
	// means the build embedded from internal/httpserver/web.
	Frontend fs.FS
}

// Option adjusts a Server at construction.
type Option func(*Server)

// WithConfigurationCheck adds a validation step to every configuration
// publication after the candidate registry and transports are prepared. A
// failing check aborts the write before anything is committed.
func WithConfigurationCheck(check func(store.DirectorySnapshot) error) Option {
	return func(s *Server) { s.configurationCheck = check }
}

// WithDeleteWait bounds how long an application deletion waits for the
// application's running work to stop (default 15 seconds).
func WithDeleteWait(wait time.Duration) Option {
	return func(s *Server) { s.deleteWait = wait }
}

// WithClock replaces the clock that ages the cached readiness result
// (default time.Now).
func WithClock(now func() time.Time) Option {
	return func(s *Server) { s.now = now }
}

// Server is the HTTP layer: routing, middleware, request parsing and response
// encoding over the domain services in Deps.
type Server struct {
	version     string
	store       *store.Store
	registry    *application.Registry
	catalog     *catalog.Service
	downloads   *download.Manager
	httpCache   *httpcache.Service
	hosted      *hosted.Service
	auth        *auth.Auth
	proxies     TrustedProxies
	pool        *distributor.Pool
	icons       *media.Store
	history     *history.History
	public      *config.PublicSettings
	prewarmer   *prewarm.Service
	maintenance *releasemaintenance.Service
	dataDir     string
	started     time.Time
	log         *slog.Logger
	frontend    fs.FS

	mux    *http.ServeMux
	routes []route

	configurationCheck func(store.DirectorySnapshot) error
	deleteWait         time.Duration
	now                func() time.Time

	readiness readiness

	exchangeMu       sync.Mutex
	exchangePreviews map[string]exchangePreview
}

// New validates deps, installs the configuration publication coordinator on
// the store and builds the route table.
func New(deps Deps, options ...Option) (*Server, error) {
	var missing []string
	for name, present := range map[string]bool{
		"Store": deps.Store != nil, "Registry": deps.Registry != nil, "Catalog": deps.Catalog != nil,
		"Downloads": deps.Downloads != nil, "HTTPCache": deps.HTTPCache != nil, "Hosted": deps.Hosted != nil,
		"Auth": deps.Auth != nil, "Pool": deps.Pool != nil, "Icons": deps.Icons != nil,
		"History": deps.History != nil, "PublicSettings": deps.PublicSettings != nil,
		"Prewarmer": deps.Prewarmer != nil, "Maintenance": deps.Maintenance != nil, "DataDir": deps.DataDir != "",
	} {
		if !present {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return nil, fmt.Errorf("httpserver: missing dependencies: %s", strings.Join(missing, ", "))
	}
	s := &Server{
		version: deps.Version, store: deps.Store, registry: deps.Registry, catalog: deps.Catalog,
		downloads: deps.Downloads, httpCache: deps.HTTPCache, hosted: deps.Hosted, auth: deps.Auth,
		proxies: deps.TrustedProxies, pool: deps.Pool, icons: deps.Icons, history: deps.History,
		public: deps.PublicSettings, prewarmer: deps.Prewarmer, maintenance: deps.Maintenance,
		dataDir: deps.DataDir, started: deps.Started, log: deps.Logger,
		deleteWait: 15 * time.Second, now: time.Now,
	}
	if s.version == "" {
		s.version = "dev"
	}
	if s.started.IsZero() {
		s.started = time.Now().UTC()
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	s.frontend = deps.Frontend
	if s.frontend == nil {
		embedded, err := fs.Sub(web, "web")
		if err != nil {
			return nil, fmt.Errorf("httpserver: embedded frontend: %w", err)
		}
		s.frontend = embedded
	}
	for _, apply := range options {
		apply(s)
	}
	if s.deleteWait <= 0 {
		return nil, errors.New("httpserver: delete wait must be positive")
	}
	s.configurePublication()
	s.routes = s.routeTable()
	s.mux = http.NewServeMux()
	s.register(s.mux)
	return s, nil
}
