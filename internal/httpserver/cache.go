package httpserver

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/httpcache"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
)

// General HTTP owns its file path validation. The two identity segments stay
// canonical ASCII; only the relative file path permits ordinary URL escapes.
func (s *Server) generalFile(w http.ResponseWriter, r *http.Request) bool {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/", 3)
	if len(parts) != 3 {
		return false
	}
	entry, ok := s.Registry.Lookup(parts[0] + "/" + parts[1])
	if !ok || entry.Provider != application.HttpCache || parts[2] == "" {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		fail(w, 405, "File requests require GET or HEAD")
		return true
	}
	encoded := strings.ToLower(parts[2])
	path, err := url.PathUnescape(parts[2])
	if err != nil || strings.Contains(encoded, "%2f") || strings.Contains(encoded, "%5c") || r.URL.RawQuery != "" || r.URL.ForceQuery {
		problem(w, 400, "INVALID_PATH", "Invalid file path or query")
		return true
	}
	if _, err = entry.Upstream.RelativeURL(path); err != nil {
		problem(w, 400, "INVALID_PATH", "Invalid relative file path")
		return true
	}
	if s.HTTPCache == nil {
		fail(w, 503, "HTTP cache unavailable")
		return true
	}
	if err = s.DB.Add("requests", 1); err != nil {
		fail(w, 503, "Failed to record request")
		return true
	}
	r, finish, err := s.applicationResponse(w, r, entry.StorageID())
	if err != nil {
		fail(w, 404, "Application unavailable")
		return true
	}
	defer finish()
	receipt := &downloadReceipt{ResponseWriter: w}
	if err = s.HTTPCache.Serve(receipt, r, entry, path); err != nil {
		switch {
		case errors.Is(err, store.ErrSourceInactive):
			problem(w, 409, "SOURCE_CHANGED", "Application source changed; retry the request")
		case errors.Is(err, download.ErrReaderLimit), errors.Is(err, download.ErrWriterLimit):
			problem(w, 503, "TRANSFER_CAPACITY", "Transfer capacity is currently full")
		case errors.Is(err, httpcache.ErrFetchContended):
			problem(w, 503, "CACHE_CONTENDED", "Cached file kept changing; retry the request")
		default:
			fail(w, 502, "Unable to serve the requested file")
		}
	}
	if err == nil {
		s.finishDownload(receipt, r, entry.UID)
	}
	return true
}

func (s *Server) sourceEntry(app string, r *http.Request) (application.Entry, error) {
	entry, ok := s.Registry.LookupAny(app)
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
	if _, err = s.DB.Source(entry.StorageID()); err != nil {
		return entry, err
	}
	return entry, nil
}

