package httpcache

import (
	"context"
	"database/sql"
	"errors"
	"github.com/PMExtra/RedApp/internal/application"
	"io"
	"strconv"
	"sync"
)

var ErrFetchAgain = errors.New("HTTP cache fetch must be retried with current storage state")

// ErrFetchContended reports that storage kept changing under one caller for
// fetchAgainLimit consecutive attempts.
var ErrFetchContended = errors.New("HTTP cache storage kept changing; retry the request")

// fetchAgainLimit bounds every ErrFetchAgain retry loop. It is a safety net
// for generation churn; uncacheable flights never consume it.
const fetchAgainLimit = 16

// errUncacheableFlight tells a follower that the shared flight produced a
// direct response already claimed by another reader. A reader that needs the
// body makes its own unshared transfer under the writer limit.
var errUncacheableFlight = errors.New("HTTP cache shared fetch produced an uncacheable response")

func flightKey(entry application.Entry, path, generation string) string {
	return entry.StorageID() + "\x00" + path + "\x00" + strconv.FormatInt(entry.RuntimeRevision, 10) + "/" + strconv.FormatInt(entry.VendorRuntimeRevision, 10) + "\x00" + generation
}

// Flights own the upstream context. A caller relinquishes only its waiter;
// cancellation closes the upstream when no admitted waiter remains.
func (s *Service) sharedFetch(ctx context.Context, f fill, old *Row) (fetchResult, error) {
	entry, path := f.entry, f.path
	generation := ""
	if old != nil {
		generation = old.GenerationID
	}
	key := flightKey(entry, path, generation)
	waiter := &fetchWaiter{failed: make(chan struct{}), observe: f.observe, check: f.check}
	s.mu.Lock()
	current := s.flights[key]
	leader := current == nil
	if leader {
		if s.closed {
			s.mu.Unlock()
			return fetchResult{}, ErrClosed
		}
		if old == nil {
			var exists bool
			err := s.db.HTTPCacheDB().QueryRow(`SELECT EXISTS(SELECT 1 FROM http_cache_generations WHERE storage_id=? AND path=? AND is_current=1)`, entry.StorageID(), path).Scan(&exists)
			if err != nil || exists {
				s.mu.Unlock()
				if err != nil {
					return fetchResult{}, err
				}
				return fetchResult{}, ErrFetchAgain
			}
		}
		workCtx, finish, err := s.db.ApplicationWork(context.WithoutCancel(ctx), entry.StorageID())
		if err != nil {
			s.mu.Unlock()
			return fetchResult{}, err
		}
		workCtx, cancel := context.WithCancel(workCtx)
		stop := context.AfterFunc(s.ctx, cancel)
		current = &flight{done: make(chan struct{}), waiters: map[*fetchWaiter]bool{}, cancel: cancel}
		s.flights[key] = current
		s.wg.Add(1)
		// Retain old independently of the caller whose cancellation may release its pin.
		if old != nil {
			s.pins[old.GenerationID]++
		}
		// The flight runs with the leader's policy, mode and source order. Budget
		// callbacks stay on each waiter, so one exhausted budget only ends that wait.
		shared := fill{entry: entry, path: path, policy: f.policy, warm: f.warm, attempts: f.attempts}
		go s.runFetch(workCtx, shared, old, key, current, func() { stop(); cancel(); finish() })
	}
	current.waiters[waiter] = true
	s.mu.Unlock()
	defer s.leaveFetch(current, waiter)
	select {
	case <-ctx.Done():
		return fetchResult{}, ctx.Err()
	case <-waiter.failed:
		return fetchResult{}, waiter.err
	case <-current.done:
	}
	s.mu.Lock()
	if waiter.err != nil {
		err := waiter.err
		s.mu.Unlock()
		return fetchResult{}, err
	}
	result := current.result
	err := current.err
	retry := current.retry
	if result.response != nil {
		if current.claimed {
			s.mu.Unlock()
			return fetchResult{blockReason: result.blockReason}, errUncacheableFlight
		}
		current.claimed = true
		s.mu.Unlock()
		return result, err
	}
	s.mu.Unlock()
	if retry {
		return fetchResult{}, ErrFetchAgain
	}
	if err != nil {
		return fetchResult{}, err
	}
	if result.row == nil {
		return result, nil
	}
	row, err := s.pin(result.row.GenerationID)
	if errors.Is(err, sql.ErrNoRows) {
		return fetchResult{}, ErrFetchAgain
	}
	if err != nil {
		return fetchResult{}, err
	}
	if !leader && old == nil && !row.current {
		s.unpin(row.GenerationID)
		return fetchResult{}, ErrFetchAgain
	}
	if !leader && !f.warm {
		if err = s.db.AddFor(entry.MetricsID(), "shared_follower_requests", 1); err != nil {
			s.unpin(row.GenerationID)
			return fetchResult{}, err
		}
	}
	result.row = row
	return result, nil
}
func (s *Service) runFetch(ctx context.Context, fl fill, old *Row, key string, f *flight, finish func()) {
	defer s.wg.Done()
	if old != nil {
		defer s.unpin(old.GenerationID)
	}
	result := fetchResult{}
	var err error
	if !fl.warm {
		err = s.db.AddFor(fl.entry.MetricsID(), "miss_requests", 1)
	}
	if err == nil {
		result, err = s.fetch(ctx, fl, old, true, f)
	}
	if result.response != nil {
		result.response.Body = &finishBody{ReadCloser: result.response.Body, finish: finish}
	} else {
		finish()
	}
	s.mu.Lock()
	f.result = result
	f.err = err
	f.retry = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	f.finished = true
	delete(s.flights, key)
	close(f.done)
	unused := len(f.waiters) == 0
	s.mu.Unlock()
	if unused {
		s.releaseFetchResult(f)
	}
}
func (s *Service) leaveFetch(f *flight, w *fetchWaiter) {
	s.mu.Lock()
	delete(f.waiters, w)
	empty := len(f.waiters) == 0
	finished := f.finished
	s.mu.Unlock()
	if empty {
		if !finished {
			f.cancel()
			<-f.done
		}
		s.releaseFetchResult(f)
	}
}
func (s *Service) releaseFetchResult(f *flight) {
	f.releaseOnce.Do(func() {
		if f.result.row != nil {
			s.unpin(f.result.row.GenerationID)
		}
		if f.result.response != nil && !f.claimed {
			f.result.response.Body.Close()
		}
	})
}

func (s *Service) observeFetch(f *flight, n int64) {
	if f == nil {
		return
	}
	s.mu.Lock()
	remaining := 0
	for w := range f.waiters {
		if w.err == nil && w.observe != nil {
			if err := w.observe(n); err != nil {
				w.err = err
				close(w.failed)
			}
		}
		if w.err == nil {
			remaining++
		}
	}
	s.mu.Unlock()
	if remaining == 0 {
		f.cancel()
	}
}

type finishBody struct {
	io.ReadCloser
	once   sync.Once
	finish func()
}

func (b *finishBody) Close() error { err := b.ReadCloser.Close(); b.once.Do(b.finish); return err }

func (s *Service) checkFetchLength(f *flight, n int64) {
	if f == nil {
		return
	}
	s.mu.Lock()
	remaining := 0
	for w := range f.waiters {
		if w.err == nil && w.check != nil {
			if err := w.check(n); err != nil {
				w.err = err
				close(w.failed)
			}
		}
		if w.err == nil {
			remaining++
		}
	}
	s.mu.Unlock()
	if remaining == 0 {
		f.cancel()
	}
}
