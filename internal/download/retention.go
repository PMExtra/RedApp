package download

import (
	"context"
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
	"time"
)

// CleanupRetention differs from manual cleanup only in admission protection and
// the durable subset. The same generation lifecycle performs all reclamation.
func (m *Manager) CleanupRetention(ctx context.Context, app, jobID string) (store.RetentionReceipt, error) {
	ctx, finish, err := m.db.ApplicationWork(ctx, app)
	if err != nil {
		return store.RetentionReceipt{}, err
	}
	defer finish()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return store.RetentionReceipt{}, err
	}
	if m.closed {
		return store.RetentionReceipt{}, context.Canceled
	}
	blocked := map[string]string{}
	for _, g := range m.all {
		if SameOwner(g.Resource.Application, app) && g.active() {
			blocked[g.Resource.Version] = "in_use_at_execution"
		}
	}
	frozen, err := m.db.CleanupPreview(app, jobID)
	if err != nil {
		return store.RetentionReceipt{}, err
	}
	if frozen.ExecutedAt == nil {
		for _, item := range frozen.Selection {
			g := m.all[item.GenerationID]
			if g == nil || g.Retired || m.current[g.Resource.ID] != g {
				blocked[item.Version] = "generation_changed"
			}
		}
	}
	job, err := m.db.RetireRetentionPreview(app, jobID, time.Now(), blocked)
	if err != nil {
		return store.RetentionReceipt{}, err
	}
	var receipt store.RetentionReceipt
	if err = json.Unmarshal(job.Result, &receipt); err != nil {
		return receipt, err
	}
	for _, item := range receipt.Selection {
		g := m.all[item.GenerationID]
		if g == nil {
			continue
		}
		g.Retired = true
		if m.current[g.Resource.ID] == g {
			delete(m.current, g.Resource.ID)
		}
		signal(g)
		if err = m.removeLocked(g); err != nil {
			return receipt, err
		}
	}
	return receipt, nil
}

// SameOwner keeps activity protection across retained source epochs without broadening deletion.
func SameOwner(a, b string) bool {
	if a == b {
		return true
	}
	au, _, aok := identity.ParseStorageID(a)
	bu, _, bok := identity.ParseStorageID(b)
	return aok && bok && au == bu
}
