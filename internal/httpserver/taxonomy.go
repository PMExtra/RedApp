package httpserver

import (
	"errors"
	"github.com/PMExtra/RedApp/internal/store"
	"net/http"
	"strings"
)

func (s *Server) taxonomyAPI(w http.ResponseWriter, r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/admin/api/")
	if path != "taxonomy" && !strings.HasPrefix(path, "taxonomy/") {
		return false
	}
	parts := strings.Split(path, "/")
	if len(parts) == 1 && r.Method == http.MethodGet {
		page, ok := positivePage(r.URL.Query().Get("page"), 1)
		limit, valid := positivePage(r.URL.Query().Get("limit"), 25)
		if !ok || !valid || !queryAllowed(r, "kind", "q", "page", "limit") {
			fail(w, 400, "Invalid taxonomy listing")
			return true
		}
		result, err := s.DB.TaxonomyPage(r.URL.Query().Get("kind"), r.URL.Query().Get("q"), page, limit)
		if err != nil {
			directoryError(w, err)
		} else {
			reply(w, 200, result)
		}
		return true
	}
	if !queryAllowed(r) || len(parts) < 2 || len(parts) > 3 || (parts[1] != "categories" && parts[1] != "tags") {
		fail(w, 400, "Invalid taxonomy endpoint")
		return true
	}
	switch {
	case len(parts) == 2 && r.Method == http.MethodPost:
		var input struct {
			ID   string              `json:"id"`
			Name store.LocalizedText `json:"name"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid taxonomy item")
			return true
		}
		item, err := s.DB.CreateTaxonomy(parts[1], input.ID, input.Name)
		if err != nil {
			directoryError(w, err)
		} else {
			reply(w, 200, item)
		}
	case len(parts) == 3 && r.Method == http.MethodPatch:
		var patch store.ConfigurationPatch
		if decode(w, r, &patch) != nil {
			fail(w, 400, "Invalid taxonomy patch")
			return true
		}
		item, err := s.DB.PatchTaxonomy(parts[1], parts[2], patch)
		if err != nil {
			directoryError(w, err)
		} else {
			reply(w, 200, item)
		}
	case len(parts) == 3 && r.Method == http.MethodDelete:
		var input struct {
			Revision int64 `json:"revision"`
		}
		if decode(w, r, &input) != nil || input.Revision < 1 {
			fail(w, 400, "Invalid taxonomy deletion")
			return true
		}
		err := s.DB.DeleteTaxonomy(parts[1], parts[2], input.Revision)
		var used *store.TaxonomyInUse
		if errors.As(err, &used) {
			reply(w, 409, map[string]any{"error": map[string]string{"code": "TAXONOMY_IN_USE", "message": "Taxonomy item is in use"}, "references": used.References})
		} else if errors.Is(err, store.ErrBuiltinTemplate) {
			problem(w, 409, "TAXONOMY_BUILTIN", "Built-in taxonomy items cannot be deleted")
		} else if err != nil {
			directoryError(w, err)
		} else {
			reply(w, 200, map[string]bool{"deleted": true})
		}
	default:
		fail(w, 405, "Taxonomy method not allowed")
	}
	return true
}
