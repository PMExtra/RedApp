package httpcache

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/logging"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
)

var ErrRefreshMissing = errors.New("HTTP cache refresh requires an existing current file")
var ErrRefreshBusy = errors.New("HTTP cache refresh is already running")
var ErrInvalidRefresh = errors.New("invalid HTTP cache refresh request")

type RefreshItem struct {
	Path         string `json:"path"`
	GenerationID string `json:"generation_id,omitempty"`
	Status       string `json:"status"`
	Reason       string `json:"reason,omitempty"`
}

// RefreshSummary is the bounded receipt. Item outcomes remain in the persisted
// preview and are read through its paginated items endpoint.
type RefreshSummary struct {
	SelectedFiles  int `json:"selected_files"`
	CompletedFiles int `json:"completed_files"`
	Refreshed      int `json:"refreshed"`
	NotModified    int `json:"not_modified"`
	StaleFallback  int `json:"stale_fallback"`
	Failed         int `json:"failed"`
	Skipped        int `json:"skipped"`
}

func (s *Service) PreviewRefresh(ctx context.Context, entry application.Entry, match pathmatch.Spec) (MaintenancePreview, error) {
	ctx, finish, err := s.db.ApplicationWork(ctx, entry.StorageID())
	if err != nil {
		return MaintenancePreview{}, err
	}
	defer finish()
	if err := s.begin(entry); err != nil {
		return MaintenancePreview{}, err
	}
	defer s.wg.Done()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer cancel()
	defer stop()
	return s.buildPreview(ctx, entry, "refresh", PreviewCriteria{Match: match}, buildOptions{})
}

// ExecuteRefresh starts a single background worker. The durable frozen set is
// consumed in small pages; disconnecting the initiating HTTP request does not
// cancel it. An active worker excludes additional batch workers, without a queue.
func (s *Service) ExecuteRefresh(ctx context.Context, entry application.Entry, id string) (MaintenancePreview, error) {
	ctx, finished, err := s.db.ApplicationWork(ctx, entry.StorageID())
	if err != nil {
		return MaintenancePreview{}, err
	}
	defer finished()
	if err := s.begin(entry); err != nil {
		return MaintenancePreview{}, err
	}
	defer s.wg.Done()
	preview, err := s.LookupPreview(entry.StorageID(), "refresh", id)
	if err != nil {
		return preview, err
	}
	if preview.State == store.PreviewDone || preview.State == store.PreviewFailed {
		return preview, nil
	}
	if err := ctx.Err(); err != nil {
		return preview, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return preview, ErrClosed
	}
	if s.refreshRunning {
		s.mu.Unlock()
		return preview, ErrRefreshBusy
	}
	s.refreshRunning = true
	s.wg.Add(1)
	s.mu.Unlock()
	preview, err = s.claimPreview(ctx, entry, "refresh", id)
	if err != nil || preview.State != store.PreviewRunning {
		s.releaseRefreshWorker()
		if errors.Is(err, store.ErrPreviewRunning) {
			err = ErrRefreshBusy
		}
		return preview, err
	}
	workCtx, finish, err := s.db.ApplicationWork(s.ctx, entry.StorageID())
	if err != nil {
		s.releaseRefreshWorker()
		return preview, err
	}
	go func() { defer finish(); s.runRefresh(workCtx, entry, preview) }()
	return preview, nil
}

func (s *Service) releaseRefreshWorker() {
	s.mu.Lock()
	s.refreshRunning = false
	s.mu.Unlock()
	s.wg.Done()
}

func (s *Service) runRefresh(ctx context.Context, entry application.Entry, preview MaintenancePreview) {
	defer s.releaseRefreshWorker()
	summary := RefreshSummary{SelectedFiles: preview.SelectedFiles}
	// cause ends the run early and fails its receipt.
	var cause error
	defer func() {
		// Persist a terminal receipt even during shutdown or after a source change.
		// The store still belongs to the service until Close has drained this worker.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		attrs := []any{slog.String("app", entry.Descriptor.ID), slog.String("preview_id", preview.ID)}
		switch {
		case cause == nil, errors.Is(cause, context.Canceled), errors.Is(cause, ErrClosed):
		case errors.Is(cause, store.ErrSourceInactive), errors.Is(cause, store.ErrPreviewStale):
			s.log.Info("HTTP cache refresh stopped", append(attrs, slog.String("reason", "source_changed"))...)
		default:
			s.log.Warn("HTTP cache refresh failed", append(attrs, logging.Error(cause))...)
		}
		if err := s.finishPreview(ctx, preview, summary, cause != nil); err != nil {
			s.log.Error("HTTP cache refresh receipt was not saved", append(attrs, logging.Error(err))...)
		}
	}()
	var after int64
	for {
		if cause = ctx.Err(); cause != nil {
			return
		}
		if cause = s.db.CheckSourceActive(entry.StorageID(), fence(entry)); cause != nil {
			return
		}
		items, err := s.pendingPreviewItems(ctx, preview, after, 25)
		if err != nil {
			cause = err
			return
		}
		if len(items) == 0 {
			return
		}
		for _, selected := range items {
			item, err := s.refreshExisting(ctx, entry, "/"+strings.TrimPrefix(selected.Label, "/"), selected.Ref)
			if errors.Is(err, context.Canceled) || errors.Is(err, ErrClosed) || errors.Is(err, store.ErrSourceInactive) {
				cause = err
				return
			}
			if err != nil && !errors.Is(err, ErrRefreshMissing) {
				item.Status, item.Reason = "failed", "refresh_failed"
			}
			if err := s.recordPreviewItem(ctx, preview, selected.Ordinal, item.Status, item.Reason); err != nil {
				cause = err
				return
			}
			after = selected.Ordinal
			summary.CompletedFiles++
			switch item.Status {
			case "refreshed":
				summary.Refreshed++
			case "not_modified":
				summary.NotModified++
			case "stale_fallback":
				summary.StaleFallback++
			case "skipped":
				summary.Skipped++
			default:
				summary.Failed++
			}
		}
	}
}

