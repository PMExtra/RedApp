package download

import (
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/PMExtra/RedApp/internal/store"
)

// Preview freezes exact generations owned by one registered application. The
// client never submits a generation list at execution time.
func (m *Manager) Preview(app string, ids map[string]bool) (Cleanup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.upstreams[app] == nil {
		return Cleanup{}, errors.New("Unknown cleanup application")
	}
	if e := m.db.DeleteExpiredCleanupPreviews(time.Now()); e != nil {
		return Cleanup{}, e
	}
	now := time.Now()
	job := Cleanup{ID: id(), Application: app, Created: now, Expires: now.Add(10 * time.Minute), Selected: []Selection{}}
	selected := map[string]bool{}
	for rid, g := range m.current {
		if !ids[rid] {
			continue
		}
		if g.Resource.Application != app {
			return Cleanup{}, errors.New("Cleanup selection belongs to another application")
		}
		job.Selected = append(job.Selected, Selection{Resource: rid, Generation: g.ID, Version: g.Resource.Version, Key: g.Resource.Key, Bytes: g.Bytes})
		selected[g.ID] = true
		job.LogicalBytes += g.Bytes
		if g.running || g.readers > 0 {
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
			if other.Resource.Application == app && other.Resource.Hash == g.Resource.Hash && other.State == "complete" && (!selected[other.ID] || other.running || other.readers > 0) {
				reclaimable = false
				break
			}
		}
		if reclaimable {
			job.ReclaimableBlobBytes += g.Bytes
		}
	}
	row := store.CleanupPreview{ID: job.ID, AppID: app, CreatedAt: now, ExpiresAt: job.Expires, Selection: []store.CleanupSelection{}}
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
		signal(g)
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
	m.checkpoint("cleanup.after_job_delete", nil) // Kept fault barrier name; the row is now a 24-hour receipt.
	return nil
}
