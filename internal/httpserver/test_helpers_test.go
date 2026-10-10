package httpserver

import (
	"net/http/httptest"
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
	"golang.org/x/crypto/bcrypt"
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
	// Route/transfer fixtures exercise authentication without repeatedly paying
	// the production bootstrap work factor. The auth package and CLI tests still
	// cover fresh bootstrap with production cost 12.
	const password = "isolated-http-test-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO admin VALUES(1,?,1)", hash); err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(db, func(string) { t.Fatal("existing test admin was replaced") })
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
	return &Server{DB: db, Registry: registry, Catalog: catalog.New(db, registry), Downloads: manager, Auth: a, History: h, PublicConfig: p, Dir: dir, Started: time.Now()}, db, password
}
func startTestServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	h := httptest.NewUnstartedServer(s)
	h.Start()
	t.Cleanup(h.Close)
	return h
}
