package httpserver

import (
	"net/http"
	"strings"
	"time"
)

func (s *Server) relatedAPI(w http.ResponseWriter, r *http.Request, origin string) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/apps/") || !strings.HasSuffix(r.URL.Path, "/related") {
		return false
	}
	key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/apps/"), "/related")
	entry, ok := s.Registry.Lookup(key)
	if !ok {
		fail(w, 404, "Application not found")
		return true
	}
	if r.Method != http.MethodGet || !queryAllowed(r) {
		fail(w, 400, "Invalid related application request")
		return true
	}
	keys, err := s.DB.RelatedApplications(entry.UID, time.Now())
	if err != nil {
		fail(w, 503, "Related applications unavailable")
		return true
	}
	taxonomy, _, err := s.DB.PublicTaxonomy()
	if err != nil {
		fail(w, 503, "Related applications unavailable")
		return true
	}
	items := []map[string]any{}
	for _, key := range keys {
		if e, ok := s.Registry.Lookup(key); ok {
			item, err := s.publicApplicationWithTaxonomy(e, origin, taxonomy[e.UID])
			if err != nil {
				fail(w, 503, "Related applications unavailable")
				return true
			}
			items = append(items, item)
		}
	}
	reply(w, 200, map[string]any{"items": items})
	return true
}
