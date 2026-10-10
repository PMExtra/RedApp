package httpserver

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/httpcache"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
)

// managedApp resolves {vendor}/{app} of an administrator route to any
// application, including disabled and deleted ones, or writes INVALID_PATH /
// APPLICATION_NOT_FOUND.
func (s *Server) managedApp(w http.ResponseWriter, r *http.Request) (application.Entry, bool) {
	key := r.PathValue("vendor") + "/" + r.PathValue("app")
	if _, err := application.ParseKey(key); err != nil {
		s.fail(w, r, codeInvalidPath, nil, "Invalid application identity")
		return application.Entry{}, false
	}
	e, ok := s.registry.LookupAny(key)
	if ok && e.DeletedAt != nil {
		// Purging a deleted application publishes no new registry, so its
		// entry outlives its data; only the stored row proves it still exists.
		row, err := s.store.Application(key)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			s.writeError(w, r, storageError(err))
			return e, false
		}
		ok = err == nil && row.UID == e.UID
	}
	if !ok {
		s.fail(w, r, codeApplicationNotFound, nil, "Application not found")
	}
	return e, ok
}

// managedAppWith is managedApp restricted to providers; other applications get
// CAPABILITY_UNSUPPORTED.
func (s *Server) managedAppWith(w http.ResponseWriter, r *http.Request, providers ...string) (application.Entry, bool) {
	e, ok := s.managedApp(w, r)
	if !ok {
		return e, false
	}
	for _, provider := range providers {
		if e.Provider == provider {
			return e, true
		}
	}
	s.fail(w, r, codeCapabilityUnsupported, nil, "This application does not support this endpoint")
	return e, false
}

// ---------------------------------------------------------------- sources

type sourceEpochDTO struct {
	Epoch          int64     `json:"epoch"`
	BaseURL        *string   `json:"base_url,omitempty"`
	BaseURLs       []string  `json:"base_urls,omitempty"`
	SourceStrategy *string   `json:"source_strategy,omitempty"`
	Current        bool      `json:"current"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"created_at"`
}

type sourceListDTO struct {
	Items []sourceEpochDTO `json:"items"`
}

func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.managedAppWith(w, r, application.HttpCache, application.Codex, application.ClaudeCode)
	if !ok {
		return
	}
	rows, err := s.store.Sources()
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	out := sourceListDTO{Items: []sourceEpochDTO{}}
	for _, row := range rows {
		if entry.UID == "" || row.AppUID != entry.UID {
			continue
		}
		item := sourceEpochDTO{Epoch: row.Epoch, Current: row.Epoch == entry.SourceEpoch, Active: row.Active, CreatedAt: row.CreatedAt.UTC()}
		if entry.Provider == application.HttpCache {
			strategy := row.SourceStrategy
			item.BaseURLs, item.SourceStrategy = append([]string{}, row.BaseURLs...), &strategy
		} else {
			base := row.BaseURL
			item.BaseURL = &base
		}
		out.Items = append(out.Items, item)
	}
	writeOK(w, out)
}

// sourceEpochEntry returns entry bound to epoch (the current epoch when zero),
// or SOURCE_NOT_FOUND when the application never had that epoch.
func (s *Server) sourceEpochEntry(entry application.Entry, epoch int64) (application.Entry, *apiError) {
	if epoch == 0 || epoch == entry.SourceEpoch {
		return entry, nil
	}
	entry.SourceEpoch = epoch
	if _, err := s.store.Source(entry.StorageID()); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrInvalidDirectory) {
			return entry, newError(codeSourceNotFound, nil, "source_epoch does not exist for this application")
		}
		return entry, storageError(err)
	}
	return entry, nil
}

// ---------------------------------------------------------------- entries

