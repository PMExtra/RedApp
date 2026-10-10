package httpserver

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/store"
)

func positivePage(raw string, fallback int) (int, bool) {
	if raw == "" {
		return fallback, true
	}
	n, e := strconv.Atoi(raw)
	return n, e == nil && n >= 1 && n <= 1000000000 && strconv.Itoa(n) == raw
}
func (s *Server) directoryList(w http.ResponseWriter, r *http.Request, parts []string) {
	table := len(parts) == 3 && r.URL.Query().Get("view") == "table"
	allowed := []string{"page", "limit", "q", "state"}
	if table {
		allowed = append(allowed, "view", "sort", "order", "lang")
	}
	if !queryAllowed(r, allowed...) {
		fail(w, 400, "Invalid directory query")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "current"
	}
	page, ok := positivePage(r.URL.Query().Get("page"), 1)
	limit, okLimit := positivePage(r.URL.Query().Get("limit"), 12)
	if !ok || !okLimit || limit > 100 || !utf8.ValidString(q) || utf8.RuneCountInString(q) > 128 || (state != "enabled" && state != "current" && state != "disabled" && state != "deleted") {
		fail(w, 400, "Invalid directory page or search")
		return
	}
	if len(parts) == 1 && parts[0] == "vendors" {
		value, err := s.store.DirectoryPage(page, limit, q, state)
		if err != nil {
			directoryError(w, err)
			return
		}
		reply(w, 200, value)
		return
	}
	vendor := ""
	if len(parts) == 3 {
		vendor = parts[1]
		if _, err := s.store.Vendor(vendor); err != nil {
			directoryError(w, err)
			return
		}
	}
	if table {
		s.applicationTable(w, r, vendor, page, limit, q, state)
		return
	}
	value, err := s.store.ApplicationPage(vendor, page, limit, q, state)
	if err != nil {
		directoryError(w, err)
		return
	}
	reply(w, 200, value)
}
func (s *Server) instructionsAPI(w http.ResponseWriter, r *http.Request, key string) {
	if !queryAllowed(r) {
		fail(w, 400, "Unexpected query parameters")
		return
	}
	app, err := s.store.Application(key)
	if err != nil {
		directoryError(w, err)
		return
	}
	if r.Method == http.MethodGet {
		value, err := s.store.Instructions(app.UID)
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
	var value store.Instructions
	if err = decodeLimit(w, r, &value, 128<<10); err != nil {
		fail(w, 400, "Invalid instructions")
		return
	}
	if r.Header.Get("If-Match") != "" {
		value.Revision, err = expectedRevision(r)
		if err != nil {
			fail(w, 400, "Invalid revision")
			return
		}
	}
	s.directoryMu.Lock()
	defer s.directoryMu.Unlock()
	value, err = s.store.SaveInstructions(key, value.Revision, value.LocalizedText)
	if err != nil {
		directoryError(w, err)
		return
	}
	entry, _ := s.registry.LookupAny(key)
	revisionReply(w, value.Revision, struct {
		store.Instructions
		EntityRevision int64 `json:"entity_revision"`
	}{value, entry.Revision})
}
