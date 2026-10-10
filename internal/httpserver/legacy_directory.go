package httpserver

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/PMExtra/RedApp/internal/auth"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
)

// legacyDirectoryAdmin was the pre-contract dispatcher of migration package 1
// (directory, configuration, admin notes, categories, exchange). The package
// is migrated; delete this file together with legacy.go.
func (s *Server) legacyDirectoryAdmin(w http.ResponseWriter, r *http.Request, session auth.Session) bool {
	return false
}

// directoryError and positivePage are pre-contract helpers still used by the
// legacy handlers of migration packages 2 and 3. New code maps errors with
// directoryFailure and reads pages with pageQuery.
func directoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errDeletePending), errors.Is(err, download.ErrTransfersActive):
		problem(w, 409, "DIRECTORY_DELETE_PENDING", "Deletion is not complete. This application is blocked while its tasks stop. Retry deletion; restarting also resumes it.")
	case errors.Is(err, store.ErrConflict):
		problem(w, 409, "DIRECTORY_REVISION_CONFLICT", "Configuration changed; reload before saving")
	case errors.Is(err, sql.ErrNoRows):
		problem(w, 404, "DIRECTORY_NOT_FOUND", "Vendor or application not found")
	case errors.Is(err, store.ErrBuiltinTemplate), errors.Is(err, store.ErrDirectoryExists), errors.Is(err, store.ErrDirectoryDeleted), errors.Is(err, store.ErrVendorHasApplications):
		problem(w, 409, "DIRECTORY_CONFLICT", err.Error())
	case errors.Is(err, store.ErrInvalidDirectory):
		problem(w, 400, "INVALID_DIRECTORY", err.Error())
	default:
		problem(w, 503, "DIRECTORY_UNAVAILABLE", "Unable to persist or load the application directory")
	}
}

func positivePage(raw string, fallback int) (int, bool) {
	if raw == "" {
		return fallback, true
	}
	n, e := strconv.Atoi(raw)
	return n, e == nil && n >= 1 && n <= 1000000000 && strconv.Itoa(n) == raw
}
