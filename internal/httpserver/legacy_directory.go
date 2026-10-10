package httpserver

import (
	"net/http"
	"strings"

	"github.com/PMExtra/RedApp/internal/auth"
)

// legacyDirectoryAdmin is the pre-contract dispatcher of migration package 1
// (directory, configuration, admin notes, categories, exchange). Return false
// unconditionally once the package is migrated, then delete this file
// together with legacy.go.
func (s *Server) legacyDirectoryAdmin(w http.ResponseWriter, r *http.Request, session auth.Session) bool {
	if s.exchangeAPI(w, r, session) || s.categoriesAPI(w, r) || s.directoryAPI(w, r) {
		return true
	}
	return s.legacyChannelTTL(w, r)
}

// legacyChannelTTL serves the removed /admin/api/apps/{key}/settings document
// (now the configuration path cache_ttl_seconds).
func (s *Server) legacyChannelTTL(w http.ResponseWriter, r *http.Request) bool {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/admin/api/"), "/")
	if len(parts) != 4 || parts[0] != "apps" || parts[3] != "settings" {
		return false
	}
	app := parts[1] + "/" + parts[2]
	entry, ok := s.registry.LookupAny(app)
	if !ok {
		problem(w, 404, "APPLICATION_NOT_FOUND", "Application not found")
		return true
	}
	if entry.Protocol == nil {
		fail(w, 404, "API endpoint not found")
		return true
	}
	if !queryAllowed(r) {
		fail(w, 400, "Invalid query")
		return true
	}
	switch r.Method {
	case http.MethodGet:
		ttl, rev := entry.Descriptor.DefaultChannelTTLSeconds, entry.Revision
		revisionReply(w, rev, map[string]any{"channel_ttl_seconds": ttl, "revision": rev})
	case http.MethodPut:
		rev, err := expectedRevision(r)
		if err != nil {
			problem(w, 400, "INVALID_REQUEST", err.Error())
			return true
		}
		var input struct {
			TTL int `json:"channel_ttl_seconds"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid settings")
			return true
		}
		if input.TTL < 1 || input.TTL > 86400 {
			fail(w, 400, "Channel TTL must be between 1 and 86400 seconds")
			return true
		}
		next, e := s.setDirectoryTTL(app, rev, input.TTL)
		if e != nil {
			settingsError(w, e)
			return true
		}
		revisionReply(w, next, map[string]any{"channel_ttl_seconds": input.TTL, "revision": next})
	default:
		fail(w, 405, "Method not allowed")
	}
	return true
}
