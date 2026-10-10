package download

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
)

// Preview freezes exact generations owned by one registered application. The
// client never submits a generation list at execution time. unknown lists the
// versions the selection could not compare; they are kept and recorded.
func (m *Manager) Preview(app string, ids map[string]bool, unknown []string) (Cleanup, error) {
	return m.preview(app, ids, unknown, nil)
}
func (m *Manager) PreviewRetention(app string, ids map[string]bool, guard store.RetentionGuard) (Cleanup, error) {
	return m.preview(app, ids, nil, &guard)
}
func (m *Manager) preview(app string, ids map[string]bool, unknown []string, guard *store.RetentionGuard) (Cleanup, error) {
	ctx, finish, err := m.db.ApplicationWork(context.Background(), app)
	if err != nil {
		return Cleanup{}, err
	}
	defer finish()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.upstreams[app] == nil {
		return Cleanup{}, errors.New("Unknown cleanup application")
	}
	if e := m.db.DeleteExpiredCleanupPreviews(time.Now()); e != nil {
		return Cleanup{}, e
	}
	jobID, err := fsutil.RandomID()
	if err != nil {
		return Cleanup{}, err
	}
	now := time.Now()
	job := Cleanup{ID: jobID, Application: app, Created: now, Expires: now.Add(10 * time.Minute), Selected: []Selection{}}
	selected := map[string]bool{}
	for rid, g := range m.current {
		if err := ctx.Err(); err != nil {
			return Cleanup{}, err
		}
		if !ids[rid] {
			continue
		}
		if g.Resource.Application != app {
			return Cleanup{}, errors.New("Cleanup selection belongs to another application")
		}
		job.Selected = append(job.Selected, Selection{Resource: rid, Generation: g.ID, Version: g.Resource.Version, Key: g.Resource.Key, Bytes: g.Bytes})
		selected[g.ID] = true
		job.LogicalBytes += g.Bytes
		if g.active() {
			job.ActiveGenerations++
		}
	}
	sort.Slice(job.Selected, func(i, j int) bool { return job.Selected[i].Generation < job.Selected[j].Generation })
	seen := map[string]bool{}
	for _, item := range job.Selected {
		g := m.all[item.Generation]
		if g.State != "complete" || seen[g.Resource.Hash] {
			continue
		}
		seen[g.Resource.Hash] = true
		reclaimable := true
		for _, other := range m.all {
			if other.Resource.Application == app && other.Resource.Hash == g.Resource.Hash && other.State == "complete" && (!selected[other.ID] || other.active()) {
				reclaimable = false
				break
			}
		}
		if reclaimable {
			job.ReclaimableBlobBytes += g.Bytes
		}
	}
	row := store.CleanupPreview{Retention: guard, ID: job.ID, AppID: app, CreatedAt: now, ExpiresAt: job.Expires, Selection: []store.CleanupSelection{}, ReclaimableBytes: job.ReclaimableBlobBytes, ActiveGenerations: job.ActiveGenerations, UnknownVersions: unknown}
	if _, _, dynamic := identity.ParseStorageID(app); dynamic {
		source, err := m.db.Source(app)
		if err != nil {
			return job, err
		}
		row.SourceFence = source.Fence()
		if guard != nil && guard.SourceFence != row.SourceFence {
			return job, store.ErrConflict
		}
	}
	for _, item := range job.Selected {
		row.Selection = append(row.Selection, store.CleanupSelection{GenerationID: item.Generation, Version: item.Version, ResourceKey: item.Key, SnapshotBytes: item.Bytes})
	}
	if e := m.db.SaveCleanupPreview(row); e != nil {
		return job, e
	}
	m.checkpoint("preview.after_job_save", nil)
	return job, nil
}

func (m *Manager) Cleanup(app, jobID string) error {
	ctx, finish, err := m.db.ApplicationWork(context.Background(), app)
	if err != nil {
		return err
	}
	defer finish()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.upstreams[app] == nil {
		return errors.New("Unknown cleanup application")
	}
	if !validID(jobID) {
		return errors.New("Invalid cleanup ID")
	}
	job, e := m.db.RetireCleanupPreview(app, jobID, time.Now())
	if e != nil {
		return e
	}
	// The store validates the entire frozen selection and retires it in one
	// transaction. Memory changes only follow successful durable detachment.
	for _, s := range job.Selection {
		if err := ctx.Err(); err != nil {
			return err
		}
		g := m.all[s.GenerationID]
		if g == nil {
			continue
		}
		if g.Resource.Application != app || g.Resource.Version != s.Version || g.Resource.Key != s.ResourceKey {
			return errors.New("Persisted cleanup selection identity mismatch")
		}
		g.Retired = true
		m.checkpoint("cleanup.after_tombstone", g)
		if m.current[g.Resource.ID] == g {
			m.checkpoint("cleanup.after_pointer_delete", g)
			delete(m.current, g.Resource.ID)
			m.checkpoint("cleanup.after_detach", g)
		}
		if e = m.removeLocked(g); e != nil {
			return e
		}
	}
	{
		result, _ := json.Marshal(map[string]any{"selected_generations": len(job.Selection), "status": "retired"})
		if e = m.db.CompleteCleanupPreview(app, jobID, result); e != nil {
			return e
		}
	}
	m.checkpoint("cleanup.after_receipt", nil)
	return nil
}
