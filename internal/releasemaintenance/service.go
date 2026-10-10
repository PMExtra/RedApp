// Package releasemaintenance retains cached immutable releases. Its existing
// scheduled cycle can also invoke the dedicated prewarm worker.
package releasemaintenance

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/catalog"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
	"sort"
	"sync/atomic"
	"time"
)

const Interval = 15 * time.Minute
const VersionLimit = 100

var ErrChannels = errors.New("declared channel could not be verified")

type Service struct {
	DB               *store.Store
	Registry         *application.Registry
	Catalog          *catalog.Service
	Downloads        *download.Manager
	AutomaticPrewarm func(context.Context)
	running          atomic.Bool
	next             atomic.Int64
}
type Preview struct {
	ID               string                   `json:"id"`
	Versions         []store.RetentionVersion `json:"-"`
	SelectedVersions int                      `json:"selected_versions"`
	LogicalBytes     int64                    `json:"logical_bytes"`
	ReclaimableBytes int64                    `json:"reclaimable_bytes"`
	Expires          time.Time                `json:"expires"`
}

// Status is the persisted record of the last retention run of an application.
type Status struct {
	Attempt         time.Time  `json:"attempt"`
	Success         *time.Time `json:"success,omitempty"`
	Outcome         string     `json:"outcome"`
	Reason          string     `json:"reason,omitempty"`
	RetiredVersions int        `json:"retired_versions"`
	LogicalBytes    int64      `json:"logical_bytes"`
}

// Select counts only complete current binary versions. Unknown comparisons retain
// both involved versions; stable string tie breaking makes equal versions repeatable.
func Select(protocol application.Protocol, storage string, keep int, views []download.View, channels map[string]store.RetentionChannel) ([]store.RetentionVersion, map[string]bool) {
	versions := map[string]*store.RetentionVersion{}
	active := map[string]bool{}
	for _, v := range views {
		if download.SameOwner(v.Resource.Application, storage) && (v.ActiveWriter || v.Readers > 0) {
			active[v.Resource.Version] = true
		}
		if v.Resource.Application != storage {
			continue
		}
		if v.Current && !v.Retired && v.State == "complete" {
			item := versions[v.Resource.Version]
			if item == nil {
				item = &store.RetentionVersion{Version: v.Resource.Version, Reasons: []string{}}
				versions[item.Version] = item
			}
			item.Bytes += v.Bytes
		}
	}
	for _, item := range versions {
		item.Bytes = 0
	}
	for _, v := range views {
		if v.Resource.Application == storage && v.Current && !v.Retired {
			if item := versions[v.Resource.Version]; item != nil {
				item.Bytes += v.Bytes
			}
		}
	}
	names := []string{}
	unknown := map[string]bool{}
	invalid := map[string]bool{}
	for name := range versions {
		names = append(names, name)
		v, e := protocol.ValidateVersion(name)
		if e != nil || v != name {
			unknown[name] = true
			invalid[name] = true
		}
	}
	sort.Strings(names)
	for i, a := range names {
		for _, b := range names[i+1:] {
			if invalid[a] || invalid[b] {
				continue
			}
			if _, e := protocol.CompareVersions(a, b); e != nil {
				unknown[a] = true
				unknown[b] = true
			}
		}
	}
	sortable := []string{}
	for _, v := range names {
		if !unknown[v] {
			sortable = append(sortable, v)
		}
	}
	sort.SliceStable(sortable, func(i, j int) bool {
		order, _ := protocol.CompareVersions(sortable[i], sortable[j])
		if order == 0 {
			return sortable[i] < sortable[j]
		}
		return order > 0
	})
	latest := map[string]bool{}
	for i, v := range sortable {
		if i < keep {
			latest[v] = true
		}
	}
	protected := map[string]bool{}
	for _, c := range channels {
		protected[c.Version] = true
	}
	out := []store.RetentionVersion{}
	selected := map[string]bool{}
	count := 0
	ordered := append(sortable, []string{}...)
	for _, v := range names {
		if unknown[v] {
			ordered = append(ordered, v)
		}
	}
	for _, v := range ordered {
		item := versions[v]
		if latest[v] {
			item.Reasons = append(item.Reasons, "latest_n")
		}
		if protected[v] {
			item.Reasons = append(item.Reasons, "channel")
		}
		if active[v] {
			item.Reasons = append(item.Reasons, "in_use")
		}
		if unknown[v] {
			item.Reasons = append(item.Reasons, "uncomparable")
		}
		if len(item.Reasons) == 0 {
			if count < VersionLimit {
				item.Selected = true
				selected[v] = true
				count++
				item.Reasons = append(item.Reasons, "outside_latest_n")
			} else {
				item.Reasons = append(item.Reasons, "cycle_limit")
			}
		}
		out = append(out, *item)
	}
	ids := map[string]bool{}
	for _, v := range views {
		if v.Resource.Application == storage && v.Current && !v.Retired && selected[v.Resource.Version] {
			ids[v.Resource.ID] = true
		}
	}
	return out, ids
}
func (s *Service) Preview(ctx context.Context, key string) (Preview, error) {
	return s.preview(ctx, key, false)
}
func (s *Service) preview(ctx context.Context, key string, automatic bool) (Preview, error) {
	e, ok := s.Registry.Lookup(key)
	if !ok || !e.Active() || e.Protocol == nil {
		return Preview{}, store.ErrSourceInactive
	}
	ctx, finish, err := s.DB.ApplicationWork(ctx, e.StorageID())
	if err != nil {
		return Preview{}, err
	}
	defer finish()
	policy, hash, err := s.DB.Retention(key)
	if err != nil {
		return Preview{}, err
	}
	if automatic && !policy.Enabled {
		return Preview{}, store.ErrConflict
	}
	releaseBudget, err := s.Downloads.AcquireHTTPReader()
	if err != nil {
		return Preview{}, err
	}
	defer releaseBudget()
	channels := map[string]store.RetentionChannel{}
	if len(e.Descriptor.Channels) == 0 {
		return Preview{}, ErrChannels
	}
	for _, name := range e.Descriptor.Channels {
		release, err := s.Catalog.Release(ctx, key, name)
		if err != nil || release.Version == "" {
			return Preview{}, ErrChannels
		}
		cached, err := s.DB.Channel(e.StorageID(), name)
		if err != nil || cached.Version != release.Version {
			return Preview{}, ErrChannels
		}
		channels[name] = store.RetentionChannel{Version: cached.Version, FetchedAt: cached.FetchedAt, ExpiresAt: cached.ExpiresAt}
	}
	versions, ids := Select(e.Protocol, e.StorageID(), policy.KeepLatest, s.Downloads.Snapshot(), channels)
	guard := store.RetentionGuard{Automatic: automatic, SourceFence: store.SourceFence{AppRuntimeRevision: e.RuntimeRevision, VendorRuntimeRevision: e.VendorRuntimeRevision}, Hash: hash, Channels: channels, Versions: versions}
	job, err := s.Downloads.PreviewRetention(e.StorageID(), ids, guard)
	if err != nil {
		return Preview{}, err
	}
	count := 0
	for _, v := range versions {
		if v.Selected {
			count++
		}
	}
	return Preview{ID: job.ID, Versions: versions, SelectedVersions: count, LogicalBytes: job.LogicalBytes, ReclaimableBytes: job.ReclaimableBlobBytes, Expires: job.Expires}, nil
}
func (s *Service) Execute(ctx context.Context, key, id string) (store.RetentionReceipt, error) {
	e, ok := s.Registry.Lookup(key)
	if !ok || e.Protocol == nil {
		return store.RetentionReceipt{}, store.ErrSourceInactive
	}
	receipt, err := s.Downloads.CleanupRetention(ctx, e.StorageID(), id)
	s.record(e, receipt, err)
	return receipt, err
}
func (s *Service) record(e application.Entry, receipt store.RetentionReceipt, err error) {
	now := time.Now().UTC()
	status := Status{Attempt: now, Outcome: "success", RetiredVersions: receipt.RetiredVersions, LogicalBytes: receipt.LogicalBytes}
	old, _ := s.DB.RetentionStatus(e.UID)
	var prev Status
	_ = json.Unmarshal(old, &prev)
	status.Success = prev.Success
	if err == nil {
		status.Success = &now
	} else {
		status.Outcome = "failure"
		if errors.Is(err, ErrChannels) || errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrSourceInactive) || errors.Is(err, store.ErrExpired) || errors.Is(err, context.Canceled) {
			status.Outcome = "skip"
		}
		status.Reason = "retention_execution_failed"
		if status.Outcome == "skip" {
			status.Reason = "policy_source_or_channel_changed"
		}
		if errors.Is(err, ErrChannels) {
			status.Reason = "channel_unavailable"
		}
	}
	raw, _ := json.Marshal(status)
	_ = s.DB.SaveRetentionStatus(e.UID, raw)
}

