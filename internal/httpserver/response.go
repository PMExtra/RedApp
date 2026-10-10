package httpserver

import (
	"net/http"
	"strconv"
	"time"
)

// writeOK writes a 200 JSON document.
func writeOK(w http.ResponseWriter, value any) { writeJSON(w, http.StatusOK, value) }

// writeRevision writes an editable resource with its ETag ("<revision>").
func writeRevision(w http.ResponseWriter, status int, revision int64, value any) {
	w.Header().Set("ETag", etag(revision))
	writeJSON(w, status, value)
}

// writeCreated writes a 201 response for a created resource. location is the
// admin API path of the resource; revision 0 omits the ETag (non-editable).
func writeCreated(w http.ResponseWriter, location string, revision int64, value any) {
	if location != "" {
		w.Header().Set("Location", location)
	}
	if revision > 0 {
		w.Header().Set("ETag", etag(revision))
	}
	writeJSON(w, http.StatusCreated, value)
}

// writeNoContent writes 204 for successful operations without a body.
func writeNoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

func etag(revision int64) string { return `"` + strconv.FormatInt(revision, 10) + `"` }

// optionalText maps an empty string to a JSON null.
func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// utcTime returns t in UTC, or nil when t is nil.
func utcTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	value := t.UTC()
	return &value
}
