package httpserver

import (
	"net/http"
	"strings"

	"github.com/PMExtra/RedApp/internal/store"
)

// categoriesAPI lists and renames categories. Categories are created while saving an App
// and removed automatically once no App or template uses them.
func (s *Server) categoriesAPI(w http.ResponseWriter, r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/admin/api/")
	if path != "categories" && !strings.HasPrefix(path, "categories/") {
		return false
	}
	parts := strings.Split(path, "/")
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		page, ok := positivePage(r.URL.Query().Get("page"), 1)
		limit, valid := positivePage(r.URL.Query().Get("limit"), 25)
		if !ok || !valid || !queryAllowed(r, "q", "page", "limit") {
			fail(w, 400, "Invalid category listing")
			return true
		}
		result, err := s.store.TaxonomyPage(r.URL.Query().Get("q"), page, limit)
		if err != nil {
			directoryError(w, err)
		} else {
			reply(w, 200, result)
		}
	case len(parts) == 2 && r.Method == http.MethodPatch && queryAllowed(r):
		var patch store.ConfigurationPatch
		if decode(w, r, &patch) != nil {
			fail(w, 400, "Invalid category patch")
			return true
		}
		item, err := s.store.PatchTaxonomy(parts[1], patch)
		if err != nil {
			directoryError(w, err)
		} else {
			reply(w, 200, item)
		}
	case len(parts) <= 2:
		fail(w, 405, "Category method not allowed")
	default:
		fail(w, 404, "API endpoint not found")
	}
	return true
}
