// Package httpcache stores mutable HTTP representations independently of release
// metadata. Observed content hashes protect storage, not publisher authenticity.
package httpcache

import (
	"container/list"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
)

var ErrClosed = errors.New("HTTP cache is shutting down")
var ErrUpstream = errors.New("HTTP upstream response unavailable")
var ErrInvalidCleanup = errors.New("Invalid HTTP cache cleanup request")

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

type fetchWaiter struct {
	observe func(int64) error
	check   func(int64) error
	failed  chan struct{}
	err     error
}
type flight struct {
	releaseOnce sync.Once
	waiters     map[*fetchWaiter]bool
	cancel      context.CancelFunc
	result      fetchResult
	finished    bool
	claimed     bool
	done        chan struct{}
	rowID       string
	err         error
	retry       bool
	stale       bool
}

type transfer struct {
	filePath  string
	id, path  string
	entry     application.Entry
	started   time.Time
	bytes     int64
	diskBytes int64
}

type Service struct {
	sourceCursors   map[string]*list.Element
	sourceOrder     list.List
	cleanupMu       sync.Mutex
	cleanupStatus   AutomaticCleanupStatus
	cleanupCursors  map[string]cleanupCursor
	refreshRunning  bool
	previewBuilders int
	readers         map[string]int
	transfers       map[string]*transfer
	dir             string
	db              *store.Store
	budget          download.Budget
	mu              sync.Mutex
	flights         map[string]*flight
	pins            map[string]int
	closed          bool
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	now             func() time.Time
}

// Option configures a Service at construction.
type Option func(*Service)

// WithClock replaces the wall clock used for freshness, access buckets and
// maintenance previews.
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

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
	s := &Service{dir: filepath.Join(dir, "objects", "http"), db: db, budget: budget, flights: map[string]*flight{}, transfers: map[string]*transfer{}, pins: map[string]int{}, readers: map[string]int{}, ctx: ctx, cancel: cancel, now: time.Now, cleanupCursors: map[string]cleanupCursor{}}
	for _, option := range options {
		option(s)
	}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if entry.Provider != application.HttpCache || entry.Upstream == nil || !entry.Active() {
		return store.ErrSourceInactive
	}
	if err := s.db.CheckSourceActive(entry.StorageID(), fence(entry)); err != nil {
		return err
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

func (s *Service) lookup(storageID, path string) (*Row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.db.CurrentHTTPCacheEntry(storageID, path)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row, err := rowFromEntry(e)
	if err != nil {
		return nil, err
	}
	size, statErr := s.bodies().Size(row.GenerationID)
	if os.IsNotExist(statErr) || (statErr == nil && size != row.SizeBytes) {
		if err = s.db.RetireHTTPCacheEntry(row.GenerationID, s.now()); err != nil {
			return nil, err
		}
		if err = s.collectLocked(row.GenerationID); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if statErr != nil {
		return nil, statErr
	}
	s.pins[row.GenerationID]++
	return row, nil
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
func (s *Service) touch(r *Row) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.now().Unix() / 60 * 60
	if bucket <= r.accessBucket {
		return nil
	}
	if err := s.db.TouchHTTPCacheEntry(r.GenerationID, bucket); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("HTTP cache generation disappeared")
		}
		return err
	}
	r.accessBucket = bucket
	return nil
}
func (s *Service) bodyPath(id string) string { return filepath.Join(s.dir, id+".body") }
func (s *Service) collectLocked(id string) error {
	if s.pins[id] > 0 {
		return nil
	}
	e, err := s.db.HTTPCacheEntry(id)
	if errors.Is(err, sql.ErrNoRows) {
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
	if removed && e.SizeBytes > 0 {
		metric := e.StorageID
		if uid, _, ok := identity.ParseStorageID(e.StorageID); ok {
			metric = identity.MetricsID(uid)
		}
		return s.db.AddFor(metric, "cleanup_freed_bytes", e.SizeBytes)
	}
	return nil
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
func (s *Service) recover() error {
	entries, err := s.db.AllHTTPCacheEntries()
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, e := range entries {
		if len(e.ID) != 32 || strings.Trim(e.ID, "0123456789abcdef") != "" {
			return errors.New("Invalid HTTP cache file identity")
		}
		if e.Current {
			f, err := fsutil.OpenRegular(s.bodyPath(e.ID))
			if err == nil {
				h := sha256.New()
				n, readErr := io.Copy(h, f)
				f.Close()
				if readErr == nil && n == e.SizeBytes && hex.EncodeToString(h.Sum(nil)) == e.SHA256 {
					keep[e.ID+".body"] = true
					continue
				}
			} else if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if err = s.db.RetireHTTPCacheEntry(e.ID, s.now()); err != nil {
				return err
			}
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
		id := strings.TrimSuffix(strings.TrimSuffix(name, ".body"), ".tmp")
		if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" || name != id+".body" && name != id+".tmp" {
			continue
		}
		if !keep[name] {
			if f.Type()&os.ModeSymlink != 0 || f.IsDir() {
				return errors.New("Unexpected nonregular HTTP cache file")
			}
			if _, err = fsutil.Remove(filepath.Join(s.dir, name)); err != nil {
				return err
			}
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
