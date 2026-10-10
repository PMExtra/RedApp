package httpserver

import (
	"net/http"

	"github.com/PMExtra/RedApp/internal/store"
)

// Reached only through admin(), after session and CSRF validation.
func (s *Server) adminNotesAPI(w http.ResponseWriter, r *http.Request, kind, key string) {
	w.Header().Set("Cache-Control", "no-store")
	if !queryAllowed(r) {
		fail(w, 400, "Unexpected query parameters")
		return
	}
	if r.Method == http.MethodGet {
		value, err := s.store.AdminNotes(kind, key)
		if err != nil {
			directoryError(w, err)
			return
		}
		revisionReply(w, value.Revision, value)
		return
	}
	if r.Method != http.MethodPut {
		fail(w, 405, "Method not allowed")
		return
	}
	var value store.AdminNotes
	if err := decodeLimit(w, r, &value, 128<<10); err != nil {
		fail(w, 400, "Invalid notes")
		return
	}
	if r.Header.Get("If-Match") != "" {
		var err error
		value.Revision, err = expectedRevision(r)
		if err != nil {
			fail(w, 400, "Invalid revision")
			return
		}
	}
	s.directoryMu.Lock()
	defer s.directoryMu.Unlock()
	value, err := s.store.SaveAdminNotes(kind, key, value.Revision, value.Text)
	if err != nil {
		directoryError(w, err)
		return
	}
	revisionReply(w, value.Revision, value)
}