type cacheEntryDTO struct {
	GenerationID string     `json:"generation_id"`
	Path         string     `json:"path"`
	SizeBytes    int64      `json:"size_bytes"`
	SHA256       string     `json:"sha256"`
	ETag         *string    `json:"etag"`
	SourceURL    string     `json:"source_url"`
	FetchedAt    time.Time  `json:"fetched_at"`
	ValidatedAt  time.Time  `json:"validated_at"`
	LastAccessAt *time.Time `json:"last_access_at"`
	FreshUntil   time.Time  `json:"fresh_until"`
}

type cacheEntryPageDTO struct {
	Items      []cacheEntryDTO `json:"items"`
	NextCursor *string         `json:"next_cursor"`
}

func cacheEntryDocument(row httpcache.Row) cacheEntryDTO {
	out := cacheEntryDTO{GenerationID: row.GenerationID, Path: "/" + row.Path, SizeBytes: row.SizeBytes, SHA256: row.SHA256, SourceURL: row.SourceURL, FetchedAt: row.FetchedAt.UTC(), ValidatedAt: row.ValidatedAt.UTC(), FreshUntil: row.FreshUntil.UTC()}
	if row.ETag != "" {
		etag := row.ETag
		out.ETag = &etag
	}
	if row.LastAccessAt != nil {
		at := row.LastAccessAt.UTC()
		out.LastAccessAt = &at
	}
	return out
}

// entryCursorPrefixBytes bounds the path stored in a cache entry cursor so the
// token stays within maxCursorLength for paths of up to 4096 bytes.
const entryCursorPrefixBytes = 768

func (s *Server) listCacheEntries(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.managedAppWith(w, r, application.HttpCache)
	if !ok {
		return
	}
	var epoch int64
	if raw := r.URL.Query().Get("source_epoch"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 || strconv.FormatInt(n, 10) != raw {
			s.fail(w, r, codeInvalidQuery, nil, "source_epoch must be a positive integer")
			return
		}
		epoch = n
	}
	limit, e := queryInt(r, "limit", 50, 1, 100)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	entry, e = s.sourceEpochEntry(entry, epoch)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	scope := cursorScope(entry.UID, strconv.FormatInt(entry.SourceEpoch, 10))
	cursor, e := decodePageCursor(r, "listCacheEntries", scope)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	var after httpcache.EntryAfter
	if cursor != nil {
		after = httpcache.EntryAfter{Path: string(cursor.Last), Prefix: cursor.Prefix, Skip: cursor.Skip}
	}
	rows, err := s.httpCache.ListEntryPage(entry, after, limit+1)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	page := cacheEntryPageDTO{Items: []cacheEntryDTO{}}
	if len(rows) > limit {
		rows = rows[:limit]
		page.NextCursor = encodePageCursor(nextEntryCursor(scope, after, rows))
	}
	for _, row := range rows {
		page.Items = append(page.Items, cacheEntryDocument(row))
	}
	writeOK(w, page)
}

// nextEntryCursor positions after the last row of a page. Long paths are
// replaced by a bounded prefix and the number of listed rows sharing it.
func nextEntryCursor(scope string, after httpcache.EntryAfter, rows []httpcache.Row) pageCursor {
	last := rows[len(rows)-1].Path
	if len(last) <= entryCursorPrefixBytes {
		return pageCursor{Operation: "listCacheEntries", Scope: scope, Last: []byte(last)}
	}
	cut := entryCursorPrefixBytes
	for cut > 0 && !utf8.RuneStart(last[cut]) {
		cut--
	}
	prefix := last[:cut]
	skip := 0
	if after.Prefix && after.Path == prefix {
		skip = after.Skip
	}
	for _, row := range rows {
		if strings.HasPrefix(row.Path, prefix) {
			skip++
		}
	}
	return pageCursor{Operation: "listCacheEntries", Scope: scope, Last: []byte(prefix), Prefix: true, Skip: skip}
}

// ---------------------------------------------------------------- single refresh

type cacheRefreshItemDTO struct {
	Path         string  `json:"path"`
	GenerationID *string `json:"generation_id"`
	Status       string  `json:"status"`
	Reason       *string `json:"reason"`
}

