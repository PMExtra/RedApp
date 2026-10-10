package httpserver

import (
	"net/http"

	"github.com/PMExtra/RedApp/internal/store"
)

func (s *Server) homepageAPI(w http.ResponseWriter, r *http.Request) {
	if !queryAllowed(r) {
		fail(w, 400, "Invalid query")
		return
	}
	if r.Method == http.MethodGet {
		value, err := s.store.HomepagePins()
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
	saved, err := s.store.SaveHomepagePins(value)
	if err != nil {
		directoryError(w, err)
	} else {
		revisionReply(w, saved.Revision, saved)
	}
}
