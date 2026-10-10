package httpcache

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Register before exposing work so Close cancels and waits for admitted builds
// and cleanup executions. Builders reject excess work instead of queueing it.
func (s *Service) beginMaintenance(ctx context.Context, builder bool) (context.Context, func(), error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, nil, ErrClosed
	}
	if builder && s.previewBuilders >= PreviewBuilderLimit {
		s.mu.Unlock()
		return nil, nil, ErrPreviewBusy
	}
	if builder {
		s.previewBuilders++
	}
	s.wg.Add(1)
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	return ctx, func() {
		stop()
		cancel()
		if builder {
			s.mu.Lock()
			s.previewBuilders--
			s.mu.Unlock()
		}
		s.wg.Done()
	}, nil
}

// An interrupted execution is a receipt, never a restartable queue. Updating
// bounded batches keeps crash recovery independent of the number of items.
func (s *Service) recoverPreviews(ctx context.Context) error {
	for {
		result, err := s.db.HTTPCacheDB().ExecContext(ctx, `UPDATE http_cleanup_previews SET
			result_json=json_object('error',CASE WHEN state='building' THEN 'preview_build_failed' ELSE 'interrupted_by_restart' END,
			'selected_files',selected_count,'completed_files',completed_count,'failed',failed_count),
			executed_at_s=?,state='failed'
			WHERE id IN (SELECT id FROM http_cleanup_previews WHERE state IN ('building','running') LIMIT 100)`, s.now().Unix())
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n < 100 {
			break
		}
	}
	return s.prunePreviews(ctx, 4)
}

// Each transaction removes at most 1000 frozen items, then removes the header
// only if it has no children. Never invoke an unbounded FK cascade. Failed
// builds keep their small diagnostic receipt but discard unusable selections.
func (s *Service) prunePreviews(ctx context.Context, batches int) error {
	for i := 0; i < batches; i++ {
		tx, err := s.db.HTTPCacheDB().BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var id string
		var expired bool
		now := s.now()
		err = tx.QueryRowContext(ctx, `SELECT id,
			(state='ready' AND expires_at_s<=?) OR (state IN ('done','failed') AND executed_at_s<=?)
			FROM http_cleanup_previews p WHERE
			(state='ready' AND expires_at_s<=?) OR
			(state IN ('done','failed') AND executed_at_s<=?) OR
			(state='failed' AND json_extract(result_json,'$.error')='preview_build_failed' AND EXISTS(SELECT 1 FROM http_cleanup_preview_items i WHERE i.preview_id=p.id))
			ORDER BY created_at_s,id LIMIT 1`, now.Unix(), now.Add(-24*time.Hour).Unix(), now.Unix(), now.Add(-24*time.Hour).Unix()).Scan(&id, &expired)
		if errors.Is(err, sql.ErrNoRows) {
			tx.Rollback()
			return nil
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM http_cleanup_preview_items WHERE preview_id=? AND ordinal IN (SELECT ordinal FROM http_cleanup_preview_items WHERE preview_id=? ORDER BY ordinal LIMIT 1000)`, id, id); err != nil {
			tx.Rollback()
			return err
		}
		if expired {
			if _, err = tx.ExecContext(ctx, `DELETE FROM http_cleanup_previews WHERE id=? AND NOT EXISTS(SELECT 1 FROM http_cleanup_preview_items WHERE preview_id=?)`, id, id); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