// cacheWriteEntry is the current, active source of an HTTP cache application
// for refresh and refresh previews; deleted applications are read-only.
func (s *Server) cacheWriteEntry(w http.ResponseWriter, r *http.Request) (application.Entry, bool) {
	entry, ok := s.managedAppWith(w, r, application.HttpCache)
	if ok && entry.DeletedAt != nil {
		s.fail(w, r, codeEntityDeleted, nil, "The application is deleted and read-only")
		return entry, false
	}
	return entry, ok
}

// activeCacheEntry is cacheWriteEntry for work on the current, active source
// (refresh); a disabled application has no active source.
func (s *Server) activeCacheEntry(w http.ResponseWriter, r *http.Request) (application.Entry, bool) {
	entry, ok := s.cacheWriteEntry(w, r)
	if ok && !entry.Active() {
		s.fail(w, r, codeSourceChanged, store.ErrSourceInactive, "The application source is not active; enable the application to refresh")
		return entry, false
	}
	return entry, ok
}

func (s *Server) refreshCacheEntry(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.activeCacheEntry(w, r)
	if !ok {
		return
	}
	var input struct {
		Path string `json:"path"`
	}
	if e := decodeJSON(r, &input); e != nil {
		s.writeError(w, r, e)
		return
	}
	if len(input.Path) > 4096 || input.Path == "/" || pathmatch.ValidatePath(input.Path) != nil {
		s.fail(w, r, codeValidationFailed, nil, "path must be a decoded application-relative file path starting with /")
		return
	}
	item, err := s.httpCache.Refresh(r.Context(), entry, input.Path)
	switch {
	case errors.Is(err, httpcache.ErrInvalidRefresh):
		s.fail(w, r, codeValidationFailed, err, "path cannot be mapped to the application's source")
	case errors.Is(err, httpcache.ErrRefreshMissing):
		s.fail(w, r, codeCacheEntryNotFound, nil, "The path is not cached in the current source epoch")
	case errors.Is(err, download.ErrReaderLimit), errors.Is(err, download.ErrWriterLimit):
		s.fail(w, r, codeTransferCapacity, err, "Transfer capacity is currently full; retry later")
	case errors.Is(err, store.ErrSourceInactive):
		s.fail(w, r, codeSourceChanged, err, "The application source is not active or changed during the request")
	case errors.Is(err, httpcache.ErrClosed), errors.Is(err, context.Canceled):
		s.writeError(w, r, storageError(err))
	case errors.Is(err, httpcache.ErrFetchContended):
		item.Status, item.Reason = "failed", "generation_changed"
		writeOK(w, refreshItemDocument(item))
	case err != nil && (item.Reason != "" || errors.Is(err, httpcache.ErrUpstream)):
		// Upstream failures are an outcome of the refresh, not an error response.
		s.log.Info("cache refresh failed", "request_id", requestState(r).id, "error", redactError(err))
		item.Status = "failed"
		writeOK(w, refreshItemDocument(item))
	case err != nil:
		s.writeError(w, r, storageError(err))
	default:
		writeOK(w, refreshItemDocument(item))
	}
}

func refreshItemDocument(item httpcache.RefreshItem) cacheRefreshItemDTO {
	return cacheRefreshItemDTO{Path: item.Path, GenerationID: optionalText(item.GenerationID), Status: item.Status, Reason: optionalText(item.Reason)}
}

// ---------------------------------------------------------------- previews

type cleanupResultDTO struct {
	Kind string `json:"kind"`
	httpcache.CleanupResult
}

type refreshSummaryDTO struct {
	Kind string `json:"kind"`
	httpcache.RefreshSummary
}

