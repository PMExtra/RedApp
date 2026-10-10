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

// pruneBatches bounds the expired previews removed before a new release
// preview and at startup.
const pruneBatches = 100

// Preview freezes the current generations of the resources ids of one
// registered application epoch as a version cleanup preview. The client never
// submits a generation list at execution time. unknown lists the versions the
// selection could not compare; they are kept and reported.
func (m *Manager) Preview(app string, ids map[string]bool, unknown []string) (store.Preview, error) {
	source, err := m.db.Source(app)
	if err != nil {
		return store.Preview{}, err
	}
	return m.freeze(context.Background(), app, store.PreviewVersionCleanup, source.Fence(), nil, ids, nil, unknown)
}

// PreviewRetention freezes a retention preview of app admitted under fence:
// versions are the evaluated versions in display order, ids the resources of
// the selected ones.
func (m *Manager) PreviewRetention(ctx context.Context, app string, fence store.SourceFence, guard store.RetentionGuard, versions []store.RetentionVersion, ids map[string]bool) (store.Preview, error) {
	criteria, err := json.Marshal(guard)
	if err != nil {
		return store.Preview{}, err
	}
	return m.freeze(ctx, app, store.PreviewRetention, fence, criteria, ids, versions, nil)
}

func (m *Manager) freeze(ctx context.Context, app string, kind store.PreviewKind, fence store.SourceFence, criteria json.RawMessage, ids map[string]bool, versions []store.RetentionVersion, unknown []string) (store.Preview, error) {
	uid, epoch, ok := identity.ParseStorageID(app)
	if !ok {
		return store.Preview{}, errors.New("invalid cleanup application")
	}
	ctx, finish, err := m.db.ApplicationWork(ctx, app)
	if err != nil {
		return store.Preview{}, err
	}
	defer finish()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.upstreams[app] == nil {
		return store.Preview{}, errors.New("unknown cleanup application")
	}
	now := time.Now()
	if err = m.db.PrunePreviews(ctx, now, pruneBatches); err != nil {
		return store.Preview{}, err
	}
	id, err := fsutil.RandomID()
	if err != nil {
		return store.Preview{}, err
	}
	generations := map[string][]store.CleanupSelection{}
	selected := map[string]bool{}
	active := 0
	for rid, g := range m.current {
		if err := ctx.Err(); err != nil {
			return store.Preview{}, err
		}
		if !ids[rid] {
			continue
		}
		if g.Resource.Application != app {
			return store.Preview{}, errors.New("cleanup selection belongs to another application")
		}
		v := g.Resource.Version
		generations[v] = append(generations[v], store.CleanupSelection{GenerationID: g.ID, Version: v, ResourceKey: g.Resource.Key, SnapshotBytes: g.Bytes})
		selected[g.ID] = true
		if g.active() {
			active++
		}
	}
	for _, list := range generations {
		sort.Slice(list, func(i, j int) bool { return list[i].GenerationID < list[j].GenerationID })
	}
	items := []store.PreviewItem{}
	add := func(version string, reasons []string, bytes int64, chosen bool) error {
		detail := store.ReleaseItemDetail{Reasons: reasons, Generations: []store.CleanupSelection{}}
		if chosen {
			detail.Generations = generations[version]
			bytes = 0
			for _, g := range detail.Generations {
				bytes += g.SnapshotBytes
			}
		}
		raw, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		items = append(items, store.PreviewItem{Ordinal: int64(len(items) + 1), Label: version, SizeBytes: bytes, Selected: chosen, Detail: raw})
		return nil
	}
	if kind == store.PreviewRetention {
		for _, v := range versions {
			if err = add(v.Version, v.Reasons, v.Bytes, v.Selected && len(generations[v.Version]) > 0); err != nil {
				return store.Preview{}, err
			}
		}
	} else {
		labels := make([]string, 0, len(generations))
		for version := range generations {
			labels = append(labels, version)
		}
		sort.Strings(labels)
		for _, version := range labels {
			if err = add(version, nil, 0, true); err != nil {
				return store.Preview{}, err
			}
		}
	}
	if unknown == nil {
		unknown = []string{}
	}
	summary, err := json.Marshal(store.ReleasePreviewSummary{ReclaimableBytes: m.reclaimableLocked(app, selected), UnknownVersions: unknown})
	if err != nil {
		return store.Preview{}, err
	}
	p, err := m.db.CreatePreview(ctx, store.Preview{
		ID: id, Kind: kind, AppUID: uid, SourceEpoch: epoch, Fence: fence, RequireActive: kind == store.PreviewRetention,
		State: store.PreviewReady, CreatedAt: now, ExpiresAt: now.Add(store.PreviewLifetime), Criteria: criteria, Summary: summary,
		ScannedItems: len(items), ActiveItems: active,
	}, items)
	if err != nil {
		return p, err
	}
	m.checkpoint("preview.after_job_save", nil)
	return p, nil
}

