package httpserver

import (
	assets "github.com/PMExtra/RedApp/installers/claude-code"
	"github.com/PMExtra/RedApp/internal/apps/claude"
	"net/http"
	"strings"
)

func (s *Server) application(id string) (string, bool) {
	if id == "" || id == "codex" {
		return "codex", true
	}
	return id, id == claude.ID && s.Claude != nil
}
func (s *Server) claudeDownload(w http.ResponseWriter, r *http.Request, origin string) {
	if s.Claude == nil {
		fail(w, 503, "Claude distribution is unavailable")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/claude-code/")
	w.Header().Set("Cache-Control", "no-store")
	if path == "install.sh" || path == "install.ps1" {
		b, err := assets.Installer(path, origin+"/claude-code")
		if err != nil {
			fail(w, 503, "Installer is unavailable")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(b)
		return
	}
	if path == "claude-code.asc" || path == "LICENSE.md" {
		b := assets.PublicKey()
		if path == "LICENSE.md" {
			b = assets.License()
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(b)
		return
	}
	if path == "latest" || path == "stable" {
		m, err := s.Claude.Get(r.Context(), path)
		if err != nil {
			fail(w, 502, "Failed to fetch trusted metadata")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(m.Manifest.Version + "\n"))
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || !claude.ValidVersion(parts[0]) {
		fail(w, 404, "Route not found")
		return
	}
	if len(parts) == 2 && (parts[1] == "manifest.json" || parts[1] == "manifest.json.sig") {
		m, err := s.Claude.Get(r.Context(), parts[0])
		if err != nil {
			fail(w, 502, "Failed to fetch trusted metadata")
			return
		}
		if parts[1] == "manifest.json" {
			w.Header().Set("Content-Type", "application/json")
			w.Write(m.Raw)
		} else {
			w.Header().Set("Content-Type", "application/pgp-signature")
			w.Write(m.Signature)
		}
		return
	}
	if len(parts) != 3 {
		fail(w, 404, "Route not found")
		return
	}
	resource, err := s.Claude.Authorize(r.Context(), parts[0], parts[1], parts[2])
	if err != nil {
		fail(w, 404, "Resource is not authorized or metadata is unavailable")
		return
	}
	s.serveResource(w, r, resource)
}