type maintenancePreviewDTO struct {
	ID             string         `json:"id"`
	Kind           string         `json:"kind"`
	State          string         `json:"state"`
	SourceEpoch    int64          `json:"source_epoch"`
	Match          pathmatch.Spec `json:"match"`
	Basis          *string        `json:"basis"`
	Before         *time.Time     `json:"before"`
	CreatedAt      time.Time      `json:"created_at"`
	ExpiresAt      time.Time      `json:"expires_at"`
	ScannedFiles   int            `json:"scanned_files"`
	SelectedFiles  int            `json:"selected_files"`
	SelectedBytes  int64          `json:"selected_bytes"`
	ActiveFiles    int            `json:"active_files"`
	CompletedFiles int            `json:"completed_files"`
	FailedFiles    int            `json:"failed_files"`
	Result         any            `json:"result"`
}

func maintenanceDocument(p httpcache.MaintenancePreview) maintenancePreviewDTO {
	out := maintenancePreviewDTO{ID: p.ID, Kind: p.Kind, State: p.State, SourceEpoch: p.SourceEpoch(), Match: p.Match, CreatedAt: p.CreatedAt.UTC(), ExpiresAt: p.ExpiresAt.UTC(), ScannedFiles: p.ScannedFiles, SelectedFiles: p.SelectedFiles, SelectedBytes: p.SelectedBytes, ActiveFiles: p.ActiveFiles, CompletedFiles: p.CompletedFiles, FailedFiles: p.FailedFiles}
	if p.Kind == "cleanup" {
		basis := p.Basis
		before := p.Before.UTC()
		out.Basis, out.Before = &basis, &before
		var result httpcache.CleanupResult
		if strictResult(p.Result, &result) {
			out.Result = cleanupResultDTO{Kind: "cleanup", CleanupResult: result}
		}
	} else {
		var result httpcache.RefreshSummary
		if strictResult(p.Result, &result) {
			out.Result = refreshSummaryDTO{Kind: "refresh", RefreshSummary: result}
		}
	}
	return out
}

