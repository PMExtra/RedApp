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
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/config"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/history"
	"github.com/PMExtra/RedApp/internal/jsoncheck"
	"github.com/PMExtra/RedApp/internal/site"
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

// legacyAdmin serves unmigrated admin operations. Origin, session and CSRF are
// already enforced by the route chain.
func (s *Server) legacyAdmin(w http.ResponseWriter, r *http.Request) {
	info := requestState(r)
	requestOrigin := info.origin
	public := s.public.View(requestOrigin).EffectiveURL
	session := info.session
	path := r.URL.Path
	if s.exchangeAPI(w, r, session) {
		return
	}
	if s.categoriesAPI(w, r) {
		return
	}
	if s.directoryAPI(w, r) {
		return
	}
	app := ""
	endpoint := strings.TrimPrefix(path, "/admin/api/")
	if strings.HasPrefix(endpoint, "apps/") {
		p := strings.SplitN(strings.TrimPrefix(endpoint, "apps/"), "/", 3)
		if len(p) != 3 {
			fail(w, 404, "API endpoint not found")
			return
		}
		app = p[0] + "/" + p[1]
		if _, exists := s.registry.LookupAny(app); !exists {
			problem(w, 404, "APPLICATION_NOT_FOUND", "Application not found")
			return
		}
		endpoint = p[2]
		if s.hostedAPI(w, r, app, endpoint) {
			return
		}
		entry, _ := s.registry.LookupAny(app)
		if entry.Provider == application.Info || entry.Provider == application.Hosted {
			fail(w, 404, "Content applications do not provide distribution endpoints")
			return
		}
	}
	if endpoint == "history" {
		if !queryAllowed(r, "scope", "metric", "range") {
			fail(w, 400, "Invalid query")
			return
		}
		if app != "" && r.URL.Query().Has("scope") {
			fail(w, 400, "Application history does not accept scope")
			return
		}
		if app == "" && r.URL.Query().Get("scope") != "global" {
			fail(w, 400, "Global history requires scope=global")
			return
		}
	} else if r.Method == "GET" && (endpoint == "versions" || endpoint == "resources" || endpoint == "events") {
		allowed := []string{"limit", "cursor"}
		if endpoint != "events" {
			allowed = append(allowed, "page")
		}
		if endpoint == "resources" {
			allowed = append(allowed, "version")
		}
		if !queryAllowed(r, allowed...) {
			fail(w, 400, "Invalid query")
			return
		}
	} else if app != "" && (endpoint == "cache" || strings.HasPrefix(endpoint, "cache/") || strings.HasPrefix(endpoint, "cleanup/")) {
		allowed := []string{"source_epoch"}
		if r.Method == http.MethodGet && strings.HasSuffix(endpoint, "/items") {
			allowed = append(allowed, "cursor", "limit")
		}
		if !queryAllowed(r, allowed...) || r.URL.Query().Has("source_epoch") && r.URL.Query().Get("source_epoch") == "" {
			fail(w, 400, "Invalid source selection")
			return
		}
	} else if app != "" && r.Method == http.MethodGet && (strings.HasPrefix(endpoint, "retention/") || strings.HasPrefix(endpoint, "prewarm/")) && strings.HasSuffix(endpoint, "/items") {
		if !queryAllowed(r, "page", "limit") {
			fail(w, 400, "Invalid query")
			return
		}
	} else if !queryAllowed(r) {
		fail(w, 400, "Invalid query")
		return
	}
	if s.prewarmAPI(w, r, app, endpoint) {
		return
	}
	if s.retentionAPI(w, r, app, endpoint) {
		return
	}
	if s.cacheAPI(w, r, app, endpoint) {
		return
	}
	if r.Method == "GET" {
		switch endpoint {
		case "status":
			var result map[string]any
			var err error
			if app == "" {
				result, err = s.status(public)
			} else {
				result, err = s.appStatus(app, public)
			}
			if err != nil {
				fail(w, 503, "Failed to read status")
				return
			}
			for _, key := range []string{"resources", "events", "versions", "version_stats", "application_versions"} {
				delete(result, key)
			}
			reply(w, 200, result)
			return
		case "versions", "resources":
			if app == "" {
				break
			}
			s.applicationList(w, r, app, endpoint)
			return
		case "events":
			s.eventList(w, r, app)
			return
		case "history":
			if s.history == nil {
				fail(w, 503, "History unavailable")
				return
			}
			key, window := r.URL.Query().Get("metric"), r.URL.Query().Get("range")
			validMetric := false
			if app == "" {
				_, validMetric = history.Find(key)
			} else {
				for _, definition := range history.AppDefinitions() {
					if definition.Key == key {
						validMetric = true
						break
					}
				}
			}
			if !validMetric || (window != "24h" && window != "7d" && window != "30d") {
				fail(w, 400, "Invalid history metric or range")
				return
			}

			var series history.Series
			var err error
			if app == "" {
				series, err = s.history.Query(key, window, time.Now().UTC())
			} else {
				entry, _ := s.registry.LookupAny(app)
				series, err = s.history.QueryFor(entry.MetricsID(), key, window, time.Now().UTC())
			}
			if err != nil {
				fail(w, 503, "Failed to read metric history")
				return
			}
			reply(w, 200, series)
			return
		case "settings":
			if app == "" {
				break
			}
			entry, _ := s.registry.LookupAny(app)
			if entry.Protocol == nil {
				break
			}
			ttl, rev := entry.Descriptor.DefaultChannelTTLSeconds, entry.Revision
			revisionReply(w, rev, map[string]any{"channel_ttl_seconds": ttl, "revision": rev})
			return
		case "settings/site":
			if app != "" {
				break
			}
			v, err := site.LoadSnapshot(s.store)
			if err != nil {
				fail(w, 503, "Site settings unavailable")
				return
			}
			revisionReply(w, v.Revision, v)
			return
		case "settings/proxy":
			if app != "" || s.pool == nil {
				break
			}
			v := s.pool.Proxy().Redacted()
			revisionReply(w, v.Revision, v)
			return
		case "settings/public-url":
			if app != "" || s.public == nil {
				break
			}
			v := s.public.View(requestOrigin)
			revisionReply(w, v.Revision, v)
			return
		}
		fail(w, 404, "API endpoint not found")
		return
	}
	if r.Method == "PUT" {
		rev, err := expectedRevision(r)
		if err != nil {
			problem(w, 400, "INVALID_REQUEST", err.Error())
			return
		}
		switch endpoint {
		case "settings":
			if app == "" {
				break
			}
			entry, _ := s.registry.LookupAny(app)
			if entry.Protocol == nil {
				break
			}
			var input struct {
				TTL int `json:"channel_ttl_seconds"`
			}
			if decode(w, r, &input) != nil {
				fail(w, 400, "Invalid settings")
				return
			}
			if input.TTL < 1 || input.TTL > 86400 {
				fail(w, 400, "Channel TTL must be between 1 and 86400 seconds")
				return
			}
			next, e := s.setDirectoryTTL(app, rev, input.TTL)
			if e != nil {
				settingsError(w, e)
				return
			}
			revisionReply(w, next, map[string]any{"channel_ttl_seconds": input.TTL, "revision": next})
			return
		case "settings/site":
			if app != "" {
				break
			}
			var input site.Settings
			if decode(w, r, &input) != nil {
				fail(w, 400, "Invalid site settings")
				return
			}
			if input.Validate() != nil {
				fail(w, 400, "Invalid site settings")
				return
			}
			v, e := site.SaveCAS(s.store, input, rev)
			if e != nil {
				settingsError(w, e)
				return
			}
			revisionReply(w, v.Revision, v)
			return
		case "settings/proxy":
			if app != "" || s.pool == nil {
				break
			}
			var input distributor.ProxyUpdate
			if decode(w, r, &input) != nil {
				fail(w, 400, "Invalid proxy settings")
				return
			}
			if e := s.pool.SetProxy(input, rev); e != nil {
				if errors.Is(e, distributor.ErrInvalidProxySettings) {
					fail(w, 400, "Invalid proxy settings")
					return
				}
				settingsError(w, e)
				return
			}
			v := s.pool.Proxy().Redacted()
			revisionReply(w, v.Revision, v)
			return
		case "settings/public-url":
			if app != "" || s.public == nil {
				break
			}
			var input map[string]json.RawMessage
			if decode(w, r, &input) != nil || len(input) != 1 || input["override_url"] == nil {
				fail(w, 400, "Expected override_url")
				return
			}
			var value *string
			if json.Unmarshal(input["override_url"], &value) != nil {
				fail(w, 400, "Invalid public URL")
				return
			}
			if value != nil {
				clean, e := config.PublicURL(*value)
				if e != nil || clean == "" {
					fail(w, 400, "Invalid public URL")
					return
				}
			}
			_, e := s.public.Set(value, rev)
			if e != nil {
				settingsError(w, e)
				return
			}
			v := s.public.View(requestOrigin)
			revisionReply(w, v.Revision, v)
			return
		}
		fail(w, 404, "API endpoint not found")
		return
	}
	if r.Method != "POST" {
		fail(w, 405, "Method not allowed")
		return
	}
	switch endpoint {
	case "cleanup/preview":
		if app == "" {
			break
		}
		var input struct {
			Minimum string `json:"minimum_version"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid request")
			return
		}
		entry, e := s.sourceEntry(app, r)
		if e != nil {
			directoryError(w, e)
			return
		}
		views := s.downloads.Snapshot()
		ids, unknown, e := s.catalog.CandidatesForSource(app, entry.StorageID(), input.Minimum, views)
		if e != nil {
			fail(w, 400, "Invalid minimum version")
			return
		}
		job, e := s.downloads.Preview(entry.StorageID(), ids)
		if e != nil {
			fail(w, 503, "Failed to persist cleanup preview")
			return
		}
		reply(w, 200, map[string]any{"job": job, "logical_bytes": job.LogicalBytes, "reclaimable_blob_bytes": job.ReclaimableBlobBytes, "active": job.ActiveGenerations, "unknown_versions": unknown})
		return
	}
	if app != "" && strings.HasPrefix(endpoint, "cleanup/") && strings.HasSuffix(endpoint, "/execute") {
		p := strings.Split(endpoint, "/")
		if len(p) == 3 {
			entry, e := s.sourceEntry(app, r)
			if e != nil {
				directoryError(w, e)
				return
			}
			if e := s.downloads.Cleanup(entry.StorageID(), p[1]); e != nil {
				problem(w, 409, "CLEANUP_INVALID", "Cleanup preview is expired, has a different application, or execution failed")
				return
			}
			reply(w, 200, map[string]bool{"ok": true})
			return
		}
	}
	fail(w, 404, "API endpoint not found")
}