// NextCheck is the time of the next scheduled pass, nil while Run is not active.
func (s *Service) NextCheck() *time.Time {
	n := s.next.Load()
	if n == 0 {
		return nil
	}
	value := time.Unix(0, n).UTC()
	return &value
}

// Status returns the last recorded run of the application, nil before the first.
func (s *Service) Status(uid string) (*Status, error) {
	raw, err := s.DB.RetentionStatus(uid)
	if err != nil {
		return nil, err
	}
	var value Status
	if err = json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if value.Attempt.IsZero() {
		return nil, nil
	}
	return &value, nil
}
func (s *Service) Run(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)
	s.next.Store(time.Now().Add(Interval).UnixNano())
	defer s.next.Store(0)
	tick := time.NewTicker(Interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-tick.C:
			s.next.Store(at.Add(Interval).UnixNano())
			s.Pass(ctx)
			if s.AutomaticPrewarm != nil {
				s.AutomaticPrewarm(ctx)
			}
		}
	}
}
func (s *Service) Pass(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	if s.DB.DeleteExpiredCleanupPreviews(time.Now()) != nil {
		return
	}
	for _, e := range s.Registry.Entries() {
		if ctx.Err() != nil {
			return
		}
		if e.Protocol == nil || !e.Active() {
			continue
		}
		r, _, err := s.DB.Retention(e.Descriptor.ID)
		if err != nil {
			s.record(e, store.RetentionReceipt{}, err)
			continue
		}
		if !r.Enabled {
			continue
		}
		preview, err := s.preview(ctx, e.Descriptor.ID, true)
		if err != nil {
			s.record(e, store.RetentionReceipt{}, err)
			continue
		}
		_, _ = s.Execute(ctx, e.Descriptor.ID, preview.ID)
	}
}
