package httpcache

import (
	"context"
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

// recoverPreviews fails executions a previous process left behind; they are
// receipts, never restartable queues.
func (s *Service) recoverPreviews(ctx context.Context) error {
	if err := s.db.InterruptHTTPPreviews(ctx, s.now()); err != nil {
		return err
	}
	return s.prunePreviews(ctx, 4)
}

// prunePreviews removes expired previews and old receipts in bounded batches.
func (s *Service) prunePreviews(ctx context.Context, batches int) error {
	return s.db.PruneHTTPPreviews(ctx, s.now(), batches)
}
