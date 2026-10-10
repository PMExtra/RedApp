package httpserver

import (
	"errors"
	"net/http"
	"os"

	"github.com/PMExtra/RedApp/internal/store"
)

var healthOK = map[string]bool{"ok": true}

func (s *Server) getLiveness(w http.ResponseWriter, r *http.Request) { writeOK(w, healthOK) }

// getReadiness pings SQLite and creates then removes a file in the data
// directory; it never contacts an upstream.
func (s *Server) getReadiness(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		s.fail(w, r, codeNotReady, err, "Database is not ready")
		return
	}
	f, err := os.CreateTemp(s.dataDir, ".health-")
	if err != nil {
		s.fail(w, r, codeNotReady, err, "Data directory is not writable")
		return
	}
	name := f.Name()
	closeErr := f.Close()
	if err = errors.Join(closeErr, os.Remove(name)); err != nil {
		s.fail(w, r, codeNotReady, err, "Data directory is not writable")
		return
	}
	writeOK(w, healthOK)
}

func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }
