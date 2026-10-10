// Package httpcache stores mutable HTTP representations independently of release
// metadata. Observed content hashes protect storage, not publisher authenticity.
//
// A miss or a changed representation streams: one shared upstream fill writes
// a part file through a spool.Body that every concurrent reader follows, and
// only a complete body becomes a stored entry. Stored entries are pinned while
// they are read and collected once retired and unpinned.
package httpcache

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/logging"
	"github.com/PMExtra/RedApp/internal/spool"
	"github.com/PMExtra/RedApp/internal/store"
)

var ErrClosed = errors.New("HTTP cache is shutting down")
var ErrUpstream = errors.New("HTTP upstream response unavailable")
var ErrInvalidCleanup = errors.New("invalid HTTP cache cleanup request")

type Row struct {
	SourceURL    string     `json:"source_url"`
	GenerationID string     `json:"generation_id"`
	Path         string     `json:"path"`
	SizeBytes    int64      `json:"size_bytes"`
	SHA256       string     `json:"sha256"`
	FetchedAt    time.Time  `json:"fetched_at"`
	ValidatedAt  time.Time  `json:"validated_at"`
	LastAccessAt *time.Time `json:"last_access_at"`
	FreshUntil   time.Time  `json:"fresh_until"`
	ETag         string     `json:"etag"`
	storageID    string
	headers      http.Header
	accessBucket int64
	current      bool
}

func rowFromEntry(e store.HTTPCacheEntry) (*Row, error) {
	r := &Row{SourceURL: e.SourceURL, GenerationID: e.ID, Path: e.Path, SizeBytes: e.SizeBytes, SHA256: e.SHA256, FetchedAt: e.FetchedAt, ValidatedAt: e.ValidatedAt, FreshUntil: e.FreshUntil, storageID: e.StorageID, accessBucket: e.AccessBucket, current: e.Current}
	if err := json.Unmarshal(e.Headers, &r.headers); err != nil {
		return nil, err
	}
	if r.accessBucket > 0 {
		t := time.Unix(r.accessBucket+60, 0).UTC()
		r.LastAccessAt = &t
	}
	r.ETag = r.headers.Get("ETag")
	if r.ETag == "" {
		r.ETag = `"sha256-` + r.SHA256 + `"`
	}
	return r, nil
}

