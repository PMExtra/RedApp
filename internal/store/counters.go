package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"
)

// CounterFlushInterval bounds how long accepted counter increments stay in memory.
const CounterFlushInterval = time.Second

type counterEntry struct{ app, key string }
type versionEntry struct{ app, version string }
type versionDelta struct{ requests, downstreamBytes int64 }

// counterBuffer coalesces hot-path counter increments in memory. Keys are bounded
// by applications x counters and applications x versions, so increments retained
// after a failed flush never grow beyond one value per key.
type counterBuffer struct {
	mu       sync.Mutex
	counters map[counterEntry]int64
	versions map[versionEntry]versionDelta
	onError  func(error)
	flushMu  sync.Mutex // Serializes flushes so a returned flush covers all earlier adds.
}

func counterKey(name string) (string, error) {
	name = strings.TrimPrefix(name, "counters.")
	switch name {
	case "requests", "artifact_requests", "cache_hit_requests", "shared_follower_requests", "miss_requests", "download_success", "download_errors", "upstream_errors", "upstream_bytes", "downstream_bytes", "cleanup_freed_bytes":
		return "counters." + name, nil
	}
	return "", errors.New("Unknown active counter")
}
func increment(tx *sql.Tx, scope, app, key string, n int64, at int64) error {
	_, err := tx.Exec("INSERT INTO metric_counters(scope,app_id,metric,value,observed_since_s) VALUES(?,?,?,?,?) ON CONFLICT(scope,app_id,metric) DO UPDATE SET value=value+excluded.value", scope, app, key, n, at)
	return err
}

// add validates and buffers an increment; persistence happens in FlushCounters.
func (s *Store) add(app, name string, n int64) error {
	key, err := counterKey(name)
	if err != nil {
		return err
	}
	if n < 0 {
		return errors.New("Counter increments cannot be negative")
	}
	b := &s.pending
	b.mu.Lock()
	if b.counters == nil {
		b.counters = map[counterEntry]int64{}
	}
	b.counters[counterEntry{app, key}] += n
	b.mu.Unlock()
	s.sample(strings.TrimPrefix(key, "counters."), n)
	return nil
}
func (s *Store) Add(name string, n int64) error { return s.add("", name, n) }

// AddFor increments global and owning-app counters; both are persisted in the same flush transaction.
func (s *Store) AddFor(app, name string, n int64) error {
	if err := requireApp(app); err != nil {
		return err
	}
	return s.add(app, name, n)
}

// AddVersion buffers per-version request and downstream byte counters.
func (s *Store) AddVersion(app, version string, requests, downstreamBytes int64) error {
	if requireApp(app) != nil || requests < 0 || downstreamBytes < 0 {
		return errors.New("Invalid version counter")
	}
	b := &s.pending
	b.mu.Lock()
	if b.versions == nil {
		b.versions = map[versionEntry]versionDelta{}
	}
	d := b.versions[versionEntry{app, version}]
	d.requests += requests
	d.downstreamBytes += downstreamBytes
	b.versions[versionEntry{app, version}] = d
	b.mu.Unlock()
	return nil
}

// FlushCounters persists buffered increments in one transaction. On failure the
// increments are retained and retried by the next flush.
func (s *Store) FlushCounters() error {
	b := &s.pending
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	b.mu.Lock()
	counters, versions := b.counters, b.versions
	b.counters, b.versions = nil, nil
	b.mu.Unlock()
	if len(counters) == 0 && len(versions) == 0 {
		return nil
	}
	err := s.writeCounters(counters, versions)
	if err != nil {
		b.mu.Lock()
		if b.counters == nil {
			b.counters = map[counterEntry]int64{}
		}
		for k, n := range counters {
			b.counters[k] += n
		}
		if b.versions == nil {
			b.versions = map[versionEntry]versionDelta{}
		}
		for k, d := range versions {
			v := b.versions[k]
			v.requests += d.requests
			v.downstreamBytes += d.downstreamBytes
			b.versions[k] = v
		}
		b.mu.Unlock()
	}
	return err
}
func (s *Store) writeCounters(counters map[counterEntry]int64, versions map[versionEntry]versionDelta) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing := map[string]bool{}
	exists := func(app string) (bool, error) {
		if ok, seen := existing[app]; seen {
			return ok, nil
		}
		ok, e := applicationNamespaceExists(tx, app)
		if errors.Is(e, ErrInvalidDirectory) {
			ok, e = false, nil // A malformed owner can never be recorded; drop it rather than block other counters.
		}
		if e == nil {
			existing[app] = ok
		}
		return ok, e
	}
	at := time.Now().Unix()
	for entry, n := range counters {
		if entry.app != "" {
			ok, e := exists(entry.app)
			if e != nil {
				return e
			}
			if !ok {
				continue // Counters for deleted applications are discarded.
			}
		}
		if err = increment(tx, "global", "", entry.key, n, at); err != nil {
			return err
		}
		if entry.app != "" {
			if err = increment(tx, "app", entry.app, entry.key, n, at); err != nil {
				return err
			}
		}
	}
	for entry, d := range versions {
		// Unknown or deleted versions update no row and are discarded.
		if _, err = tx.Exec("UPDATE app_versions SET artifact_requests=artifact_requests+?,downstream_bytes=downstream_bytes+? WHERE app_id=? AND version=?", d.requests, d.downstreamBytes, entry.app, entry.version); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SettleCounters flushes buffered counters without failing the caller; errors go
// to the handler registered by StartCounterFlush and increments are retried later.
func (s *Store) SettleCounters() {
	if err := s.FlushCounters(); err != nil {
		s.pending.mu.Lock()
		onError := s.pending.onError
		s.pending.mu.Unlock()
		if onError != nil {
			onError(err)
		}
	}
}

// StartCounterFlush registers onError for every settle failure and flushes
// buffered counters every interval. The returned stop waits for the loop and
// performs a final flush.
func (s *Store) StartCounterFlush(interval time.Duration, onError func(error)) (stop func()) {
	s.pending.mu.Lock()
	s.pending.onError = onError
	s.pending.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				s.SettleCounters()
				return
			case <-tick.C:
				s.SettleCounters()
			}
		}
	}()
	return func() { cancel(); <-done }
}

// Close flushes buffered counters and closes the database.
func (s *Store) Close() error {
	s.SettleCounters()
	return s.closeDatabases()
}

// Counter reads settle buffered increments first so they observe every accepted add.
func (s *Store) counters(scope, app string) (map[string]int64, error) {
	s.SettleCounters()
	rows, err := s.read.Query("SELECT metric,value FROM metric_counters WHERE scope=? AND app_id=?", scope, app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var key string
		var value int64
		if err = rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[strings.TrimPrefix(key, "counters.")] = value
	}
	return out, rows.Err()
}
func (s *Store) Counters() (map[string]int64, error) { return s.counters("global", "") }
func (s *Store) CountersFor(app string) (map[string]int64, error) {
	if err := requireApp(app); err != nil {
		return nil, err
	}
	return s.counters("app", app)
}
