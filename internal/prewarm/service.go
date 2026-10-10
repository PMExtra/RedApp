// Package prewarm owns one bounded worker, not a general queue or job platform.
package prewarm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/catalog"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/httpcache"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/logging"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/warmplan"
	"github.com/PMExtra/RedApp/presets"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

var ErrBusy = errors.New("prewarm worker busy")

// jobFinished is nil in production. Tests set it to run after a job's terminal
// state is persisted and before the job releases the worker slot.
var jobFinished func()
var ErrInvalid = errors.New("invalid prewarm input")

// ErrRunning reports that a running job cannot be retried.
var ErrRunning = errors.New("prewarm job is still running")

type activeJob struct {
	UID, ID string
	Done    chan struct{}
	Cancel  context.CancelFunc
	Budget  *warmplan.Budget
}
type Service struct {
	DB        *store.Store
	Registry  *application.Registry
	Catalog   *catalog.Service
	Downloads *download.Manager
	HTTP      *httpcache.Service
	ctx       context.Context
	cancel    context.CancelFunc
	active    atomic.Pointer[activeJob]
	closed    atomic.Bool
	wg        sync.WaitGroup
	// mu orders Start's closed check and wg.Add against Close; it is never
	// held during I/O.
	mu  sync.Mutex
	log *slog.Logger
}

// Option configures a Service at construction.
type Option func(*Service)

// WithLogger sets the logger for job outcomes and automatic prewarm failures
// (default: discard).
func WithLogger(log *slog.Logger) Option { return func(s *Service) { s.log = log } }

