package httpserver

import (
	"io/fs"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/identity"
)

// SPA documents of the embedded frontend build. Public routes use the public
// entry and /admin routes the admin entry. Until a bundle contains the admin
// entry, admin routes fall back to the public document (the pre-rewrite
// bundle is one SPA for both). Changing the build layout only touches these
// names.
const (
	publicSPADocument = "web/index.html"
	adminSPADocument  = "web/admin.html"
)

const spaPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'"

// adminSPARoutes are the /admin entries of x-spa-routes in api/openapi.yaml
// (TestSPARoutesMatchSpec). ServeMux registers each one for getAdminPage.
var adminSPARoutes = []string{
	"/admin/login",
	"/admin/overview",
	"/admin/events",
	"/admin/settings/site",
	"/admin/settings/proxy",
	"/admin/vendors",
	"/admin/vendors/new",
	"/admin/categories",
	"/admin/vendors/{vendor}",
	"/admin/vendors/{vendor}/settings",
	"/admin/vendors/{vendor}/apps",
	"/admin/vendors/{vendor}/apps/new",
	"/admin/vendors/{vendor}/admin-notes",
	"/admin/vendors/{vendor}/apps/{app}/settings",
	"/admin/vendors/{vendor}/apps/{app}/admin-notes",
	"/admin/vendors/{vendor}/apps/{app}/versions",
	"/admin/vendors/{vendor}/apps/{app}/cache",
	"/admin/vendors/{vendor}/apps/{app}/files",
}

// spaDocument writes an SPA entry with the strict document policy.
func (s *Server) spaDocument(w http.ResponseWriter, r *http.Request, admin bool, status int) {
	name := publicSPADocument
	if admin {
		if _, err := fs.Stat(web, adminSPADocument); err == nil {
			name = adminSPADocument
		}
	}
	body, err := web.ReadFile(name)
	if err != nil {
		s.fail(w, r, codeInternalError, err, "Frontend bundle is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", spaPolicy)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// spaQueryValid checks the values of the SPA query parameters; the route
// allow-list has already rejected unknown names.
func spaQueryValid(r *http.Request) *apiError {
	limits := map[string]int{"returnTo": 2048, "q": 128, "page": 10, "category": 63}
	q := r.URL.Query()
	for name, limit := range limits {
		if v := q.Get(name); !utf8.ValidString(v) || utf8.RuneCountInString(v) > limit {
			return newError(codeInvalidQuery, nil, name+" is too long")
		}
	}
	if v := q.Get("state"); v != "" && v != "current" && v != "enabled" && v != "disabled" && v != "deleted" {
		return newError(codeInvalidQuery, nil, "state must be current, enabled, disabled or deleted")
	}
	if v := q.Get("cleanup"); v != "" && v != "pending" {
		return newError(codeInvalidQuery, nil, "cleanup must be pending")
	}
	return nil
}

func (s *Server) getHomePage(w http.ResponseWriter, r *http.Request) {
	s.spaDocument(w, r, false, http.StatusOK)
}

func (s *Server) getCatalogPage(w http.ResponseWriter, r *http.Request) {
	if e := spaQueryValid(r); e != nil {
		s.writeError(w, r, e)
		return
	}
	s.spaDocument(w, r, false, http.StatusOK)
}

func (s *Server) redirectAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Location", "/admin/overview")
	w.WriteHeader(http.StatusTemporaryRedirect)
}

// getAdminPage serves a registered admin SPA route. Vendor and application
// segments must exist (enabled or not) and tabs must match the provider's
// capabilities; otherwise the document is served with 404.
func (s *Server) getAdminPage(w http.ResponseWriter, r *http.Request) {
	if e := spaQueryValid(r); e != nil {
		s.writeError(w, r, e)
		return
	}
	status := http.StatusOK
	if !s.adminRouteExists(r) {
		status = http.StatusNotFound
	}
	s.spaDocument(w, r, true, status)
}

func (s *Server) adminRouteExists(r *http.Request) bool {
	vendor, app := r.PathValue("vendor"), r.PathValue("app")
	if vendor == "" {
		return true
	}
	if app == "" {
		_, err := s.store.Vendor(vendor)
		return err == nil
	}
	e, ok := s.registry.LookupAny(vendor + "/" + app)
	if !ok {
		return false
	}
	switch r.Pattern[strings.LastIndexByte(r.Pattern, '/')+1:] {
	case "files":
		return e.Provider == application.Hosted
	case "cache":
		return e.Provider != application.Info && e.Provider != application.Hosted
	case "versions":
		return e.Protocol != nil
	}
	return true
}

// reservedPath handles requests whose first segment is a reserved vendor ID
// (admin, api, assets, health, all) that reached a /{vendor} route: unknown
// admin pages get the admin document with 404, API paths an Error document.
// It reports whether the request was handled.
func (s *Server) reservedPath(w http.ResponseWriter, r *http.Request, vendor string) bool {
	if identity.ValidVendor(vendor) {
		return false
	}
	if vendor == "admin" && !strings.HasPrefix(r.URL.Path, "/admin/api/") {
		s.spaDocument(w, r, true, http.StatusNotFound)
		return true
	}
	if identity.ValidSlug(vendor) {
		s.fail(w, r, codeNotFound, nil, "No route matches this path")
		return true
	}
	return false
}

func (s *Server) getVendorPage(w http.ResponseWriter, r *http.Request) {
	vendor := r.PathValue("vendor")
	if s.reservedPath(w, r, vendor) {
		return
	}
	if !identity.ValidVendor(vendor) {
		s.fail(w, r, codeVendorNotFound, nil, "Vendor not found")
		return
	}
	if e := spaQueryValid(r); e != nil {
		s.writeError(w, r, e)
		return
	}
	if _, ok := s.publishedVendor(w, r, vendor); ok {
		s.spaDocument(w, r, false, http.StatusOK)
	}
}

func (s *Server) getAppPage(w http.ResponseWriter, r *http.Request) {
	if s.reservedPath(w, r, r.PathValue("vendor")) {
		return
	}
	if _, ok := s.publishedApp(w, r); ok {
		s.spaDocument(w, r, false, http.StatusOK)
	}
}
