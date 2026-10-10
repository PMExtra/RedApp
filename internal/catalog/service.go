// Package catalog owns metadata caching, channel expiry, fetch coalescing and
// durable resource authorization for every registered application.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
)

type flight struct {
	done    chan struct{}
	release application.Release
	err     error
}
type Service struct {
	db       *store.Store
	registry *application.Registry
	mu       sync.Mutex
	flights  map[string]*flight
	active   map[string]int
}

func New(db *store.Store, registry *application.Registry) *Service {
	return &Service{db: db, registry: registry, flights: map[string]*flight{}, active: map[string]int{}}
}

// Release returns only freshly verified metadata. Expired channels never fall
// back to stale data after upstream failure, while immutable versions do not expire.
func (s *Service) Release(ctx context.Context, app, target string) (application.Release, error) {
	e, ok := s.registry.Lookup(app)
	if !ok {
		return application.Release{}, application.ErrNotFound
	}
	return s.release(ctx, e, target)
}

func sourceFence(e application.Entry) store.SourceFence {
	return store.SourceFence{AppRuntimeRevision: e.RuntimeRevision, VendorRuntimeRevision: e.VendorRuntimeRevision}
}

// release carries one immutable runtime snapshot through channel resolution,
// verification and persistence. A concurrent registry replacement cannot mix
// the previous protocol with the new source namespace.
//
// The durable cache is read, verified and re-persisted without s.mu, which
// only guards the flight map. A caller that misses the cache just before a
// flight publishes may start a second flight; that flight checks the cache
// again before contacting the upstream, so a completed fetch is never
// repeated.
func (s *Service) release(ctx context.Context, e application.Entry, target string) (application.Release, error) {
	if e.Protocol == nil {
		return application.Release{}, application.ErrNotFound
	}
	app := e.StorageID()
	if err := s.db.CheckSourceActive(app, sourceFence(e)); err != nil {
		return application.Release{}, err
	}
	channel := e.HasChannel(target)
	if !channel {
		v, err := e.Protocol.ValidateVersion(target)
		if err != nil || v != target {
			return application.Release{}, application.ErrNotFound
		}
	}
	r, version, hit, err := s.cached(e, target, channel)
	if err != nil {
		return application.Release{}, err
	}
	if hit && version != "" {
		return s.release(ctx, e, version)
	}
	if hit {
		return r, nil
	}
	key := fmt.Sprintf("%s\x00%d/%d\x00%s", app, e.Revision, e.VendorRevision, target)
	s.mu.Lock()
	f := s.flights[key]
	if f == nil {
		if s.active[e.MetricsID()] >= 32 {
			s.mu.Unlock()
			return application.Release{}, application.ErrBusy
		}
		f = &flight{done: make(chan struct{})}
		s.flights[key] = f
		s.active[e.MetricsID()]++
		workCtx, finish, err := s.db.ApplicationWork(context.Background(), e.StorageID())
		if err != nil {
			delete(s.flights, key)
			s.active[e.MetricsID()]--
			s.mu.Unlock()
			return application.Release{}, err
		}
		go func() { defer finish(); s.fetch(workCtx, e, target, channel, key, f) }()
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return application.Release{}, ctx.Err()
	case <-f.done:
		if f.err == nil {
			if err := s.db.CheckSourceActive(app, sourceFence(e)); err != nil {
				return application.Release{}, err
			}
		}
		return f.release, f.err
	}
}