func rowsFromEntries(entries []store.HTTPCacheEntry) ([]Row, error) {
	out := make([]Row, 0, len(entries))
	for _, e := range entries {
		r, err := rowFromEntry(e)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, nil
}

type Service struct {
	sourceCursors   map[string]*list.Element
	sourceOrder     list.List
	cleanupMu       sync.Mutex
	cleanupStatus   AutomaticCleanupStatus
	cleanupCursors  map[string]cleanupCursor
	refreshRunning  bool
	previewBuilders int
	dir             string
	db              *store.Store
	budget          download.Budget
	// Upstream transfer policy of streamed fills: a body read waiting
	// idleTimeout without bytes fails an attempt; retry bounds resumption.
	idleTimeout time.Duration
	retry       spool.Retry
	// mu guards the maps below. Store calls made under mu keep entry rows,
	// pins and collection consistent; the longest is one entry publication.
	mu       sync.Mutex
	flights  map[string]*flight
	streams  map[string]*stream // fills not yet released, by entry ID
	pins     map[string]int     // stored entry ID -> holders
	readers  map[string]int     // public responses by storage ID and path
	verified map[string]bool    // entries whose body this process verified or wrote
	checks   spool.Checks       // lazy body verifications by entry ID
	closed   bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	now      func() time.Time
	log      *slog.Logger
}

// Option configures a Service at construction.
type Option func(*Service)

// WithClock replaces the wall clock used for freshness, access buckets and
// maintenance previews.
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithLogger sets the logger for the automatic cleanup scheduler (default:
// discard).
func WithLogger(log *slog.Logger) Option { return func(s *Service) { s.log = log } }

// WithTransferPolicy replaces the idle read timeout and the retry bounds of
// streamed upstream fills.
func WithTransferPolicy(idle time.Duration, retry spool.Retry) Option {
	return func(s *Service) { s.idleTimeout, s.retry = idle, retry }
}

func New(dir string, db *store.Store, budget download.Budget, options ...Option) (*Service, error) {
	if db == nil || budget == nil || budget.MaxArtifactBytes() <= 0 {
		return nil, errors.New("HTTP cache requires storage and shared limits")
	}
	for _, path := range []string{dir, filepath.Join(dir, "objects"), filepath.Join(dir, "objects", "http")} {
		if err := fsutil.EnsureDir(path); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{dir: filepath.Join(dir, "objects", "http"), db: db, budget: budget, idleTimeout: distributor.DefaultIdleTimeout, retry: spool.DefaultRetry(),
		flights: map[string]*flight{}, streams: map[string]*stream{}, pins: map[string]int{}, readers: map[string]int{}, verified: map[string]bool{},
		ctx: ctx, cancel: cancel, now: time.Now, cleanupCursors: map[string]cleanupCursor{}}
	for _, option := range options {
		option(s)
	}
	s.log = logging.For(s.log, "http_cache")
	if err := s.recover(); err != nil {
		cancel()
		return nil, err
	}
	return s, nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
	return nil
}

func fence(entry application.Entry) store.SourceFence {
	return store.SourceFence{AppRuntimeRevision: entry.RuntimeRevision, VendorRuntimeRevision: entry.VendorRuntimeRevision}
}
func (s *Service) begin(entry application.Entry) error {
	if entry.Provider != application.HttpCache || entry.Upstream == nil || !entry.Active() {
		return store.ErrSourceInactive
	}
	// Every publication re-checks the fence in its own transaction, so this
	// early check needs no lock.
	if err := s.db.CheckSourceActive(entry.StorageID(), fence(entry)); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	s.wg.Add(1)
	return nil
}

func (s *Service) listRows(storageID string) ([]Row, error) {
	entries, err := s.db.HTTPCacheEntries(storageID)
	if err != nil {
		return nil, err
	}
	return rowsFromEntries(entries)
}

func (s *Service) bodyPath(id string) string { return filepath.Join(s.dir, id+".body") }
func (s *Service) partPath(id string) string { return filepath.Join(s.dir, id+".part") }

// lookup returns the pinned current entry of path, or nil. An entry recovered
// from a previous run is hashed once before its first use; callers wait for
// that shared check without holding mu.
func (s *Service) lookup(ctx context.Context, storageID, path string) (*Row, error) {
	for {
		row, check, err := s.lookupOnce(storageID, path)
		if check == nil {
			return row, err
		}
		if err = check.Wait(ctx); ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, err
		}
	}
}

func (s *Service) lookupOnce(storageID, path string) (*Row, *spool.Check, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.db.CurrentHTTPCacheEntry(storageID, path)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	row, err := rowFromEntry(e)
	if err != nil {
		return nil, nil, err
	}
	size, statErr := s.bodies().Size(row.GenerationID)
	if os.IsNotExist(statErr) || (statErr == nil && size != row.SizeBytes) {
		if err = s.db.RetireHTTPCacheEntry(row.GenerationID, s.now()); err != nil {
			return nil, nil, err
		}
		return nil, nil, s.collectLocked(row.GenerationID)
	}
	if statErr != nil {
		return nil, nil, statErr
	}
	if !s.verified[row.GenerationID] {
		check, err := s.verifyLocked(row)
		return nil, check, err
	}
	s.pins[row.GenerationID]++
	return row, nil, nil
}

// verifyLocked starts, or joins, the whole-body check of an entry this process
// did not write. A body that is missing or does not match its observed hash
// is retired; the check runs under the service context, so a waiter that
// gives up does not cancel it for the others.
func (s *Service) verifyLocked(row *Row) (*spool.Check, error) {
	id := row.GenerationID
	if check := s.checks.Pending(id); check != nil {
		return check, nil
	}
	if s.closed {
		return nil, ErrClosed
	}
	check := s.checks.Start(id)
	path, size, digest := s.bodyPath(id), row.SizeBytes, row.SHA256
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		result, err := spool.CheckFile(s.ctx, path, size, digest)
		s.mu.Lock()
		defer s.mu.Unlock()
		if err == nil {
			if st, statErr := os.Lstat(path); result != nil && result.Valid && statErr == nil && result.Matches(st) {
				s.verified[id] = true
			} else if err = s.db.RetireHTTPCacheEntry(id, s.now()); err == nil {
				err = s.collectLocked(id)
			}
		}
		s.checks.Finish(id, check, err)
	}()
	return check, nil
}

func (s *Service) pin(id string) (*Row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.db.HTTPCacheEntry(id)
	if err != nil {
		return nil, err
	}
	r, err := rowFromEntry(e)
	if err != nil {
		return nil, err
	}
	s.pins[id]++
	return r, nil
}
func (s *Service) unpin(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pins[id]--
	if s.pins[id] <= 0 {
		delete(s.pins, id)
		_ = s.collectLocked(id)
	}
}

// touch records an access bucket of a pinned entry. The store keeps the
// latest bucket, so concurrent touches need no lock around the write.
func (s *Service) touch(r *Row) error {
	bucket := s.now().Unix() / 60 * 60
	s.mu.Lock()
	seen := r.accessBucket
	s.mu.Unlock()
	if bucket <= seen {
		return nil
	}
	if err := s.db.TouchHTTPCacheEntry(r.GenerationID, bucket); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return errors.New("HTTP cache generation disappeared")
		}
		return err
	}
	s.mu.Lock()
	r.accessBucket = max(r.accessBucket, bucket)
	s.mu.Unlock()
	return nil
}

