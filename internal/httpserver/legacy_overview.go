package httpserver

import (
	"net/http"
	"strconv"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/store"
)

// legacyOverviewAdmin was the pre-contract dispatcher of migration package 3
// (HTTP cache administration, overview, events, history, settings). The
// package is migrated, so it serves nothing; delete this file together with
// legacy.go.
func (s *Server) legacyOverviewAdmin(w http.ResponseWriter, r *http.Request, app, endpoint, requestOrigin string) bool {
	return false
}

// sourceEntry selects a source epoch with the legacy ?source_epoch= query. It
// is only used by the pre-contract handlers in legacy_releases.go; delete it
// with them.
func (s *Server) sourceEntry(app string, r *http.Request) (application.Entry, error) {
	entry, ok := s.registry.LookupAny(app)
	if !ok {
		return entry, application.ErrNotFound
	}
	raw := r.URL.Query().Get("source_epoch")
	if raw == "" {
		return entry, nil
	}
	epoch, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || epoch < 1 || strconv.FormatInt(epoch, 10) != raw || entry.UID == "" {
		return entry, store.ErrInvalidDirectory
	}
	entry.SourceEpoch = epoch
	if _, err = s.store.Source(entry.StorageID()); err != nil {
		return entry, err
	}
	return entry, nil
}