// cached looks target up in the durable cache. A fresh channel hit returns
// the version it points to; a release hit returns the release, re-verified
// under the compiled trust policy. A miss, an expired channel or a cached
// release that no longer verifies reports hit=false, so the caller fetches
// it again without deleting the durable, immutable resource binding.
func (s *Service) cached(e application.Entry, target string, channel bool) (release application.Release, version string, hit bool, err error) {
	app := e.StorageID()
	if channel {
		cached, err := s.db.Channel(app, target)
		if errors.Is(err, store.ErrNotFound) {
			return application.Release{}, "", false, nil
		}
		if err != nil {
			return application.Release{}, "", false, err
		}
		ttl := e.Descriptor.DefaultChannelTTLSeconds
		now := time.Now()
		expires := cached.FetchedAt.Add(time.Duration(ttl) * time.Second)
		if cached.ExpiresAt.Before(expires) {
			expires = cached.ExpiresAt
		}
		if !cached.FetchedAt.After(now) && now.Before(expires) {
			return application.Release{}, cached.Version, true, nil
		}
		return application.Release{}, "", false, nil
	}
	m, err := s.db.Release(app, target)
	if errors.Is(err, store.ErrNotFound) {
		return application.Release{}, "", false, nil
	}
	if err != nil {
		return application.Release{}, "", false, err
	}
	r, err := e.Protocol.VerifyRelease(target, application.Envelope{Raw: m.Raw, Signature: m.Signature})
	if err != nil {
		return application.Release{}, "", false, nil
	}
	// Resource bindings are checked transactionally when trust changes.
	if m.TrustRevision != e.Descriptor.TrustRevision {
		if err = s.persist(e, r, m.FetchedAt); errors.Is(err, store.ErrImmutableRelease) {
			return application.Release{}, "", false, nil
		}
		if err != nil {
			return application.Release{}, "", false, err
		}
	}
	return r, "", true, nil
}

func (s *Service) fetch(parent context.Context, e application.Entry, target string, isChannel bool, key string, f *flight) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	app := e.StorageID()
	// A flight that published while this one was being started has already
	// filled the cache.
	release, version, hit, err := s.cached(e, target, isChannel)
	switch {
	case err != nil: // reported below like a fetch failure
	case hit && version != "":
		release, err = s.release(ctx, e, version)
	case hit: // release is the cached release
	case isChannel:
		var resolved application.ChannelResolution
		resolved, err = e.Protocol.ResolveChannel(ctx, target)
		if err == nil {
			v, versionErr := e.Protocol.ValidateVersion(resolved.Version)
			if versionErr != nil || v != resolved.Version {
				err = fmt.Errorf("%w: noncanonical channel version", application.ErrUntrusted)
			}
		}
		if err == nil && resolved.Envelope != nil {
			release, err = s.verifyAndPersist(e, resolved.Version, *resolved.Envelope)
		} else if err == nil {
			release, err = s.release(ctx, e, resolved.Version)
		}
		if err == nil {
			ttl := e.Descriptor.DefaultChannelTTLSeconds
			now := time.Now().UTC()
			err = s.db.PutChannel(store.Channel{AppID: app, Name: target, Version: release.Version, FetchedAt: now, ExpiresAt: now.Add(time.Duration(ttl) * time.Second)}, sourceFence(e))
		}
	default:
		var envelope application.Envelope
		envelope, err = e.Protocol.FetchRelease(ctx, target)
		if err == nil {
			release, err = s.verifyAndPersist(e, target, envelope)
		}
	}
	if err != nil {
		event := store.Event{AppID: e.MetricsID(), Category: "metadata", Code: "metadata_fetch_failed", Message: err.Error()}
		if !isChannel {
			event.Version = target
		}
		if errors.Is(err, store.ErrImmutableRelease) {
			event.Code = "metadata_immutable_conflict"
		}
		var upstream *application.HTTPError
		if errors.As(err, &upstream) {
			event.UpstreamStatus = &upstream.Status
		}
		_ = s.db.RecordEvent(event)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f.release = release
	f.err = err
	delete(s.flights, key)
	s.active[e.MetricsID()]--
	close(f.done)
}

func (s *Service) persist(e application.Entry, r application.Release, fetched time.Time) error {
	resources := make([]store.Resource, 0, len(r.Artifacts))
	for _, a := range r.Artifacts {
		resources = append(resources, store.Resource{AppID: e.StorageID(), Version: r.Version, Key: a.Key, SourceURL: a.Source, SHA256: a.SHA256, ExpectedSize: a.Size})
	}
	return s.db.PutRelease(store.ReleaseMetadata{AppID: e.StorageID(), Version: r.Version, Raw: r.Envelope.Raw, Signature: r.Envelope.Signature, TrustRevision: e.Descriptor.TrustRevision, FetchedAt: fetched}, resources, sourceFence(e))
}

func (s *Service) Represent(ctx context.Context, app string, op application.Operation, publicBase string) (application.Representation, error) {
	e, ok := s.registry.Lookup(app)
	if !ok || e.Protocol == nil || (op.Kind != application.ChannelOperation && op.Kind != application.MetadataOperation) {
		return application.Representation{}, application.ErrNotFound
	}
	r, err := s.release(ctx, e, op.Target)
	if err != nil {
		return application.Representation{}, err
	}
	return e.Protocol.Render(r, op, publicBase)
}

