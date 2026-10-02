package httpserver

import (
	"net/http/httptest"
	"strings"
	"testing"
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
	"github.com/PMExtra/RedApp/internal/store"
)

func newTestServer(t *testing.T, upstream *distributor.Client) (*Server, *store.Store, string) {
	t.Helper()
	registry, err := builtin.New()
	if err != nil {
		t.Fatal(err)
	}
	if upstream != nil {
		entries := registry.Entries()
		for i := range entries {
			if entries[i].Descriptor.ID == "openai/codex" {
				entries[i].Upstream = upstream
				entries[i].Protocol = codex.NewProtocol(upstream)
			}
		}
		registry, err = application.NewRegistry(entries)
		if err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	manager, err := download.NewApplications(dir, db, registry.Upstreams())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	var password string
	a, err := auth.New(db, false, func(p string) { password = p })
	if err != nil {
		t.Fatal(err)
	}
	h, err := history.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.LoadPublicSettings(db, "")
	if err != nil {
		t.Fatal(err)
	}
	return &Server{DB: db, Registry: registry, Catalog: catalog.New(db, registry), Downloads: manager, Auth: a, History: h, PublicConfig: p, AllowedHosts: []string{"internal", "internal:8080", "redapp.local", "example.com"}, Dir: dir, Started: time.Now()}, db, password
}
func startTestServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	h := httptest.NewUnstartedServer(s)
	s.AllowedHosts = append(s.AllowedHosts, h.Listener.Addr().String())
	h.Start()
	t.Cleanup(h.Close)
	return h
}
func allowTestOrigin(s *Server, origin string) {
	s.AllowedHosts = append(s.AllowedHosts, strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://"))
}
