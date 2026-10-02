package httpserver

import (
	"github.com/PMExtra/RedApp/internal/apps"
	"github.com/PMExtra/RedApp/internal/apps/claude"
	app "github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/site"
	"io"
	"io/fs"
	"net/http"
	"runtime"
)

func (s *Server) publicPage(w http.ResponseWriter, r *http.Request, origin string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if r.URL.Path == "/api/info" {
		version := s.Version
		if version == "" {
			version = "dev"
		}
		settings, err := site.Load(s.DB)
		if err != nil {
			fail(w, 503, "Site settings are unavailable")
			return
		}
		reply(w, http.StatusOK, map[string]any{"version": version, "os": runtime.GOOS, "arch": runtime.GOARCH, "site": settings})
		return
	}
	if r.URL.Path == "/api/apps" {
		reply(w, http.StatusOK, []apps.PublicInfo{app.PublicApplication(origin), claude.PublicApplication(origin)})
		return
	}
	if r.URL.Path == "/apps/codex/icon.svg" {
		w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		io.WriteString(w, app.OpenAISymbol())
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/apps/codex" && r.URL.Path != "/apps/claude-code" {
		fail(w, http.StatusNotFound, "Page not found")
		return
	}
	assets, _ := fs.Sub(web, "web")
	body, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "Page is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(body)
}
