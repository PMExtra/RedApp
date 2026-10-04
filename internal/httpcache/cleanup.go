package httpcache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/pathmatch"
)

type CleanupPreview = MaintenancePreview

type CleanupResult struct {
	SelectedFiles   int   `json:"selected_files"`
	RetiredFiles    int   `json:"retired_files"`
	SkippedAccessed int   `json:"skipped_accessed"`
	SkippedChanged  int   `json:"skipped_changed"`
	RetiredBytes    int64 `json:"retired_bytes"`
}

func (s *Service) Preview(entry application.Entry, basis string, before time.Time) (CleanupPreview, error) {
	return s.PreviewPattern(entry, basis, before, pathmatch.Spec{Type: "glob", Pattern: "/"})
}
func (s *Service) PreviewPattern(entry application.Entry, basis string, before time.Time, match pathmatch.Spec) (CleanupPreview, error) {
	return s.PreviewPatternContext(context.Background(), entry, basis, before, match)
}
func (s *Service) PreviewPatternContext(ctx context.Context, entry application.Entry, basis string, before time.Time, match pathmatch.Spec) (CleanupPreview, error) {
	return s.BuildPreview(ctx, entry, "cleanup", PreviewCriteria{Match: match, Basis: basis, Before: before})
}
func (s *Service) Execute(entry application.Entry, id string) (CleanupResult, error) {
	return s.ExecuteContext(context.Background(), entry, id)
}
func (s *Service) execute(ctx context.Context, entry application.Entry, id string) (CleanupResult, error) {
	return s.ExecuteContext(ctx, entry, id)
}

// ExecuteContext processes the whole frozen set using transactions of at most
// 100 items. New generations can never enter an already-built preview.
func (s *Service) ExecuteContext(ctx context.Context, entry application.Entry, id string) (out CleanupResult, resultErr error) {
	ctx, release, err := s.beginMaintenance(ctx, false)
	if err != nil {
		return out, err
	}
	defer release()
	preview, err := s.claimPreview(ctx, entry, "cleanup", id)
	if err != nil {
		return out, err
	}
	if preview.State == "done" || preview.State == "failed" {
		err = json.Unmarshal(preview.result, &out)
		if err == nil && preview.State == "failed" {
			err = ErrInvalidPreview
		}
		return out, err
	}
	out.SelectedFiles = preview.SelectedFiles
	defer func() {
		if resultErr != nil {
			finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.finishPreview(finishCtx, entry, "cleanup", id, out, true)
		}
	}()
	after := int64(0)
	for {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		items, err := s.pendingPreviewItems(ctx, entry.StorageID(), "cleanup", id, after, 100)
		if err != nil {
			return out, err
		}
		if len(items) == 0 {
			break
		}
		result, err := s.retirePreviewBatch(ctx, entry, preview, items)
		out.RetiredFiles += result.RetiredFiles
		out.SkippedAccessed += result.SkippedAccessed
		out.SkippedChanged += result.SkippedChanged
		out.RetiredBytes += result.RetiredBytes
		if err != nil {
			return out, err
		}
		after = items[len(items)-1].Ordinal
	}
	if err = s.finishPreview(ctx, entry, "cleanup", id, out, false); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Service) retirePreviewBatch(ctx context.Context, entry application.Entry, preview MaintenancePreview, items []PreviewItem) (out CleanupResult, resultErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if preview.criteria.Automatic {
		err = s.db.RequireSourceActive(tx, entry.StorageID(), preview.fence)
	} else {
		err = s.db.RequireCleanupFence(tx, entry.StorageID(), preview.fence)
	}
	if err != nil {
		return out, err
	}
	retired := []string{}
	for _, item := range items {
		if err = ctx.Err(); err != nil {
			return CleanupResult{}, err
		}
		r, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM http_cache_generations WHERE id=? AND storage_id=?`, item.GenerationID, entry.StorageID()))
		status := "retired"
		if errors.Is(err, sql.ErrNoRows) {
			status = "skipped_changed"
		} else if err != nil {
			return CleanupResult{}, err
		} else if !r.current || r.Path != item.Path {
			status = "skipped_changed"
		} else if item.Basis == "last_access" && r.accessBucket != item.AccessBucket {
			status = "skipped_accessed"
		}
		switch status {
		case "skipped_changed":
			out.SkippedChanged++
		case "skipped_accessed":
			out.SkippedAccessed++
		default:
			if _, err = tx.ExecContext(ctx, `UPDATE http_cache_generations SET is_current=0,retired_at_s=? WHERE id=? AND is_current=1`, s.now().Unix(), r.GenerationID); err != nil {
				return CleanupResult{}, err
			}
			out.RetiredFiles++
			out.RetiredBytes += r.SizeBytes
			retired = append(retired, r.GenerationID)
		}
		if err = recordPreviewItemTx(tx, preview.ID, item.Ordinal, status, ""); err != nil {
			return CleanupResult{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return CleanupResult{}, err
	}
	// The logical result is durable before unlinking. Reader pins defer physical
	// collection; a failed unlink remains a retired row for later recovery.
	for _, id := range retired {
		if err = s.collectLocked(id); err != nil {
			return out, err
		}
	}
	return out, nil
}
