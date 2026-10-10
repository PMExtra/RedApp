package httpserver

import (
	"io/fs"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/identity"
)

// Layout of the frontend build (x-spa-routes.documents): two entry documents
// and the flat assets directory at the root of the bundle. Public routes use
// the public entry and every /admin page, including its 404 document, uses the
// admin entry; admin pages never fall back to the public document. Changing
// the build layout only touches these names.
const (
	publicSPADocument = "index.html"
	adminSPADocument  = "admin.html"
	buildAssetsDir    = "assets"
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
	"/admin/vendors/{vendor}/apps/{app}",
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
		name = adminSPADocument
	}
	body, err := fs.ReadFile(s.frontend, name)
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
// that reached a /{vendor} route: unknown admin pages get the admin document
// with 404; api, assets and health paths (and unknown /admin/api/ paths) get
// the router-level NOT_FOUND error. It reports whether the request was handled.
func (s *Server) reservedPath(w http.ResponseWriter, r *http.Request, vendor string) bool {
	switch {
	case reservedNotFound(r.URL.Path):
		s.reservedRouteError(w, r)
		return true
	case vendor == "admin":
		s.spaDocument(w, r, true, http.StatusNotFound)
		return true
	}
	return false
}

// reservedRouteError answers a reserved path that no operation serves with
// r.Method. ServeMux matches every GET path with the /{vendor} page and file
// routes, so its own 404/405 decision does not apply: the path exists only if
// an operation route matches it for some method (405 with Allow), otherwise it
// is NOT_FOUND.
func (s *Server) reservedRouteError(w http.ResponseWriter, r *http.Request) {
	var allow []string
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		probe := r.Clone(r.Context())
		probe.Method = method
		if _, pattern := s.mux.Handler(probe); pattern != "" && !strings.HasPrefix(pattern, "GET /{vendor}") {
			allow = append(allow, method)
			if method == http.MethodGet {
				allow = append(allow, http.MethodHead)
			}
		}
	}
	if len(allow) == 0 {
		s.fail(w, r, codeNotFound, nil, "No route matches this path")
		return
	}
	w.Header().Set("Allow", strings.Join(allow, ", "))
	s.fail(w, r, codeMethodNotAllowed, nil, "Method not allowed for this path")
}

// reservedNotFound reports whether path is under a reserved first segment
// whose unknown paths are the router-level NOT_FOUND: /admin/api/, /api/,
// /assets/ and /health/.
func reservedNotFound(path string) bool {
	if strings.HasPrefix(path, "/admin/api/") {
		return true
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	return first == "api" || first == "assets" || first == "health"
}

// getVendorPage serves the public document for a published vendor and the
// same document with 404 for anything else, so the SPA shows its not-found page.
func (s *Server) getVendorPage(w http.ResponseWriter, r *http.Request) {
	vendor := r.PathValue("vendor")
	if s.reservedPath(w, r, vendor) {
		return
	}
	if e := spaQueryValid(r); e != nil {
		s.writeError(w, r, e)
		return
	}
	status := http.StatusNotFound
	if identity.ValidVendor(vendor) {
		v, err := s.store.Vendor(vendor)
		if err != nil && !isNotFound(err) {
			s.writeError(w, r, storageError(err))
			return
		}
		if err == nil && v.Enabled && v.DeletedAt == nil {
			status = http.StatusOK
		}
	}
	s.spaDocument(w, r, false, status)
}

// getAppPage serves the public document for a published application and the
// same document with 404 for anything else.
func (s *Server) getAppPage(w http.ResponseWriter, r *http.Request) {
	if s.reservedPath(w, r, r.PathValue("vendor")) {
		return
	}
	status := http.StatusNotFound
	if _, ok := s.registry.Lookup(r.PathValue("vendor") + "/" + r.PathValue("app")); ok {
		status = http.StatusOK
	}
	s.spaDocument(w, r, false, status)
}
