package httpserver

import (
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

func publicVendor(v store.Vendor) map[string]any {
	return map[string]any{"id": v.ID, "name": v.Name, "description": v.Description, "icon": v.Icon, "localized_icons": v.LocalizedIcons}
}
func (s *Server) publicCatalogAPI(w http.ResponseWriter, r *http.Request, origin string) bool {
	path := r.URL.Path
	if path != "/api/catalog" && path != "/api/search" && path != "/api/home" && !strings.HasPrefix(path, "/api/vendors/") {
		return false
	}
	if r.Method != http.MethodGet {
		fail(w, 405, "Method not allowed")
		return true
	}
	if !queryAllowed(r, "q", "page", "limit", "vendor", "category") {
		fail(w, 400, "Invalid catalog query")
		return true
	}
	category := r.URL.Query().Get("category")
	if category != "" && !identity.ValidSlug(category) {
		fail(w, 400, "Invalid category")
		return true
	}
	taxonomy, categories, taxErr := s.DB.PublicTaxonomy()
	if taxErr != nil {
		fail(w, 503, "Directory unavailable")
		return true
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page, valid := positivePage(r.URL.Query().Get("page"), 1)
	limit, validLimit := positivePage(r.URL.Query().Get("limit"), 24)
	if !valid || !validLimit || limit > 100 || !utf8.ValidString(q) || utf8.RuneCountInString(q) > 128 {
		fail(w, 400, "Invalid catalog page or search")
		return true
	}
	if strings.HasPrefix(path, "/api/vendors/") {
		v, err := s.DB.Vendor(strings.TrimPrefix(path, "/api/vendors/"))
		if err != nil || !v.Enabled || v.DeletedAt != nil {
			fail(w, 404, "Vendor not found")
		} else {
			reply(w, 200, publicVendor(v))
		}
		return true
	}
	if path == "/api/home" {
		pins, err := s.DB.HomepagePins()
		if err != nil {
			fail(w, 503, "Homepage unavailable")
			return true
		}
		pinned := []map[string]any{}
		ranking := []map[string]any{}
		for _, key := range pins.Keys {
			if e, ok := s.Registry.Lookup(key); ok {
				item, err := s.publicApplicationWithTaxonomy(e, origin, taxonomy[e.UID])
				if err != nil {
					fail(w, 503, "Homepage unavailable")
					return true
				}
				pinned = append(pinned, item)
			}
		}
		scores, err := s.DB.DownloadRanking(time.Now(), 20)
		if err != nil {
			fail(w, 503, "Ranking unavailable")
			return true
		}
		keys := map[string]string{}
		for _, e := range s.Registry.Entries() {
			keys[e.UID] = e.Descriptor.ID
		}
		for _, score := range scores {
			if e, ok := s.Registry.Lookup(keys[score.UID]); ok {
				item, err := s.publicApplicationWithTaxonomy(e, origin, taxonomy[e.UID])
				if err != nil {
					fail(w, 503, "Ranking unavailable")
					return true
				}
				item["download_clients"] = score.Clients
				ranking = append(ranking, item)
			}
		}
		reply(w, 200, map[string]any{"pinned": pinned, "ranking": ranking, "window_hours": 168, "bucket_hours": 1})
		return true
	}
	if path == "/api/search" {
		results := []map[string]any{}
		if q == "" {
			reply(w, 200, map[string]any{"items": results})
			return true
		}
		rows, err := s.DB.DB.Query(`SELECT id,name_en,name_zh_cn,icon,icon_en,icon_zh_cn FROM vendors v WHERE v.enabled=1 AND v.deleted_at_s IS NULL AND (instr(lower(v.id),lower(?))>0 OR instr(lower(v.name_en),lower(?))>0 OR instr(lower(v.name_zh_cn),lower(?))>0) ORDER BY id LIMIT 4`, q, q, q)
		if err != nil {
			fail(w, 503, "Search unavailable")
			return true
		}
		for rows.Next() {
			var id, icon string
			var name, icons store.LocalizedText
			if err = rows.Scan(&id, &name.En, &name.ZhCN, &icon, &icons.En, &icons.ZhCN); err != nil {
				break
			}
			results = append(results, map[string]any{"kind": "vendor", "id": id, "name": name, "icon": icon, "localized_icons": icons, "url": "/" + id})
		}
		rowErr := rows.Err()
		rows.Close()
		if err != nil || rowErr != nil {
			fail(w, 503, "Search unavailable")
			return true
		}
		apps, err := s.DB.ApplicationPage("", 1, 6, q, "enabled")
		if err != nil {
			fail(w, 503, "Search unavailable")
			return true
		}
		for _, a := range apps.Items {
			if _, ok := s.Registry.Lookup(a.Key); ok {
				results = append(results, map[string]any{"kind": "app", "id": a.Key, "name": a.Name, "icon": a.Icon, "url": "/" + a.Key})
			}
		}
		reply(w, 200, map[string]any{"items": results})
		return true
	}
	vendor := r.URL.Query().Get("vendor")
	if vendor != "" {
		v, err := s.DB.Vendor(vendor)
		if err != nil || !v.Enabled || v.DeletedAt != nil {
			fail(w, 404, "Vendor not found")
			return true
		}
	}
	apps, err := s.DB.ApplicationCategoryPage(vendor, page, limit, q, category)
	if err != nil {
		fail(w, 503, "Catalog unavailable")
		return true
	}
	result := store.NewPage[map[string]any](apps.Page, apps.Limit, apps.Total)
	for _, a := range apps.Items {
		if e, ok := s.Registry.Lookup(a.Key); ok {
			item, err := s.publicApplicationWithTaxonomy(e, origin, taxonomy[e.UID])
			if err != nil {
				fail(w, 503, "Catalog unavailable")
				return true
			}
			result.Items = append(result.Items, item)
		}
	}
	reply(w, 200, struct {
		store.Page[map[string]any]
		Categories []store.TaxonomyLabel `json:"categories"`
	}{result, categories})
	return true
}
func (s *Server) homepageAPI(w http.ResponseWriter, r *http.Request) {
	if !queryAllowed(r) {
		fail(w, 400, "Invalid query")
		return
	}
	if r.Method == http.MethodGet {
		value, err := s.DB.HomepagePins()
		if err != nil {
			directoryError(w, err)
		} else {
			revisionReply(w, value.Revision, value)
		}
		return
	}
	if r.Method != http.MethodPut {
		fail(w, 405, "Method not allowed")
		return
	}
	var value store.HomepagePins
	if err := decodeLimit(w, r, &value, 16<<10); err != nil {
		fail(w, 400, "Invalid homepage selection")
		return
	}
	if r.Header.Get("If-Match") != "" {
		revision, err := expectedRevision(r)
		if err != nil {
			fail(w, 400, "Invalid revision")
			return
		}
		value.Revision = revision
	}
	s.directoryMu.Lock()
	defer s.directoryMu.Unlock()
	saved, err := s.DB.SaveHomepagePins(value)
	if err != nil {
		directoryError(w, err)
	} else {
		revisionReply(w, saved.Revision, saved)
	}
}
func (s *Server) templateAPI(w http.ResponseWriter, r *http.Request, key string) {
	template, ok, templateErr := s.DB.BoundApplicationTemplate(key)
	if templateErr != nil {
		directoryError(w, templateErr)
		return
	}
	if !ok {
		fail(w, 404, "No entity template matches this application")
		return
	}
	if !queryAllowed(r) {
		fail(w, 400, "Unexpected query")
		return
	}
	if r.Method == http.MethodGet {
		a, err := s.DB.Application(key)
		if err != nil {
			directoryError(w, err)
			return
		}
		instructions, err := s.DB.Instructions(a.UID)
		if err != nil {
			directoryError(w, err)
			return
		}
		groups := []string{"metadata", "icon", "instructions_en", "instructions_zh"}
		if a.Provider == template.Application.Provider {
			groups = append(groups, "source", "cache")
		}
		reply(w, 200, map[string]any{"template": template, "current": a, "instructions": instructions, "groups": groups})
		return
	}
	if r.Method != http.MethodPost {
		fail(w, 405, "Method not allowed")
		return
	}
	var input store.TemplateReset
	if err := decode(w, r, &input); err != nil {
		fail(w, 400, "Invalid template reset")
		return
	}
	s.directoryMu.Lock()
	defer s.directoryMu.Unlock()
	a, err := s.DB.ResetApplicationTemplate(key, input)
	if err != nil {
		directoryError(w, err)
	} else {
		reply(w, 200, map[string]any{"app": a})
	}
}

func (s *Server) vendorTemplateAPI(w http.ResponseWriter, r *http.Request, id string) {
	template, ok, templateErr := s.DB.BoundVendorTemplate(id)
	if templateErr != nil {
		directoryError(w, templateErr)
		return
	}
	if !ok {
		fail(w, 404, "No entity template matches this vendor")
		return
	}
	if !queryAllowed(r) {
		fail(w, 400, "Unexpected query")
		return
	}
	if r.Method == http.MethodGet {
		v, err := s.DB.Vendor(id)
		if err != nil {
			directoryError(w, err)
		} else {
			reply(w, 200, map[string]any{"template": map[string]any{"application": template}, "current": v, "instructions": store.Instructions{}, "groups": []string{"metadata", "icon"}})
		}
		return
	}
	if r.Method != http.MethodPost {
		fail(w, 405, "Method not allowed")
		return
	}
	var input store.TemplateReset
	if err := decode(w, r, &input); err != nil {
		fail(w, 400, "Invalid template reset")
		return
	}
	s.directoryMu.Lock()
	defer s.directoryMu.Unlock()
	v, err := s.DB.ResetVendorTemplate(id, input)
	if err != nil {
		directoryError(w, err)
	} else {
		reply(w, 200, map[string]any{"vendor": v})
	}
}
