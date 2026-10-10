package httpserver

import (
	"errors"
	"net/http"
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

func (s *Server) cacheAPI(w http.ResponseWriter, r *http.Request, app, endpoint string) bool {
	if app == "" || endpoint != "sources" && endpoint != "cache" && !strings.HasPrefix(endpoint, "cache/") {
		return false
	}
	if endpoint == "sources" {
		if r.Method != http.MethodGet {
			fail(w, 405, "Method not allowed")
			return true
		}
		entry, found := s.registry.LookupAny(app)
		if !found || entry.DeletedAt != nil {
			fail(w, 404, "Application not found")
			return true
		}
		rows, err := s.store.Sources()
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
	if s.httpCache == nil {
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
			reply(w, 200, s.httpCache.CleanupStatus())
		}
		return true
	}
	if endpoint == "cache" && r.Method == http.MethodGet {
		rows, err := s.httpCache.ListEntry(entry)
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
			page, err := s.httpCache.PreviewItems(entry.StorageID(), parts[1], parts[2], r.URL.Query().Get("cursor"), limit)
			if err != nil {
				problem(w, 409, "PREVIEW_INVALID", "Preview expired or its selection is unavailable")
			} else {
				reply(w, 200, page)
			}
			return true
		}
		if len(parts) == 3 {
			job, err := s.httpCache.LookupPreview(entry.StorageID(), parts[1], parts[2])
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
		job, err := s.httpCache.PreviewCleanup(r.Context(), entry, input.Basis, input.Before, match)
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
		item, err := s.httpCache.Refresh(r.Context(), entry, input.Path)
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
		job, err := s.httpCache.PreviewRefresh(r.Context(), entry, input.Match)
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
		job, err := s.httpCache.ExecuteRefresh(r.Context(), entry, parts[2])
		if err != nil {
			problem(w, 409, "REFRESH_INVALID", "Refresh preview expired, is running, or its source changed")
		} else {
			reply(w, 200, map[string]any{"job": job})
		}
		return true
	}
	if len(parts) == 4 && parts[0] == "cache" && parts[1] == "cleanup" && parts[3] == "execute" {
		result, err := s.httpCache.ExecuteCleanup(r.Context(), entry, parts[2])
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
		if _, err = s.store.SaveHTTPPolicy(app, revision, config); err != nil {
			if errors.Is(err, cachepolicy.ErrInvalidPolicy) {
				fail(w, 400, err.Error())
			} else {
				directoryError(w, err)
			}
			return
		}
	}
	config, revision, err := s.store.ReadHTTPPolicy(app)
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