// Refresh explicitly validates one existing current resource, even when fresh.
// It never crawls an origin or records an administrator operation as file access.
// Path is a decoded application-relative path including its leading slash.
func (s *Service) Refresh(ctx context.Context, entry application.Entry, path string) (RefreshItem, error) {
	return s.refreshExisting(ctx, entry, path, "")
}

func (s *Service) refreshExisting(ctx context.Context, entry application.Entry, path, expectedGeneration string) (RefreshItem, error) {
	item := RefreshItem{Path: path, Status: "failed"}
	ctx, finish, err := s.db.ApplicationWork(ctx, entry.StorageID())
	if err != nil {
		return item, err
	}
	defer finish()
	if len(path) > 4096 || path == "/" || pathmatch.ValidatePath(path) != nil || entry.Upstream == nil {
		return item, ErrInvalidRefresh
	}
	relative := strings.TrimPrefix(path, "/")
	if _, err := entry.Upstream.RelativeURL(relative); err != nil {
		return item, ErrInvalidRefresh
	}
	if err := s.begin(entry); err != nil {
		return item, err
	}
	defer s.wg.Done()
	release, err := s.budget.AcquireHTTPReader()
	if err != nil {
		return item, err
	}
	defer release()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer cancel()
	defer stop()
	policy, err := s.readPolicy(entry)
	if err != nil {
		return item, err
	}
	f := fill{entry: entry, path: relative, policy: policy}
	for tries := 0; ; tries++ {
		if err := ctx.Err(); err != nil {
			return item, err
		}
		if tries == fetchAgainLimit {
			item.Reason = "generation_changed"
			return item, ErrFetchContended
		}
		old, err := s.lookup(ctx, entry.StorageID(), relative)
		if err != nil {
			return item, err
		}
		if old == nil {
			item.Status, item.Reason = "skipped", "file_no_longer_current"
			return item, ErrRefreshMissing
		}
		item.GenerationID = old.GenerationID
		if expectedGeneration == "" {
			expectedGeneration = old.GenerationID
		}
		if old.GenerationID != expectedGeneration {
			s.unpin(old.GenerationID)
			item.Status, item.Reason = "skipped", "generation_changed"
			return item, nil
		}
		result, err := s.sharedFetch(ctx, f, old)
		s.unpin(old.GenerationID)
		if errors.Is(err, ErrFetchAgain) {
			continue
		}
		uncacheable := errors.Is(err, errUncacheableFlight)
		if err != nil && !uncacheable {
			item.Reason = "refresh_failed"
			return item, err
		}
		if result.stream != nil {
			// The changed body is cached while it streams; the refresh reports
			// it once the new entry is published.
			published, err := s.awaitPublication(ctx, result.stream)
			s.leaveStream(result.stream)
			if err != nil || published == "" {
				item.Reason = "refresh_failed"
				if err == nil {
					err = ErrUpstream
				}
				return item, err
			}
			item.GenerationID, item.Status = published, "refreshed"
			return item, nil
		}
		if result.response != nil || uncacheable {
			if result.response != nil {
				result.response.Body.Close()
			}
			item.Status, item.Reason = "skipped", "response_not_cacheable"
			if result.blockReason != "" {
				item.Reason = result.blockReason
			}
			return item, nil
		}
		if result.row == nil {
			item.Reason = "upstream_response_unavailable"
			return item, ErrUpstream
		}
		item.GenerationID = result.row.GenerationID
		s.unpin(result.row.GenerationID)
		switch {
		case result.stale:
			item.Status = "stale_fallback"
		case result.row.GenerationID == expectedGeneration:
			item.Status = "not_modified"
		default:
			item.Status = "refreshed"
		}
		return item, nil
	}
}