// reclaimableLocked estimates the blob bytes freed by retiring the selected
// generations: a blob counts once no unselected or active generation of the
// application still uses it.
func (m *Manager) reclaimableLocked(app string, selected map[string]bool) int64 {
	var total int64
	seen := map[string]bool{}
	for id := range selected {
		g := m.all[id]
		if g == nil || g.State != "complete" || seen[g.Resource.Hash] {
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
			total += g.Bytes
		}
	}
	return total
}

// Cleanup executes a version cleanup preview of app. Generations being read
// or written are retired now and deleted after their transfers.
func (m *Manager) Cleanup(app, id string) error {
	_, err := m.execute(context.Background(), app, store.PreviewVersionCleanup, id, false)
	return err
}

// CleanupRetention executes a retention preview of app. Unlike a version
// cleanup it keeps versions in use by any source epoch of the application.
func (m *Manager) CleanupRetention(ctx context.Context, app, id string) (store.ReleaseReceipt, error) {
	return m.execute(ctx, app, store.PreviewRetention, id, true)
}

func (m *Manager) execute(ctx context.Context, app string, kind store.PreviewKind, id string, protectActive bool) (store.ReleaseReceipt, error) {
	ctx, finish, err := m.db.ApplicationWork(ctx, app)
	if err != nil {
		return store.ReleaseReceipt{}, err
	}
	defer finish()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return store.ReleaseReceipt{}, err
	}
	if m.closed {
		return store.ReleaseReceipt{}, context.Canceled
	}
	if m.upstreams[app] == nil {
		return store.ReleaseReceipt{}, errors.New("unknown cleanup application")
	}
	if !validID(id) {
		return store.ReleaseReceipt{}, errors.New("invalid cleanup ID")
	}
	blocked := map[string]string{}
	if protectActive {
		for _, g := range m.all {
			if SameOwner(g.Resource.Application, app) && g.active() {
				blocked[g.Resource.Version] = "in_use_at_execution"
			}
		}
	}
	_, receipt, err := m.db.ExecuteReleasePreview(ctx, app, kind, id, blocked, time.Now())
	if err != nil {
		return receipt, err
	}
	// The store retired the frozen generations durably; memory follows. A
	// repeated execution repeats this for generations a failed removal left.
	for _, s := range receipt.Selection {
		g := m.all[s.GenerationID]
		if g == nil {
			continue
		}
		if g.Resource.Application != app || g.Resource.Version != s.Version || g.Resource.Key != s.ResourceKey {
			return receipt, errors.New("persisted cleanup selection identity mismatch")
		}
		g.Retired = true
		m.checkpoint("cleanup.after_tombstone", g)
		if m.current[g.Resource.ID] == g {
			m.checkpoint("cleanup.after_pointer_delete", g)
			delete(m.current, g.Resource.ID)
			m.checkpoint("cleanup.after_detach", g)
		}
		if err = m.removeLocked(g); err != nil {
			return receipt, err
		}
	}
	m.checkpoint("cleanup.after_receipt", nil)
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
