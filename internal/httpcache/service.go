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
	return store.SourceFence{AppRevision: entry.RuntimeRevision, VendorRevision: entry.VendorRuntimeRevision}
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

const columns = `id,storage_id,path,sha256,size_bytes,fetched_at_s,validated_at_s,last_access_bucket_s,fresh_until_s,headers_json,is_current,source_url`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (*Row, error) {
	var r Row
	var fetched, validated, fresh int64
	var headers []byte
	err := row.Scan(&r.GenerationID, &r.storageID, &r.Path, &r.SHA256, &r.SizeBytes, &fetched, &validated, &r.accessBucket, &fresh, &headers, &r.current, &r.SourceURL)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(headers, &r.headers); err != nil {
		return nil, err
	}
	r.FetchedAt = time.Unix(fetched, 0).UTC()
	r.ValidatedAt = time.Unix(validated, 0).UTC()
	r.FreshUntil = time.Unix(fresh, 0).UTC()
	if r.accessBucket > 0 {
		t := time.Unix(r.accessBucket+60, 0).UTC()
		r.LastAccessAt = &t
	}
	r.ETag = r.headers.Get("ETag")
	if r.ETag == "" {
		r.ETag = `"sha256-` + r.SHA256 + `"`
	}
	return &r, nil
}

func (s *Service) listRows(storageID string) ([]Row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.DB.Query(`SELECT `+columns+` FROM http_cache_generations WHERE storage_id=? AND is_current=1 ORDER BY path`, storageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Row{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *Service) lookup(storageID, path string) (*Row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := scan(s.db.DB.QueryRow(`SELECT `+columns+` FROM http_cache_generations WHERE storage_id=? AND path=? AND is_current=1`, storageID, path))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	size, statErr := s.bodies().Size(row.GenerationID)
	if os.IsNotExist(statErr) || (statErr == nil && size != row.SizeBytes) {
		if _, err = s.db.DB.Exec(`UPDATE http_cache_generations SET is_current=0,retired_at_s=? WHERE id=?`, s.now().Unix(), row.GenerationID); err != nil {
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
	r, err := scan(s.db.DB.QueryRow(`SELECT `+columns+` FROM http_cache_generations WHERE id=?`, id))
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
	result, err := s.db.DB.Exec(`UPDATE http_cache_generations SET last_access_bucket_s=MAX(last_access_bucket_s,?) WHERE id=?`, bucket, r.GenerationID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("HTTP cache generation disappeared")
	}
	r.accessBucket = bucket
	return nil
}
func (s *Service) bodyPath(id string) string { return filepath.Join(s.dir, id+".body") }
func (s *Service) collectLocked(id string) error {
	if s.pins[id] > 0 {
		return nil
	}
	var current bool
	var storageID string
	var bytes int64
	err := s.db.DB.QueryRow(`SELECT is_current,storage_id,size_bytes FROM http_cache_generations WHERE id=?`, id).Scan(&current, &storageID, &bytes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil || current {
		return err
	}
	removed, err := s.bodies().Delete(id)
	if err != nil {
		return err
	}
	_, err = s.db.DB.Exec(`DELETE FROM http_cache_generations WHERE id=? AND is_current=0`, id)
	if err == nil && removed && bytes > 0 {
		metric := storageID
		if uid, _, ok := identity.ParseStorageID(storageID); ok {
			metric = identity.MetricsID(uid)
		}
		err = s.db.AddFor(metric, "cleanup_freed_bytes", bytes)
	}
	return err
}
func (s *Service) retire(r *Row) error {
	if r == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.DB.Exec(`UPDATE http_cache_generations SET is_current=0,retired_at_s=COALESCE(retired_at_s,?) WHERE id=?`, s.now().Unix(), r.GenerationID)
	if err == nil {
		err = s.collectLocked(r.GenerationID)
	}
	return err
}
func (s *Service) recover() error {
	rows, err := s.db.DB.Query(`SELECT ` + columns + ` FROM http_cache_generations`)
	if err != nil {
		return err
	}
	all := []*Row{}
	for rows.Next() {
		r, e := scan(rows)
		if e != nil {
			rows.Close()
			return e
		}
		all = append(all, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, r := range all {
		if len(r.GenerationID) != 32 || strings.Trim(r.GenerationID, "0123456789abcdef") != "" {
			return errors.New("Invalid HTTP cache file identity")
		}
		if r.current {
			f, e := fsutil.OpenRegular(s.bodyPath(r.GenerationID))
			if e == nil {
				h := sha256.New()
				n, readErr := io.Copy(h, f)
				f.Close()
				if readErr == nil && n == r.SizeBytes && hex.EncodeToString(h.Sum(nil)) == r.SHA256 {
					keep[r.GenerationID+".body"] = true
					continue
				}
			} else if !errors.Is(e, fs.ErrNotExist) {
				return e
			}
			if _, err = s.db.DB.Exec(`UPDATE http_cache_generations SET is_current=0,retired_at_s=? WHERE id=?`, s.now().Unix(), r.GenerationID); err != nil {
				return err
			}
		}
		if err = s.collectLocked(r.GenerationID); err != nil {
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
