package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/store"
)

// Every frozen preview (version cleanup, retention, HTTP cache refresh and
// cleanup) shares one lifecycle in the store; these helpers give it one HTTP
// mapping. Deleted and disabled applications are refused by the handlers
// before a preview is built or executed, as each operation declares.

// previewRecord resolves {preview_id} to a readable preview of kind owned by
// e, whatever source epoch it was built for, or writes the error.
func (s *Server) previewRecord(w http.ResponseWriter, r *http.Request, e application.Entry, kind store.PreviewKind) (store.Preview, bool) {
	id, ok := s.pathUID(w, r, "preview_id")
	if !ok {
		return store.Preview{}, false
	}
	p, err := s.store.Preview(e.UID, kind, id, time.Now())
	if err != nil {
		s.writeError(w, r, previewError(err))
		return p, false
	}
	return p, true
}

// previewError maps reading or executing a preview: unknown, foreign,
// other-kind and expired previews are PREVIEW_NOT_FOUND, a changed fence or
// guard PREVIEW_STALE, and a preview being built or executed
// OPERATION_IN_PROGRESS. A source that stopped serving during execution is
// also stale.
func previewError(err error) *apiError {
	switch {
	case isNotFound(err), errors.Is(err, store.ErrExpired):
		return newError(codePreviewNotFound, err, "The preview is unknown or expired; build a new preview")
	case errors.Is(err, store.ErrPreviewStale), errors.Is(err, store.ErrSourceInactive):
		return newError(codePreviewStale, err, "The source, policy or selected objects changed since the preview; nothing was changed, build a new preview")
	case errors.Is(err, store.ErrPreviewRunning):
		return newError(codeOperationInProgress, err, "The preview is being built or executed; wait for it to finish")
	}
	return storageError(err)
}

// previewBuildFailure maps a preview that could not be frozen because the
// source changed or stopped serving meanwhile.
func previewBuildFailure(err error) *apiError {
	if errors.Is(err, store.ErrPreviewStale) || errors.Is(err, store.ErrSourceInactive) {
		return newError(codeSourceChanged, err, "The application source is not active or changed during the preview; retry")
	}
	return storageError(err)
}

// previewItemCursorPage reads one cursor page (`limit`, `cursor`) of the items
// of p for operation. Items never move, so pages are stable while the
// preview executes.
func (s *Server) previewItemCursorPage(w http.ResponseWriter, r *http.Request, p store.Preview, operation string) ([]store.PreviewItem, *string, bool) {
	limit, e := queryInt(r, "limit", 25, 1, 100)
	if e != nil {
		s.writeError(w, r, e)
		return nil, nil, false
	}
	scope := cursorScope(p.ID)
	last, e := decodeAfterCursor(r, operation, scope)
	if e != nil {
		s.writeError(w, r, e)
		return nil, nil, false
	}
	var after int64
	if last != "" {
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n < 1 || strconv.FormatInt(n, 10) != last {
			s.writeError(w, r, invalidCursor())
			return nil, nil, false
		}
		after = n
	}
	items, err := s.store.PreviewItems(r.Context(), p.ID, after, limit+1, false)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return nil, nil, false
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		next = afterCursor(operation, scope, strconv.FormatInt(items[limit-1].Ordinal, 10))
	}
	return items, next, true
}