// strictResult decodes a stored receipt. Diagnostic receipts of interrupted or
// failed builds have other fields and are reported as no result.
func strictResult(raw json.RawMessage, out any) bool {
	if len(raw) == 0 {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(out) == nil
}

// previewBuildError maps a failed refresh or cleanup preview build.
func previewBuildError(err error) *apiError {
	switch {
	case errors.Is(err, httpcache.ErrPreviewBusy):
		return newError(codePreviewBusy, nil, "Eight maintenance previews are being built; retry shortly")
	case errors.Is(err, httpcache.ErrInvalidCleanup), errors.Is(err, pathmatch.ErrInvalidPattern):
		return newError(codeValidationFailed, err, "Invalid pattern, basis or cutoff")
	case errors.Is(err, store.ErrSourceInactive):
		return newError(codeSourceChanged, err, "The application source is not active or changed during the request; retry")
	default:
		return storageError(err)
	}
}

func (s *Server) previewCacheRefresh(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.activeCacheEntry(w, r)
	if !ok {
		return
	}
	var input struct {
		Match *pathmatch.Spec `json:"match"`
	}
	if e := decodeJSON(r, &input); e != nil {
		s.writeError(w, r, e)
		return
	}
	if input.Match == nil {
		s.fail(w, r, codeInvalidRequest, nil, "match is required")
		return
	}
	if _, err := pathmatch.Compile(*input.Match); err != nil {
		s.fail(w, r, codeValidationFailed, nil, "match: "+matchProblem(err))
		return
	}
	preview, err := s.httpCache.PreviewRefresh(r.Context(), entry, *input.Match)
	if err != nil {
		s.writeError(w, r, previewBuildError(err))
		return
	}
	writeCreated(w, "", 0, maintenanceDocument(preview))
}

// matchProblem is the user-facing reason of a pathmatch compile error.
func matchProblem(err error) string {
	return strings.TrimPrefix(err.Error(), pathmatch.ErrInvalidPattern.Error()+": ")
}

func (s *Server) previewCacheCleanup(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.cacheWriteEntry(w, r)
	if !ok {
		return
	}
	var input struct {
		Match       *pathmatch.Spec `json:"match"`
		Basis       *string         `json:"basis"`
		Before      *time.Time      `json:"before"`
		SourceEpoch *int64          `json:"source_epoch"`
	}
	if e := decodeJSON(r, &input); e != nil {
		s.writeError(w, r, e)
		return
	}
	if input.Basis == nil || input.Before == nil {
		s.fail(w, r, codeInvalidRequest, nil, "basis and before are required")
		return
	}
	match := pathmatch.Spec{Type: "glob", Pattern: "/"}
	if input.Match != nil {
		match = *input.Match
	}
	switch {
	case *input.Basis != "fetched_at" && *input.Basis != "last_access":
		s.fail(w, r, codeValidationFailed, nil, "basis must be fetched_at or last_access")
		return
	case input.Before.After(time.Now()):
		s.fail(w, r, codeValidationFailed, nil, "before must not be in the future")
		return
	case input.SourceEpoch != nil && *input.SourceEpoch < 1:
		s.fail(w, r, codeValidationFailed, nil, "source_epoch must be a positive integer")
		return
	}
	if _, err := pathmatch.Compile(match); err != nil {
		s.fail(w, r, codeValidationFailed, nil, "match: "+matchProblem(err))
		return
	}
	var epoch int64
	if input.SourceEpoch != nil {
		epoch = *input.SourceEpoch
	}
	entry, e := s.sourceEpochEntry(entry, epoch)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	preview, err := s.httpCache.PreviewCleanup(r.Context(), entry, *input.Basis, input.Before.UTC(), match)
	if err != nil {
		s.writeError(w, r, previewBuildError(err))
		return
	}
	writeCreated(w, "", 0, maintenanceDocument(preview))
}

// cachePreview resolves {preview_id} of kind to the preview and the entry bound
// to the preview's source epoch, or writes PREVIEW_NOT_FOUND. A preview is
// found by ID within the application, whatever epoch it was built for.
func (s *Server) cachePreview(w http.ResponseWriter, r *http.Request, kind string) (application.Entry, httpcache.MaintenancePreview, bool) {
	entry, ok := s.managedAppWith(w, r, application.HttpCache)
	if !ok {
		return entry, httpcache.MaintenancePreview{}, false
	}
	id := r.PathValue("preview_id")
	if !identity.ValidUID(id) {
		s.fail(w, r, codeInvalidPath, nil, "Invalid preview ID")
		return entry, httpcache.MaintenancePreview{}, false
	}
	preview, err := s.httpCache.LookupAppPreview(entry.UID, kind, id)
	if err != nil {
		s.writeError(w, r, previewLookupError(err))
		return entry, preview, false
	}
	entry.SourceEpoch = preview.SourceEpoch()
	return entry, preview, true
}

// previewLookupError maps unknown, expired and other-kind previews.
func previewLookupError(err error) *apiError {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrExpired) || errors.Is(err, httpcache.ErrInvalidPreview) {
		return newError(codePreviewNotFound, nil, "Preview is unknown or expired; build a new preview")
	}
	return storageError(err)
}

func (s *Server) getCacheRefresh(w http.ResponseWriter, r *http.Request) {
	if _, preview, ok := s.cachePreview(w, r, "refresh"); ok {
		writeOK(w, maintenanceDocument(preview))
	}
}

func (s *Server) getCacheCleanup(w http.ResponseWriter, r *http.Request) {
	if _, preview, ok := s.cachePreview(w, r, "cleanup"); ok {
		writeOK(w, maintenanceDocument(preview))
	}
}

type maintenanceItemDTO struct {
	Ordinal      int64   `json:"ordinal"`
	GenerationID string  `json:"generation_id"`
	Path         string  `json:"path"`
	SizeBytes    int64   `json:"size_bytes"`
	ResultStatus string  `json:"result_status"`
	ErrorCode    *string `json:"error_code"`
}

type maintenanceItemPageDTO struct {
	Items      []maintenanceItemDTO `json:"items"`
	NextCursor *string              `json:"next_cursor"`
	TotalFiles int                  `json:"total_files"`
	TotalBytes int64                `json:"total_bytes"`
	State      string               `json:"state"`
}