func New(db *store.Store, registry *application.Registry, catalog *catalog.Service, downloads *download.Manager, http *httpcache.Service, options ...Option) (*Service, error) {
	if err := db.RecoverPrewarm(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{DB: db, Registry: registry, Catalog: catalog, Downloads: downloads, HTTP: http, ctx: ctx, cancel: cancel}
	for _, option := range options {
		option(s)
	}
	s.log = logging.For(s.log, "prewarm")
	return s, nil
}
func (s *Service) Close() { s.mu.Lock(); s.closed.Store(true); s.cancel(); s.mu.Unlock(); s.wg.Wait() }

// errInterrupted ends a job whose item failed because shutdown has started.
var errInterrupted = errors.New("prewarm interrupted by shutdown")

// shuttingDown reports whether this worker or a service its items depend on
// is closing. The process closes the worker first, but the download manager
// or HTTP cache may still close while a job runs. A service the worker was
// built without never closes.
func (s *Service) shuttingDown() bool {
	return s.ctx.Err() != nil || s.Downloads != nil && s.Downloads.Closed() || s.HTTP != nil && s.HTTP.Closed()
}
func fingerprint(value any) string {
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func validate(entry application.Entry, in *warmplan.Input) error {
	if !identity.ValidUID(in.RequestID) {
		return ErrInvalid
	}
	if in.Limits == (warmplan.Limits{}) {
		in.Limits = warmplan.DefaultLimits()
	}
	if !in.Limits.Valid() {
		return ErrInvalid
	}
	if entry.Protocol != nil {
		platform, ok := entry.Protocol.(application.PlatformProtocol)
		if !ok || in.Target == "" || len(in.Platforms) == 0 || len(in.Paths) > 0 || len(in.Indexes) > 0 || in.Manifest != "" || in.Match != nil {
			return ErrInvalid
		}
		allowed := map[string]bool{}
		for _, p := range platform.Platforms() {
			allowed[p.ID] = true
		}
		for _, id := range in.Platforms {
			if !allowed[id] {
				return ErrInvalid
			}
		}
		if !entry.HasChannel(in.Target) {
			version, err := entry.Protocol.ValidateVersion(in.Target)
			if err != nil || version != in.Target {
				return ErrInvalid
			}
		}
		sort.Strings(in.Platforms)
		in.Platforms = unique(in.Platforms)
		return nil
	}
	if entry.Provider != application.HttpCache || in.Target != "" || len(in.Platforms) > 0 || !utf8.ValidString(in.Manifest) || len(in.Manifest) > 2<<20 {
		return ErrInvalid
	}
	for _, line := range strings.Split(in.Manifest, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			in.Paths = append(in.Paths, line)
		}
	}
	in.Manifest = ""
	for _, paths := range [][]string{in.Paths, in.Indexes} {
		if len(paths) > 100000 {
			return ErrInvalid
		}
		for _, p := range paths {
			if !strings.HasPrefix(p, "/") || strings.Contains(p, "://") || len(p) > 4096 || pathmatch.ValidatePath(strings.TrimSuffix(p, "/")) != nil && p != "/" {
				return ErrInvalid
			}
		}
	}
	if in.Match != nil {
		if _, err := pathmatch.Compile(*in.Match); err != nil {
			return ErrInvalid
		}
	}
	if len(in.Paths) == 0 && len(in.Indexes) == 0 && in.Match == nil {
		return ErrInvalid
	}
	return nil
}

// Start starts a job. A repeated request ID returns the existing job with
// created false.
func (s *Service) Start(ctx context.Context, key string, in warmplan.Input, automatic bool) (store.PrewarmJob, bool, error) {
	e, ok := s.Registry.Lookup(key)
	if !ok || !e.Active() {
		return store.PrewarmJob{}, false, store.ErrSourceInactive
	}
	if err := s.DB.PrunePrewarm(); err != nil {
		return store.PrewarmJob{}, false, err
	}
	if err := validate(e, &in); err != nil {
		return store.PrewarmJob{}, false, err
	}
	inputHash := in
	inputHash.RequestID = ""
	hash := fingerprint(struct {
		Storage string
		Input   warmplan.Input
	}{e.StorageID(), inputHash})
	if old, err := s.DB.PrewarmRequest(e.UID, in.RequestID); err == nil {
		if old.State != "running" && time.Now().After(old.Updated.Add(24*time.Hour)) {
			return old, false, store.ErrExpired
		}
		if old.Fingerprint != hash {
			return old, false, store.ErrConflict
		}
		return old, false, nil
	}
	jobID, err := fsutil.RandomID()
	if err != nil {
		return store.PrewarmJob{}, false, err
	}
	// The job context exists before the job is visible in s.active, so Cancel
	// never races with its construction.
	jobCtx, cancelJob := context.WithCancel(s.ctx)
	current := &activeJob{UID: e.UID, ID: jobID, Done: make(chan struct{}), Cancel: cancelJob, Budget: &warmplan.Budget{Max: in.Limits.MaxDownloadBytes}}
	// mu only orders the closed check and wg.Add against Close; the database
	// work below runs after it is released, with the worker slot reserved.
	s.mu.Lock()
	for {
		if s.closed.Load() {
			s.mu.Unlock()
			cancelJob()
			return store.PrewarmJob{}, false, context.Canceled
		}
		if s.active.CompareAndSwap(nil, current) {
			break
		}
		existing := s.active.Load()
		s.mu.Unlock()
		if existing == nil {
			s.mu.Lock()
			continue
		}
		job, _ := s.DB.PrewarmJob(existing.UID, existing.ID)
		if job.ID == "" || job.State == "running" {
			cancelJob()
			return job, false, ErrBusy
		}
		// The worker persists the terminal state before it releases the slot;
		// wait for that release instead of reporting a finished job as busy.
		select {
		case <-existing.Done:
		case <-ctx.Done():
			cancelJob()
			return store.PrewarmJob{}, false, ctx.Err()
		}
		s.mu.Lock()
	}
	s.wg.Add(1)
	s.mu.Unlock()
	release := func() {
		cancelJob()
		s.active.CompareAndSwap(current, nil)
		s.wg.Done()
	}
	ctxWork, finish, err := s.DB.ApplicationWork(jobCtx, e.StorageID())
	if err != nil {
		release()
		return store.PrewarmJob{}, false, err
	}
	work, cancel := context.WithTimeout(ctxWork, time.Duration(in.Limits.MaxDurationSeconds)*time.Second)
	job := store.PrewarmJob{SourceFence: store.SourceFence{AppRuntimeRevision: e.RuntimeRevision, VendorRuntimeRevision: e.VendorRuntimeRevision}, ID: current.ID, AppUID: e.UID, StorageID: e.StorageID(), RequestID: in.RequestID, Fingerprint: hash, State: "running", Created: time.Now().UTC(), Updated: time.Now().UTC(), Input: in, Target: in.Target, Platforms: in.Platforms, Limits: in.Limits, Ignored: map[string]int{}, Automatic: automatic}
	if automatic {
		cfg, err := s.DB.ApplicationConfiguration(key)
		if err != nil {
			cancel()
			finish()
			release()
			return job, false, err
		}
		job.PolicyHash = fingerprint(cfg.Effective["prewarm"])
	}
	if err = s.DB.CreatePrewarm(job); err != nil {
		cancel()
		finish()
		release()
		return job, false, err
	}
	go func() {
		defer s.wg.Done()
		defer close(current.Done)
		defer s.active.CompareAndSwap(current, nil)
		defer cancelJob()
		defer cancel()
		defer finish()
		s.run(work, e, job, current.Budget)
		if jobFinished != nil {
			jobFinished()
		}
	}()
	return job, true, nil
}
func (s *Service) entry(job store.PrewarmJob) (application.Entry, error) {
	for _, e := range s.Registry.Entries() {
		if e.UID == job.AppUID {
			if e.StorageID() != job.StorageID || e.RuntimeRevision != job.AppRuntimeRevision || e.VendorRuntimeRevision != job.VendorRuntimeRevision || !e.Active() {
				return e, store.ErrSourceInactive
			}
			if job.Automatic {
				cfg, err := s.DB.ApplicationConfiguration(e.Descriptor.ID)
				if err != nil || fingerprint(cfg.Effective["prewarm"]) != job.PolicyHash {
					return e, store.ErrSourceInactive
				}
			}
			return e, nil
		}
	}
	return application.Entry{}, store.ErrSourceInactive
}
func (s *Service) run(ctx context.Context, entry application.Entry, job store.PrewarmJob, budget *warmplan.Budget) {
	errorsCount := 0
	ordinal := 0
	seen := map[string]bool{}
	for _, key := range job.Input.RetrySkip {
		seen[key] = true
	}
	channel := job.Input.Target
	var matcher *pathmatch.Matcher
	if job.Input.Match != nil {
		matcher, _ = pathmatch.Compile(*job.Input.Match)
	}
	// A failed progress write is retried by the next one; only the last
	// failure is reported, with the job outcome.
	var updateErr error
	update := func() { job.Bytes = budget.Used(); job.Updated = time.Now().UTC(); updateErr = s.DB.UpdatePrewarm(job) }
	emit := func(key string) error {
		if seen[key] {
			return nil
		}
		if matcher != nil && !matcher.Match(key) {
			return nil
		}
		if ordinal >= job.Input.Limits.MaxFiles {
			return warmplan.ErrLimited
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		current, err := s.entry(job)
		if err != nil {
			return err
		}
		seen[key] = true
		ordinal++
		item := warmplan.Item{Key: key, Status: "pending"}
		if err = s.DB.AddPrewarmItem(job.ID, ordinal, item); err != nil {
			return err
		}
		before := budget.Used()
		if current.Protocol == nil {
			item = s.HTTP.Warm(ctx, current, key, budget)
		} else {
			item = s.warmRelease(ctx, current, job.ResolvedVersion, key, budget)
		}
		item.Key = key
		item.Bytes = budget.Used() - before
		job.Completed++
		interrupted := false
		switch item.Status {
		case "cached", "downloaded", "not_modified", "ttl_fallback":
			job.Succeeded++
		default:
			// A download manager or HTTP cache that is closing fails the
			// item; that is the shutdown interrupting the job, not an error.
			if interrupted = s.shuttingDown(); interrupted {
				item.Status, item.Reason = "skipped", "interrupted_by_shutdown"
			} else {
				errorsCount++
			}
		}
		if err = s.DB.AddPrewarmItem(job.ID, ordinal, item); err != nil {
			return err
		}
		update()
		if interrupted {
			return errInterrupted
		}
		if budget.Limited() {
			return warmplan.ErrLimited
		}
		return nil
	}
	var err error
	if entry.Protocol != nil {
		release, resolveErr := s.Catalog.Release(ctx, entry.Descriptor.ID, job.Input.Target)
		if resolveErr != nil {
			err = resolveErr
		} else {
			selector := entry.Protocol.(application.PlatformProtocol)
			keys, selectErr := selector.SelectArtifacts(release, job.Input.Platforms)
			if selectErr != nil {
				err = selectErr
			} else {
				job.SuccessFingerprint = fingerprint(struct {
					Storage, Policy string
					Version         string
					Keys            []string
					Artifacts       []application.VerifiedArtifact
					Platforms       []string
				}{entry.StorageID(), job.PolicyHash, release.Version, keys, release.Artifacts, job.Input.Platforms})
				if job.Automatic {
					done, _ := s.DB.PrewarmSuccess(job.AppUID, job.Input.Target, job.SuccessFingerprint)
					if done && s.complete(entry, release, keys) {
						job.State = "completed"
						job.Reason = "unchanged_target"
						update()
						s.logOutcome(entry, job, nil, updateErr)
						return
					}
				}
				job.ResolvedVersion = release.Version
				update()
				for _, key := range keys {
					if err = emit(key); err != nil {
						break
					}
				}
			}
		}
	} else {
		for _, p := range job.Input.Paths {
			if err = emit(strings.TrimSuffix(p, "/")); err != nil {
				break
			}
		}
		if err == nil && len(job.Input.Indexes) > 0 {
			err = s.HTTP.Discover(ctx, entry, job.Input.Indexes, job.Input.Limits, budget, emit, func(reason string) {
				if len(job.Ignored) < 16 {
					job.Ignored[reason]++
				}
			})
		}
		if err == nil && len(job.Input.Indexes) == 0 && job.Input.Match != nil {
			rows, listErr := s.HTTP.ListEntry(entry)
			if listErr != nil {
				err = listErr
			} else {
				for _, row := range rows {
					if err = emit("/" + row.Path); err != nil {
						break
					}
				}
			}
		}
	}
	job.State = "completed"
	if err != nil || errorsCount > 0 {
		job.State = "completed_with_errors"
		job.Reason = "prewarm_failed"
	}
	if errors.Is(err, warmplan.ErrLimited) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		job.State = "limited"
		job.Reason = "task_limit"
	} else if ctx.Err() != nil || errors.Is(err, store.ErrSourceInactive) {
		job.State = "cancelled"
		job.Reason = "cancelled_or_configuration_changed"
	}
	if errors.Is(err, errInterrupted) || s.shuttingDown() {
		job.State = "interrupted"
		job.Reason = "interrupted_by_shutdown"
	}
	var successErr error
	if job.Automatic && job.State == "completed" {
		successErr = s.DB.SavePrewarmSuccess(job.AppUID, channel, job.SuccessFingerprint)
	}
	update()
	s.logOutcome(entry, job, err, errors.Join(successErr, updateErr))
}

// logOutcome logs one record per finished job. An automatic job that found
// its target unchanged logs only at debug level: it repeats every pass.
func (s *Service) logOutcome(entry application.Entry, job store.PrewarmJob, err, stateErr error) {
	attrs := []any{slog.String("app", entry.Descriptor.ID), slog.String("job_id", job.ID), slog.Bool("automatic", job.Automatic), slog.String("state", job.State), slog.Int("completed", job.Completed), slog.Int("succeeded", job.Succeeded), slog.Int64("bytes", job.Bytes)}
	if job.Reason != "" {
		attrs = append(attrs, slog.String("reason", job.Reason))
	}
	if job.ResolvedVersion != "" {
		attrs = append(attrs, slog.String("version", job.ResolvedVersion))
	}
	level := slog.LevelInfo
	switch {
	case job.Reason == "unchanged_target":
		level = slog.LevelDebug
	case job.State == "completed_with_errors":
		level = slog.LevelWarn
	}
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, warmplan.ErrLimited) && !errors.Is(err, errInterrupted) {
		attrs = append(attrs, logging.Error(err))
	}
	if stateErr != nil {
		level = slog.LevelError
		attrs = append(attrs, slog.String("state_error", logging.Redact(stateErr.Error())))
	}
	s.log.Log(context.Background(), level, "prewarm job finished", attrs...)
}
func (s *Service) warmRelease(ctx context.Context, entry application.Entry, version, key string, budget *warmplan.Budget) warmplan.Item {
	item := warmplan.Item{Key: key, Status: "failed", Reason: "release_failed"}
	resource, err := s.Catalog.Authorize(ctx, entry.Descriptor.ID, version, key)
	if err != nil {
		return item
	}
	reader, _, err := s.Downloads.Acquire(ctx, resource)
	if err != nil {
		return item
	}
	defer reader.Close()
	if reader.Kind == "cache_hit" {
		return warmplan.Item{Status: "cached"}
	}
	if resource.Size != nil && budget.CheckLength(*resource.Size) != nil {
		return warmplan.Item{Status: "skipped", Reason: "read_limit"}
	}
	if resource.Size != nil {
		_, err = io.CopyN(io.Discard, budget.Reader(ctx, reader), *resource.Size)
		if err == nil {
			err = reader.WaitVerified()
		}
	} else {
		_, err = io.Copy(io.Discard, budget.Reader(ctx, reader))
	}
	if err != nil {
		return item
	}
	item.Reason = ""
	item.Status = "downloaded"
	return item
}

