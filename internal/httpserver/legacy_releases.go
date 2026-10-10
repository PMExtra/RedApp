package httpserver

import (
	"net/http"
	"strings"
)

// Pre-contract dispatchers of migration package 2 (releases, version cleanup,
// retention, prewarm, hosted file administration). Return false
// unconditionally once the package is migrated, then delete this file
// together with legacy.go.

func (s *Server) legacyHostedAdmin(w http.ResponseWriter, r *http.Request, app, endpoint string) bool {
	return s.hostedAPI(w, r, app, endpoint)
}

func (s *Server) legacyReleaseAdmin(w http.ResponseWriter, r *http.Request, app, endpoint string) bool {
	if s.prewarmAPI(w, r, app, endpoint) || s.retentionAPI(w, r, app, endpoint) {
		return true
	}
	if app == "" {
		return false
	}
	if r.Method == http.MethodGet && (endpoint == "versions" || endpoint == "resources") {
		s.applicationList(w, r, app, endpoint)
		return true
	}
	if r.Method != http.MethodPost {
		return false
	}
	if endpoint == "cleanup/preview" {
		var input struct {
			Minimum string `json:"minimum_version"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid request")
			return true
		}
		entry, e := s.sourceEntry(app, r)
		if e != nil {
			directoryError(w, e)
			return true
		}
		views := s.downloads.Snapshot()
		ids, unknown, e := s.catalog.CandidatesForSource(app, entry.StorageID(), input.Minimum, views)
		if e != nil {
			fail(w, 400, "Invalid minimum version")
			return true
		}
		job, e := s.downloads.Preview(entry.StorageID(), ids)
		if e != nil {
			fail(w, 503, "Failed to persist cleanup preview")
			return true
		}
		reply(w, 200, map[string]any{"job": job, "logical_bytes": job.LogicalBytes, "reclaimable_blob_bytes": job.ReclaimableBlobBytes, "active": job.ActiveGenerations, "unknown_versions": unknown})
		return true
	}
	if p := strings.Split(endpoint, "/"); len(p) == 3 && p[0] == "cleanup" && p[2] == "execute" {
		entry, e := s.sourceEntry(app, r)
		if e != nil {
			directoryError(w, e)
			return true
		}
		if e := s.downloads.Cleanup(entry.StorageID(), p[1]); e != nil {
			problem(w, 409, "CLEANUP_INVALID", "Cleanup preview is expired, has a different application, or execution failed")
			return true
		}
		reply(w, 200, map[string]bool{"ok": true})
		return true
	}
	return false
}
