// Legacy admin layer: the pre-contract dispatcher and helpers that still serve
// the admin areas not yet migrated to api/openapi.yaml. It is registered
// through the route table (operations marked legacy) and the /admin/api/
// catch-all. Delete this file, legacyCatchAll and the legacy route flags once
// every admin area is migrated; new code must not call anything defined here.
package httpserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/jsoncheck"
	"github.com/PMExtra/RedApp/internal/store"
)

// legacyStatusCodes maps legacy fail() statuses to catalog codes so even
// unmigrated responses use codes from components.x-error-codes.
var legacyStatusCodes = map[int]errorCode{400: codeInvalidRequest, 401: codeAuthRequired, 403: codeCSRFRejected, 404: codeNotFound, 405: codeMethodNotAllowed, 409: codeRevisionConflict, 413: codePayloadTooLarge, 502: codeUpstreamUnavailable, 503: codeStorageUnavailable}

func reply(w http.ResponseWriter, status int, value any) { writeJSON(w, status, value) }

// problem writes a legacy error document; the request ID comes from the
// X-Request-Id header set by ServeHTTP.
func problem(w http.ResponseWriter, status int, code, message string) {
	retryable := status >= 500 || code == "DIRECTORY_DELETE_PENDING"
	if class, ok := errorCatalog[errorCode(code)]; ok {
		retryable = class.retryable
	}
	reply(w, status, errorBody{Error: errorDetail{Code: errorCode(code), Message: message, RequestID: w.Header().Get("X-Request-Id"), Retryable: retryable}})
}
func fail(w http.ResponseWriter, status int, message string) {
	problem(w, status, string(legacyStatusCodes[status]), message)
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	return decodeLimit(w, r, v, 8192)
}
func decodeLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("JSON request required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if err = jsoncheck.Unique(b); err != nil {
		return err
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		return err
	}
	var tail any
	if err = d.Decode(&tail); err != io.EOF {
		return errors.New("Unexpected trailing request data")
	}
	return nil
}
func queryAllowed(r *http.Request, allowed ...string) bool {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return false
	}
	for key, values := range q {
		found := false
		for _, k := range allowed {
			if key == k {
				found = true
				break
			}
		}
		if !found || len(values) != 1 {
			return false
		}
	}
	return true
}
func expectedRevision(r *http.Request) (int64, error) {
	s := r.Header.Get("If-Match")
	if s == "" {
		return 0, errors.New("If-Match revision is required")
	}
	v, err := strconv.ParseInt(strings.Trim(s, "\""), 10, 64)
	if err != nil || v < 0 {
		return 0, errors.New("Invalid If-Match revision")
	}
	return v, nil
}
func revisionReply(w http.ResponseWriter, revision int64, value any) {
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(revision, 10)))
	reply(w, 200, value)
}
func settingsError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrConflict) {
		problem(w, 409, "SETTINGS_REVISION_CONFLICT", "Settings changed; reload before saving")
	} else {
		fail(w, 503, "Unable to persist settings")
	}
}

// legacyAdmin serves unmigrated admin operations; Origin, session and CSRF are
// already enforced by the route chain. Each migration package owns one of the
// dispatchers below in its own file (legacy_directory.go, legacy_releases.go,
// legacy_overview.go) and empties it when its area is migrated, so this file
// does not need to change until it is deleted.
func (s *Server) legacyAdmin(w http.ResponseWriter, r *http.Request) {
	info := requestState(r)
	if s.legacyDirectoryAdmin(w, r, info.session) {
		return
	}
	app := ""
	endpoint := strings.TrimPrefix(r.URL.Path, "/admin/api/")
	if strings.HasPrefix(endpoint, "apps/") {
		p := strings.SplitN(strings.TrimPrefix(endpoint, "apps/"), "/", 3)
		if len(p) != 3 {
			fail(w, 404, "API endpoint not found")
			return
		}
		app = p[0] + "/" + p[1]
		entry, exists := s.registry.LookupAny(app)
		if !exists {
			problem(w, 404, "APPLICATION_NOT_FOUND", "Application not found")
			return
		}
		endpoint = p[2]
		if s.legacyHostedAdmin(w, r, app, endpoint) {
			return
		}
		if entry.Provider == application.Info || entry.Provider == application.Hosted {
			fail(w, 404, "Content applications do not provide distribution endpoints")
			return
		}
	}
	if !legacyQueryAllowed(w, r, app, endpoint) {
		return
	}
	if s.legacyReleaseAdmin(w, r, app, endpoint) {
		return
	}
	if s.legacyOverviewAdmin(w, r, app, endpoint, info.origin) {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPut && r.Method != http.MethodPost {
		fail(w, 405, "Method not allowed")
		return
	}
	fail(w, 404, "API endpoint not found")
}

// legacyQueryAllowed applies the pre-contract query rules of the admin API.
func legacyQueryAllowed(w http.ResponseWriter, r *http.Request, app, endpoint string) bool {
	ok := true
	message := "Invalid query"
	switch {
	case endpoint == "history":
		ok = queryAllowed(r, "scope", "metric", "range") && !(app != "" && r.URL.Query().Has("scope")) && !(app == "" && r.URL.Query().Get("scope") != "global")
	case r.Method == http.MethodGet && (endpoint == "versions" || endpoint == "resources" || endpoint == "events"):
		allowed := []string{"limit", "cursor"}
		if endpoint != "events" {
			allowed = append(allowed, "page")
		}
		if endpoint == "resources" {
			allowed = append(allowed, "version")
		}
		ok = queryAllowed(r, allowed...)
	case app != "" && (endpoint == "cache" || strings.HasPrefix(endpoint, "cache/") || strings.HasPrefix(endpoint, "cleanup/")):
		allowed := []string{"source_epoch"}
		if r.Method == http.MethodGet && strings.HasSuffix(endpoint, "/items") {
			allowed = append(allowed, "cursor", "limit")
		}
		ok = queryAllowed(r, allowed...) && !(r.URL.Query().Has("source_epoch") && r.URL.Query().Get("source_epoch") == "")
		message = "Invalid source selection"
	case app != "" && r.Method == http.MethodGet && (strings.HasPrefix(endpoint, "retention/") || strings.HasPrefix(endpoint, "prewarm/")) && strings.HasSuffix(endpoint, "/items"):
		ok = queryAllowed(r, "page", "limit")
	default:
		ok = queryAllowed(r)
	}
	if !ok {
		fail(w, 400, message)
	}
	return ok
}