// Wait blocks until the job is no longer running, or ctx ends, and returns its
// final status.
func (s *Service) Wait(ctx context.Context, uid, id string) (store.PrewarmJob, error) {
	if current := s.active.Load(); current != nil && current.UID == uid && current.ID == id {
		select {
		case <-current.Done:
		case <-ctx.Done():
			return store.PrewarmJob{}, ctx.Err()
		}
	}
	return s.Status(uid, id)
}

func (s *Service) Status(uid, id string) (store.PrewarmJob, error) {
	job, err := s.DB.PrewarmJob(uid, id)
	if err == nil && job.State != "running" && time.Now().After(job.Updated.Add(24*time.Hour)) {
		return store.PrewarmJob{}, store.ErrNotFound
	}
	if current := s.active.Load(); current != nil && current.UID == uid && current.ID == id && err == nil {
		job.Bytes = current.Budget.Used()
	}
	return job, err
}
func (s *Service) Cancel(uid, id string) error {
	if _, err := s.DB.PrewarmJob(uid, id); err != nil {
		return err
	}
	if current := s.active.Load(); current != nil && current.UID == uid && current.ID == id {
		current.Cancel()
	}
	return nil
}

// Retry starts a job for the unsuccessful items of a finished job, with the
// idempotency rules of Start. A missing or expired job returns store.ErrNotFound.
func (s *Service) Retry(ctx context.Context, key, id, requestID string) (store.PrewarmJob, bool, error) {
	e, ok := s.Registry.Lookup(key)
	if !ok {
		return store.PrewarmJob{}, false, store.ErrSourceInactive
	}
	old, err := s.Status(e.UID, id)
	if err != nil {
		return old, false, err
	}
	if old.State == "running" {
		return old, false, ErrRunning
	}
	in := old.Input
	in.RequestID = requestID
	if old.ResolvedVersion != "" {
		in.Target = old.ResolvedVersion
	}
	for page := 1; ; page++ {
		items, total, err := s.DB.PrewarmItems(e.UID, id, page, 100)
		if err != nil {
			return old, false, err
		}
		for _, item := range items {
			switch item.Status {
			case "cached", "downloaded", "not_modified", "ttl_fallback":
				in.RetrySkip = append(in.RetrySkip, item.Key)
			}
		}
		if page*100 >= total {
			break
		}
	}
	return s.Start(ctx, key, in, false)
}
func (s *Service) Automatic(ctx context.Context) {
	if err := s.DB.PrunePrewarm(); err != nil {
		s.log.Warn("pruning finished prewarm jobs failed", logging.Error(err))
	}
	for _, e := range s.Registry.Entries() {
		if ctx.Err() != nil {
			return
		}
		if e.Protocol == nil || !e.Active() {
			continue
		}
		cfg, err := s.DB.ApplicationConfiguration(e.Descriptor.ID)
		if err != nil {
			s.log.Warn("automatic prewarm skipped: configuration unreadable", slog.String("app", e.Descriptor.ID), logging.Error(err))
			continue
		}
		raw, _ := json.Marshal(cfg.Effective["prewarm"])
		var policy presets.Prewarm
		if json.Unmarshal(raw, &policy) != nil || !policy.Enabled {
			continue
		}
		for _, channel := range policy.Channels {
			requestID, err := fsutil.RandomID()
			if err != nil {
				return
			}
			job, _, startErr := s.Start(ctx, e.Descriptor.ID, warmplan.Input{RequestID: requestID, Target: channel, Platforms: policy.Platforms, Limits: warmplan.DefaultLimits()}, true)
			if errors.Is(startErr, ErrBusy) {
				return
			}
			if startErr != nil && !errors.Is(startErr, context.Canceled) {
				s.log.Warn("automatic prewarm did not start", slog.String("app", e.Descriptor.ID), slog.String("channel", channel), logging.Error(startErr))
			}
			if startErr == nil {
				if current := s.active.Load(); current != nil && current.ID == job.ID {
					select {
					case <-ctx.Done():
						return
					case <-current.Done:
					}
				}
			}
		}
	}
}

func unique(values []string) []string {
	out := values[:0]
	for _, v := range values {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}

// Automatic success is reusable only while the selected complete files remain.
func (s *Service) complete(entry application.Entry, release application.Release, keys []string) bool {
	wanted := map[string]string{}
	for _, artifact := range release.Artifacts {
		for _, key := range keys {
			if artifact.Key == key {
				wanted[key] = artifact.SHA256
			}
		}
	}
	for _, view := range s.Downloads.SnapshotFor(entry.StorageID(), release.Version) {
		if !view.Current || view.Retired || view.State != "complete" || wanted[view.Resource.Key] != view.Resource.Hash {
			continue
		}
		if st, err := os.Stat(view.Path); err == nil && st.Mode().IsRegular() && st.Size() == view.Bytes {
			delete(wanted, view.Resource.Key)
		}
	}
	return len(wanted) == 0
}