func (s *Server) listCacheRefreshItems(w http.ResponseWriter, r *http.Request) {
	s.listPreviewItems(w, r, "refresh", "listCacheRefreshItems")
}

func (s *Server) listCacheCleanupItems(w http.ResponseWriter, r *http.Request) {
	s.listPreviewItems(w, r, "cleanup", "listCacheCleanupItems")
}

func (s *Server) listPreviewItems(w http.ResponseWriter, r *http.Request, kind, operation string) {
	entry, preview, ok := s.cachePreview(w, r, kind)
	if !ok {
		return
	}
	limit, e := queryInt(r, "limit", 25, 1, 100)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	scope := cursorScope(preview.ID)
	cursor, e := decodePageCursor(r, operation, scope)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	after := ""
	if cursor != nil {
		after = string(cursor.Last)
		if n, err := strconv.ParseInt(after, 10, 64); err != nil || n < 0 || strconv.FormatInt(n, 10) != after {
			s.fail(w, r, codeInvalidCursor, nil, "cursor is not a next_cursor of this list; start again without a cursor")
			return
		}
	}
	page, err := s.httpCache.PreviewItems(entry.StorageID(), kind, preview.ID, after, limit)
	if err != nil {
		s.writeError(w, r, previewLookupError(err))
		return
	}
	out := maintenanceItemPageDTO{Items: []maintenanceItemDTO{}, TotalFiles: page.TotalFiles, TotalBytes: page.TotalBytes, State: page.State}
	for _, item := range page.Items {
		out.Items = append(out.Items, maintenanceItemDTO{Ordinal: item.Ordinal, GenerationID: item.GenerationID, Path: "/" + strings.TrimPrefix(item.Path, "/"), SizeBytes: item.SizeBytes, ResultStatus: item.ResultStatus, ErrorCode: optionalText(item.ErrorCode)})
	}
	if page.NextCursor != "" {
		out.NextCursor = encodePageCursor(pageCursor{Operation: operation, Scope: scope, Last: []byte(page.NextCursor)})
	}
	writeOK(w, out)
}

// previewExecutionError maps a failed execution of a found preview.
func previewExecutionError(err error) *apiError {
	switch {
	case errors.Is(err, httpcache.ErrPreviewRunning), errors.Is(err, httpcache.ErrRefreshBusy):
		return newError(codeOperationInProgress, nil, "The preview is running; wait for it to finish")
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, store.ErrExpired):
		return newError(codePreviewNotFound, nil, "Preview is unknown or expired; build a new preview")
	case errors.Is(err, store.ErrSourceInactive), errors.Is(err, httpcache.ErrInvalidPreview):
		return newError(codePreviewStale, err, "The source or policy changed since the preview; nothing was changed, build a new preview")
	default:
		return storageError(err)
	}
}

func (s *Server) executeCacheRefresh(w http.ResponseWriter, r *http.Request) {
	entry, preview, ok := s.cachePreview(w, r, "refresh")
	if !ok {
		return
	}
	switch preview.State {
	case "done", "failed":
		writeJSON(w, http.StatusAccepted, maintenanceDocument(preview))
		return
	case "running":
		s.writeError(w, r, previewExecutionError(httpcache.ErrPreviewRunning))
		return
	}
	current, _ := s.registry.LookupAny(entry.Descriptor.ID)
	if current.UID != entry.UID || current.SourceEpoch != entry.SourceEpoch || current.DeletedAt != nil {
		s.writeError(w, r, previewExecutionError(store.ErrSourceInactive))
		return
	}
	started, err := s.httpCache.ExecuteRefresh(r.Context(), current, preview.ID)
	if err != nil {
		s.writeError(w, r, previewExecutionError(err))
		return
	}
	writeJSON(w, http.StatusAccepted, maintenanceDocument(started))
}