func (s *Service) Authorize(ctx context.Context, app, version, key string) (download.Resource, error) {
	e, ok := s.registry.Lookup(app)
	if !ok || e.Protocol == nil {
		return download.Resource{}, application.ErrNotFound
	}
	// Artifact paths use immutable versions. A channel is not an artifact identity.
	v, err := e.Protocol.ValidateVersion(version)
	if err != nil || v != version {
		return download.Resource{}, application.ErrNotFound
	}
	r, err := s.release(ctx, e, version)
	if err != nil {
		return download.Resource{}, err
	}
	for _, a := range r.Artifacts {
		if a.Key != key {
			continue
		}
		bound, err := s.db.Resource(e.StorageID(), version, key)
		if err != nil {
			return download.Resource{}, err
		}
		if bound.SourceURL != a.Source || bound.SHA256 != a.SHA256 || !equalSize(bound.ExpectedSize, a.Size) {
			return download.Resource{}, fmt.Errorf("%w: persisted resource binding differs from trusted metadata", application.ErrUntrusted)
		}
		return download.Resource{Application: e.StorageID(), MetricsID: e.MetricsID(), SourceFence: sourceFence(e), Version: version, Key: key, ID: download.LogicalIdentity(e.StorageID(), version, key), Source: a.Source, Hash: a.SHA256, Size: a.Size, Labels: map[string]string{"app": app, "version": version, "name": key}}, nil
	}
	return download.Resource{}, application.ErrNotFound
}
func equalSize(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func (s *Service) Candidates(app, minimum string, views []download.View) (map[string]bool, []string, error) {
	// Cleanup compares version syntax without accessing an upstream. Management
	// remains available while a source is disabled or tombstoned.
	e, ok := s.registry.LookupAny(app)
	if !ok || e.Protocol == nil {
		return nil, nil, application.ErrNotFound
	}
	return candidates(e, e.StorageID(), minimum, views)
}

// CandidatesForSource selects one explicitly requested historical source. Its
// owning UID must match the public application; neither labels nor a caller's
// view list can expand cleanup to another app. No upstream is contacted.
func (s *Service) CandidatesForSource(app, storageID, minimum string, views []download.View) (map[string]bool, []string, error) {
	e, ok := s.registry.LookupAny(app)
	if !ok || e.Protocol == nil {
		return nil, nil, application.ErrNotFound
	}
	uid, _, ok := identity.ParseStorageID(storageID)
	if !ok || uid != e.UID {
		return nil, nil, application.ErrNotFound
	}
	if _, err := s.db.Source(storageID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			err = application.ErrNotFound
		}
		return nil, nil, err
	}
	return candidates(e, storageID, minimum, views)
}

func candidates(e application.Entry, storageID, minimum string, views []download.View) (map[string]bool, []string, error) {
	if v, err := e.Protocol.ValidateVersion(minimum); err != nil || v != minimum {
		return nil, nil, errors.New("invalid canonical minimum version")
	}
	ids := map[string]bool{}
	unknownSet := map[string]bool{}
	for _, view := range views {
		r := view.Resource
		if r.Application != storageID {
			continue
		}
		order, err := e.Protocol.CompareVersions(r.Version, minimum)
		if err != nil {
			unknownSet[r.Version] = true
		} else if order < 0 {
			ids[r.ID] = true
		}
	}
	unknown := make([]string, 0, len(unknownSet))
	for v := range unknownSet {
		unknown = append(unknown, v)
	}
	sort.Strings(unknown)
	return ids, unknown, nil
}

// verifyAndPersist verifies freshly fetched release metadata and stores it.
// Verification failures and conflicts with already trusted bindings are
// untrusted metadata; storage failures keep their own error.
func (s *Service) verifyAndPersist(e application.Entry, version string, envelope application.Envelope) (application.Release, error) {
	release, err := e.Protocol.VerifyRelease(version, envelope)
	if err != nil {
		return application.Release{}, fmt.Errorf("%w: %w", application.ErrUntrusted, err)
	}
	if err = s.persist(e, release, time.Now().UTC()); err != nil {
		if errors.Is(err, store.ErrImmutableRelease) {
			err = fmt.Errorf("%w: %w", application.ErrUntrusted, err)
		}
		return application.Release{}, err
	}
	return release, nil
}