func (s *Server) cacheAPI(w http.ResponseWriter, r *http.Request, app, endpoint string) bool {
	if app == "" || endpoint != "sources" && endpoint != "cache" && !strings.HasPrefix(endpoint, "cache/") {
		return false
	}
	if endpoint == "sources" {
		if r.Method != http.MethodGet {
			fail(w, 405, "Method not allowed")
			return true
		}
		entry, found := s.Registry.LookupAny(app)
		if !found || entry.DeletedAt != nil {
			fail(w, 404, "Application not found")
			return true
		}
		rows, err := s.DB.Sources()
		if err != nil {
			fail(w, 503, "Application sources unavailable")
			return true
		}
		type sourceView struct {
			Epoch          int64     `json:"epoch"`
			BaseURL        string    `json:"base_url"`
			BaseURLs       []string  `json:"base_urls"`
			SourceStrategy string    `json:"source_strategy"`
			Current        bool      `json:"current"`
			Active         bool      `json:"active"`
			CreatedAt      time.Time `json:"created_at"`
		}
		sources := []sourceView{}
		for _, source := range rows {
			if source.AppUID == entry.UID && entry.UID != "" {
				sources = append(sources, sourceView{Epoch: source.Epoch, BaseURL: source.BaseURL, BaseURLs: source.BaseURLs, SourceStrategy: source.SourceStrategy, Current: source.Epoch == entry.SourceEpoch, Active: source.Active, CreatedAt: source.CreatedAt})
			}
		}
		reply(w, 200, map[string]any{"sources": sources})
		return true
	}
	entry, err := s.sourceEntry(app, r)
	if err != nil {
		directoryError(w, err)
		return true
	}
	if entry.Provider != application.HttpCache {
		fail(w, 404, "HTTP cache is not supported by this application")
		return true
	}
	if s.HTTPCache == nil {
		fail(w, 503, "HTTP cache unavailable")
		return true
	}
	if endpoint == "cache/policy" || endpoint == "cache/match" || endpoint == "cache/cleanup/status" {
		if !queryAllowed(r) {
			fail(w, 400, "Unexpected query parameters")
			return true
		}
		switch endpoint {
		case "cache/policy":
			s.httpPolicy(w, r, app)
		case "cache/match":
			if r.Method != http.MethodPost {
				fail(w, 405, "Method not allowed")
				return true
			}
			var input struct {
				Match pathmatch.Spec `json:"match"`
				Path  string         `json:"path"`
			}
			if decode(w, r, &input) != nil || len(input.Path) > 4096 || pathmatch.ValidatePath(input.Path) != nil {
				fail(w, 400, "Expected a decoded application-relative path beginning with / and no query")
				return true
			}
			matcher, err := pathmatch.Compile(input.Match)
			if err != nil {
				fail(w, 400, err.Error())
				return true
			}
			reply(w, 200, map[string]any{"matches": matcher.Match(input.Path), "canonical_path": input.Path})
		case "cache/cleanup/status":
			if r.Method != http.MethodGet {
				fail(w, 405, "Method not allowed")
				return true
			}
			reply(w, 200, s.HTTPCache.CleanupStatus())
		}
		return true
	}
	if endpoint == "cache" && r.Method == http.MethodGet {
		rows, err := s.HTTPCache.ListEntry(entry)
		if err != nil {
			fail(w, 503, "Cache list unavailable")
		} else {
			reply(w, 200, map[string]any{"items": rows})
		}
		return true
	}
	parts := strings.Split(endpoint, "/")
	isRefresh := len(parts) >= 2 && parts[1] == "refresh"
	if isRefresh {
		allowed := []string{}
		if r.Method == http.MethodGet && len(parts) == 4 && parts[3] == "items" {
			allowed = append(allowed, "cursor", "limit")
		}
		if !queryAllowed(r, allowed...) {
			fail(w, 400, "Refresh operates on the active current source")
			return true
		}
	}
	if r.Method == http.MethodGet && len(parts) >= 3 && (parts[1] == "cleanup" || parts[1] == "refresh") {
		if len(parts) == 4 && parts[3] == "items" {
			limit := 25
			if raw := r.URL.Query().Get("limit"); raw != "" {
				limit, err = strconv.Atoi(raw)
				if err != nil || limit < 1 || limit > 100 || strconv.Itoa(limit) != raw {
					fail(w, 400, "Page size must be 1..100")
					return true
				}
			}
			page, err := s.HTTPCache.PreviewItems(entry.StorageID(), parts[1], parts[2], r.URL.Query().Get("cursor"), limit)
			if err != nil {
				problem(w, 409, "PREVIEW_INVALID", "Preview expired or its selection is unavailable")
			} else {
				reply(w, 200, page)
			}
			return true
		}
		if len(parts) == 3 {
			job, err := s.HTTPCache.LookupPreview(entry.StorageID(), parts[1], parts[2])
			if err != nil {
				problem(w, 409, "PREVIEW_INVALID", "Preview expired or its selection is unavailable")
			} else {
				reply(w, 200, map[string]any{"job": job})
			}
			return true
		}
	}
	if r.Method != http.MethodPost {
		fail(w, 405, "Method not allowed")
		return true
	}
	if endpoint == "cache/cleanup/preview" {
		var input struct {
			Basis  string          `json:"basis"`
			Before time.Time       `json:"before"`
			Match  *pathmatch.Spec `json:"match"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid cleanup selection")
			return true
		}
		match := pathmatch.Spec{Type: "glob", Pattern: "/"}
		if input.Match != nil {
			match = *input.Match
		}
		job, err := s.HTTPCache.PreviewCleanup(r.Context(), entry, input.Basis, input.Before, match)
		if err != nil {
			if errors.Is(err, httpcache.ErrPreviewBusy) {
				problem(w, 503, "PREVIEW_BUSY", "Too many previews are being built; retry shortly")
			} else if errors.Is(err, httpcache.ErrInvalidCleanup) || errors.Is(err, pathmatch.ErrInvalidPattern) {
				fail(w, 400, "Invalid cleanup pattern, basis or cutoff")
			} else {
				problem(w, 409, "CLEANUP_INVALID", "Source changed or cleanup preview is unavailable")
			}
		} else {
			reply(w, 200, map[string]any{"job": job})
		}
		return true
	}
	if endpoint == "cache/refresh" {
		var input struct {
			Path string `json:"path"`
		}
		if decode(w, r, &input) != nil || len(input.Path) > 4096 || pathmatch.ValidatePath(input.Path) != nil || input.Path == "/" {
			fail(w, 400, "Invalid application-relative resource path")
			return true
		}
		item, err := s.HTTPCache.Refresh(r.Context(), entry, input.Path)
		if err != nil {
			if errors.Is(err, download.ErrReaderLimit) || errors.Is(err, download.ErrWriterLimit) {
				problem(w, 503, "TRANSFER_CAPACITY", "Transfer capacity is currently full")
			} else if item.GenerationID != "" && item.Reason == "refresh_failed" {
				reply(w, 200, map[string]any{"item": item})
			} else {
				problem(w, 409, "REFRESH_INVALID", "Resource changed, is unavailable, or cannot be refreshed")
			}
		} else {
			reply(w, 200, map[string]any{"item": item})
		}
		return true
	}
	if endpoint == "cache/refresh/preview" {
		var input struct {
			Match pathmatch.Spec `json:"match"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid refresh pattern")
			return true
		}
		if _, err = pathmatch.Compile(input.Match); err != nil {
			fail(w, 400, err.Error())
			return true
		}
		job, err := s.HTTPCache.PreviewRefresh(r.Context(), entry, input.Match)
		if err != nil {
			if errors.Is(err, httpcache.ErrPreviewBusy) {
				problem(w, 503, "PREVIEW_BUSY", "Too many previews are being built; retry shortly")
			} else {
				problem(w, 409, "REFRESH_INVALID", "Source changed or refresh preview is unavailable")
			}
		} else {
			reply(w, 200, map[string]any{"job": job})
		}
		return true
	}
	if len(parts) == 4 && parts[1] == "refresh" && parts[3] == "execute" {
		job, err := s.HTTPCache.ExecuteRefresh(r.Context(), entry, parts[2])
		if err != nil {
			problem(w, 409, "REFRESH_INVALID", "Refresh preview expired, is running, or its source changed")
		} else {
			reply(w, 200, map[string]any{"job": job})
		}
		return true
	}
	if len(parts) == 4 && parts[0] == "cache" && parts[1] == "cleanup" && parts[3] == "execute" {
		result, err := s.HTTPCache.ExecuteCleanup(r.Context(), entry, parts[2])
		if err != nil {
			problem(w, 409, "CLEANUP_INVALID", "Cleanup preview expired, changed source, or execution failed")
		} else {
			reply(w, 200, map[string]any{"result": result})
		}
		return true
	}
	fail(w, 404, "Cache endpoint not found")
	return true
}

func (s *Server) httpPolicy(w http.ResponseWriter, r *http.Request, app string) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		fail(w, 405, "Method not allowed")
		return
	}
	if r.Method == http.MethodPut {
		revision, err := expectedRevision(r)
		if err != nil {
			fail(w, 400, "An If-Match application revision is required")
			return
		}
		config := cachepolicy.Empty()
		if decodeLimit(w, r, &config, 128<<10) != nil {
			fail(w, 400, "Invalid HTTP cache policy")
			return
		}
		s.directoryMu.Lock()
		defer s.directoryMu.Unlock()
		if _, err = s.DB.SaveHTTPPolicy(app, revision, config); err != nil {
			if errors.Is(err, cachepolicy.ErrInvalidPolicy) {
				fail(w, 400, err.Error())
			} else {
				directoryError(w, err)
			}
			return
		}
	}
	config, revision, err := s.DB.ReadHTTPPolicy(app)
	if err != nil {
		directoryError(w, err)
		return
	}
	w.Header().Set("ETag", `"`+strconv.FormatInt(revision, 10)+`"`)
	reply(w, 200, struct {
		Revision int64 `json:"revision"`
		cachepolicy.Config
	}{revision, config})
}