func (s *Server) executeCacheCleanup(w http.ResponseWriter, r *http.Request) {
	entry, preview, ok := s.cachePreview(w, r, "cleanup")
	if !ok {
		return
	}
	switch preview.State {
	case "done", "failed":
		writeOK(w, maintenanceDocument(preview))
		return
	case "running":
		s.writeError(w, r, previewExecutionError(httpcache.ErrPreviewRunning))
		return
	}
	if _, err := s.httpCache.ExecuteCleanup(r.Context(), entry, preview.ID); err != nil {
		s.writeError(w, r, previewExecutionError(err))
		return
	}
	executed, err := s.httpCache.LookupPreview(entry.StorageID(), "cleanup", preview.ID)
	if err != nil {
		s.writeError(w, r, previewLookupError(err))
		return
	}
	writeOK(w, maintenanceDocument(executed))
}

// ---------------------------------------------------------------- automatic cleanup and path match

type autoCleanupStatusDTO struct {
	Running           bool       `json:"running"`
	IntervalSeconds   int64      `json:"interval_seconds"`
	ScanLimitPerApp   int        `json:"scan_limit_per_app"`
	RetireLimitPerApp int        `json:"retire_limit_per_app"`
	LastAttemptAt     *time.Time `json:"last_attempt_at"`
	LastSuccessAt     *time.Time `json:"last_success_at"`
	LastErrorAt       *time.Time `json:"last_error_at"`
	LastError         *string    `json:"last_error"`
	PassesTotal       int64      `json:"passes_total"`
	FailuresTotal     int64      `json:"failures_total"`
	ConfiguredApps    int        `json:"configured_apps"`
	ScannedFiles      int        `json:"scanned_files"`
	RetiredFiles      int        `json:"retired_files"`
	SkippedAccessed   int        `json:"skipped_accessed"`
	SkippedChanged    int        `json:"skipped_changed"`
	RetiredBytes      int64      `json:"retired_bytes"`
}

func (s *Server) getAutoCleanupStatus(w http.ResponseWriter, r *http.Request) {
	st := s.httpCache.CleanupStatus()
	writeOK(w, autoCleanupStatusDTO{Running: st.Running, IntervalSeconds: st.IntervalSeconds, ScanLimitPerApp: st.ScanLimitPerApp, RetireLimitPerApp: st.RetireLimitPerApp, LastAttemptAt: utcTime(st.LastAttemptAt), LastSuccessAt: utcTime(st.LastSuccessAt), LastErrorAt: utcTime(st.LastErrorAt), LastError: optionalText(st.LastError), PassesTotal: st.PassesTotal, FailuresTotal: st.FailuresTotal, ConfiguredApps: st.ConfiguredApps, ScannedFiles: st.ScannedFiles, RetiredFiles: st.RetiredFiles, SkippedAccessed: st.SkippedAccessed, SkippedChanged: st.SkippedChanged, RetiredBytes: st.RetiredBytes})
}

type pathMatchResultDTO struct {
	Matches bool   `json:"matches"`
	Path    string `json:"path"`
}

func (s *Server) testPathMatch(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Match *pathmatch.Spec `json:"match"`
		Path  *string         `json:"path"`
	}
	if e := decodeJSON(r, &input); e != nil {
		s.writeError(w, r, e)
		return
	}
	if input.Match == nil || input.Path == nil {
		s.fail(w, r, codeInvalidRequest, nil, "match and path are required")
		return
	}
	if len(*input.Path) > 4096 || pathmatch.ValidatePath(*input.Path) != nil {
		s.fail(w, r, codeValidationFailed, nil, "path must be a decoded application-relative path starting with /, without query, at most 4096 bytes")
		return
	}
	matcher, err := pathmatch.Compile(*input.Match)
	if err != nil {
		s.fail(w, r, codeValidationFailed, nil, "match: "+matchProblem(err))
		return
	}
	writeOK(w, pathMatchResultDTO{Matches: matcher.Match(*input.Path), Path: *input.Path})
}
