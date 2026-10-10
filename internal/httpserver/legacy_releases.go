package httpserver

import "net/http"

// Pre-contract dispatchers of migration package 2 (releases, version cleanup,
// retention, prewarm, hosted file administration). The package is migrated:
// the route table serves every operation, so these never handle a request.
// Delete this file together with legacy.go.

func (s *Server) legacyHostedAdmin(http.ResponseWriter, *http.Request, string, string) bool {
	return false
}

func (s *Server) legacyReleaseAdmin(http.ResponseWriter, *http.Request, string, string) bool {
	return false
}
