package catalog

import (
	"context"
	"errors"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/store"
)

// RunRefresh schedules metadata independently from retention and prewarm.
// Per-channel attempt timestamps bound retries, including after a restart.
// Warm runs only after atomic metadata publication; its failure cannot undo it.
func (s *Service) RunRefresh(ctx context.Context, warm func(context.Context, string, string, string, []string) error) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.RefreshDue(ctx, warm)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) RefreshDue(ctx context.Context, warm func(context.Context, string, string, string, []string) error) {
	for _, e := range s.registry.Entries() {
		p, ok := e.Protocol.(application.SnapshotProtocol)
		if !ok {
			continue
		}
		for _, channel := range e.Descriptor.Channels {
			if ctx.Err() != nil {
				return
			}
			canonical, err := p.CanonicalChannel(channel)
			if err != nil || canonical != channel {
				continue
			}
			status, err := s.db.ReleaseRefresh(e.StorageID(), channel)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err == nil && time.Since(status.Attempt) < time.Duration(e.Descriptor.DefaultChannelTTLSeconds)*time.Second {
				continue
			}
			release, err := s.Refresh(ctx, e.Descriptor.ID, channel)
			if err != nil {
				continue
			}
			keys := p.PrewarmKeys(channel)
			if len(keys) > 0 && warm != nil {
				if err = warm(ctx, e.Descriptor.ID, e.StorageID(), release.Version, keys); err != nil {
					_ = s.db.RecordEvent(store.Event{AppID: e.MetricsID(), Category: "metadata", Code: "release_prewarm_failed", Message: "Selected release prewarm could not start", Version: release.Version})
				}
			}
		}
	}
}
