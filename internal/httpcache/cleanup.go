package httpcache

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/logging"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
)

type CleanupResult struct {
	SelectedFiles   int   `json:"selected_files"`
	RetiredFiles    int   `json:"retired_files"`
	SkippedAccessed int   `json:"skipped_accessed"`
	SkippedChanged  int   `json:"skipped_changed"`
	RetiredBytes    int64 `json:"retired_bytes"`
}

// PreviewCleanup freezes the current files matching match whose basis time
// ("fetched_at" or "last_access") is before the cutoff.
func (s *Service) PreviewCleanup(ctx context.Context, entry application.Entry, basis string, before time.Time, match pathmatch.Spec) (MaintenancePreview, error) {
	return s.buildPreview(ctx, entry, "cleanup", PreviewCriteria{Match: match, Basis: basis, Before: before}, buildOptions{})
}

// ExecuteCleanup processes the whole frozen set using transactions of at most
// 100 items. New generations can never enter an already-built preview.
func (s *Service) ExecuteCleanup(ctx context.Context, entry application.Entry, id string) (out CleanupResult, resultErr error) {
	ctx, finish, err := s.db.ApplicationWork(ctx, entry.StorageID())
	if err != nil {
		return out, err
	}
	defer finish()
	ctx, release, err := s.beginMaintenance(ctx, false)
	if err != nil {
		return out, err
	}
	defer release()
	preview, err := s.claimPreview(ctx, entry, "cleanup", id)
	if err != nil {
		return out, err
	}
	if preview.State == store.PreviewDone || preview.State == store.PreviewFailed {
		err = json.Unmarshal(preview.Result, &out)
		if err == nil && preview.State == store.PreviewFailed {
			err = ErrInvalidPreview
		}
		return out, err
	}
	out.SelectedFiles = preview.SelectedFiles
	defer func() {
		if resultErr != nil {
			finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.finishPreview(finishCtx, preview, out, true); err != nil {
				s.log.Error("HTTP cache cleanup receipt was not saved", slog.String("app", entry.Descriptor.ID), slog.String("preview_id", id), logging.Error(err))
			}
		}
	}()
	after := int64(0)
	for {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		items, err := s.pendingPreviewItems(ctx, preview, after, 100)
		if err != nil {
			return out, err
		}
		if len(items) == 0 {
			break
		}
		result, err := s.retirePreviewBatch(ctx, preview, items)
		out.RetiredFiles += result.RetiredFiles
		out.SkippedAccessed += result.SkippedAccessed
		out.SkippedChanged += result.SkippedChanged
		out.RetiredBytes += result.RetiredBytes
		if err != nil {
			return out, err
		}
		after = items[len(items)-1].Ordinal
	}
	if err = s.finishPreview(ctx, preview, out, false); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Service) retirePreviewBatch(ctx context.Context, preview MaintenancePreview, items []store.PreviewItem) (CleanupResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	retired, err := s.db.RetireHTTPCachePreviewItems(ctx, preview.row, items, s.now())
	if err != nil {
		return CleanupResult{}, err
	}
	out := CleanupResult{RetiredFiles: len(retired.Retired), RetiredBytes: retired.RetiredBytes, SkippedAccessed: retired.SkippedAccessed, SkippedChanged: retired.SkippedChanged}
	// The logical result is durable before unlinking. Reader pins defer physical
	// collection; a failed unlink remains a retired row for later recovery.
	for _, id := range retired.Retired {
		if err = s.collectLocked(id); err != nil {
			return out, err
		}
	}
	return out, nil
}