// collectLocked deletes a retired entry's body and row once nothing pins it.
func (s *Service) collectLocked(id string) error {
	if s.pins[id] > 0 {
		return nil
	}
	e, err := s.db.HTTPCacheEntry(id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil || e.Current {
		return err
	}
	removed, err := s.bodies().Delete(id)
	if err != nil {
		return err
	}
	if _, err = s.db.DeleteRetiredHTTPCacheEntry(id); err != nil {
		return err
	}
	delete(s.verified, id)
	if removed && e.SizeBytes > 0 {
		return s.db.AddFor(metricScope(e.StorageID), "cleanup_freed_bytes", e.SizeBytes)
	}
	return nil
}

func metricScope(storageID string) string {
	if uid, _, ok := identity.ParseStorageID(storageID); ok {
		return identity.MetricsID(uid)
	}
	return storageID
}

func (s *Service) retire(r *Row) error {
	if r == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.db.RetireHTTPCacheEntry(r.GenerationID, s.now())
	if err == nil {
		err = s.collectLocked(r.GenerationID)
	}
	return err
}

// recover removes what a previous process left unfinished: retired entries,
// part files of interrupted fills and bodies without an entry. Current bodies
// are not hashed here; lookup verifies each before its first use.
func (s *Service) recover() error {
	entries, err := s.db.AllHTTPCacheEntries()
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, e := range entries {
		if !bodyID.MatchString(e.ID) {
			return errors.New("invalid HTTP cache file identity")
		}
		if e.Current {
			keep[e.ID+".body"] = true
			continue
		}
		if err = s.collectLocked(e.ID); err != nil {
			return err
		}
	}
	files, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		name := f.Name()
		id, suffix, _ := strings.Cut(name, ".")
		if !bodyID.MatchString(id) || suffix != "body" && suffix != "part" && suffix != "tmp" || keep[name] {
			continue
		}
		if f.Type()&os.ModeSymlink != 0 || f.IsDir() {
			return errors.New("unexpected nonregular HTTP cache file")
		}
		if _, err = fsutil.Remove(filepath.Join(s.dir, name)); err != nil {
			return err
		}
	}
	return s.recoverPreviews(context.Background())
}

// ErrCacheMiss reports a Cache-Control: only-if-cached request for a file that
// has no fresh cached copy. Nothing was contacted or written.
var ErrCacheMiss = errors.New("only-if-cached request for an uncached file")

// UpstreamStatusError reports an upstream response that is neither a file nor
// a retryable failure (for example 404 or 403). Nothing was written.
type UpstreamStatusError struct{ Status int }

func (e *UpstreamStatusError) Error() string {
	return "upstream returned HTTP " + strconv.Itoa(e.Status)
}

// NotFound reports whether the upstream said the file does not exist.
func (e *UpstreamStatusError) NotFound() bool {
	return e.Status == http.StatusNotFound || e.Status == http.StatusGone
}
