package httpcache

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"strconv"
	"sync"

	"github.com/PMExtra/RedApp/internal/application"
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

// flight is one shared upstream operation for a path, keyed by the admitted
// application snapshot and the entry it replaces. It resolves once the
// response headers decided the outcome; a streamed outcome keeps the flight
// joinable until the stream's fill ends.
type flight struct {
	releaseOnce sync.Once
	// waiters counts callers that joined before resolution and have not taken
	// their outcome. A streamed resolution reserves one reader slot for each.
	waiters  int
	cancel   context.CancelFunc
	result   fetchResult
	err      error
	retry    bool
	resolved bool
	claimed  bool
	done     chan struct{}
}

// sharedFetch obtains path once for all concurrent callers. Flights own the
// upstream context: a caller relinquishes only its own wait, and the upstream
// stops when no caller or reader remains. A streamed result holds a reader
// slot that the caller releases with leaveStream.
func (s *Service) sharedFetch(ctx context.Context, f fill, old *Row) (fetchResult, error) {
	entry, path := f.entry, f.path
	generation := ""
	if old != nil {
		generation = old.GenerationID
	}
	key := flightKey(entry, path, generation)
	s.mu.Lock()
	current := s.flights[key]
	leader := current == nil
	switch {
	case leader:
		if s.closed {
			s.mu.Unlock()
			return fetchResult{}, ErrClosed
		}
		if old == nil {
			_, err := s.db.CurrentHTTPCacheEntry(entry.StorageID(), path)
			if !errors.Is(err, sql.ErrNoRows) {
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
		current = &flight{done: make(chan struct{}), cancel: cancel}
		s.flights[key] = current
		s.wg.Add(1)
		// Retain old independently of the caller whose cancellation may release its pin.
		if old != nil {
			s.pins[old.GenerationID]++
		}
		// The flight runs with the leader's policy, mode and source order.
		run := &fetchRun{fill: fill{entry: entry, path: path, policy: f.policy, warm: f.warm, attempts: f.attempts}, shared: true, key: key, cancel: cancel, finish: func() { stop(); cancel(); finish() }}
		go s.runFetch(workCtx, run, old, current)
	case current.resolved && current.result.stream != nil:
		// Only a stream that is still being written keeps a resolved flight
		// joinable; the late caller becomes another reader.
		st := current.result.stream
		st.readers++
		s.mu.Unlock()
		if err := s.countFollower(f); err != nil {
			s.leaveStream(st)
			return fetchResult{}, err
		}
		return fetchResult{stream: st, status: current.result.status}, nil
	}
	current.waiters++
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		s.leaveFlight(current, false)
		return fetchResult{}, ctx.Err()
	case <-current.done:
	}
	result, err, retry := current.result, current.err, current.retry
	if result.stream != nil {
		// The resolution reserved this caller's reader slot.
		s.leaveFlight(current, true)
		if !leader {
			if err := s.countFollower(f); err != nil {
				s.leaveStream(result.stream)
				return fetchResult{}, err
			}
		}
		return fetchResult{stream: result.stream, status: result.status}, nil
	}
	defer s.leaveFlight(current, false)
	if result.response != nil {
		s.mu.Lock()
		claimed := current.claimed
		current.claimed = true
		s.mu.Unlock()
		if claimed {
			return fetchResult{blockReason: result.blockReason}, errUncacheableFlight
		}
		return result, err
	}
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
	if !leader {
		if err = s.countFollower(f); err != nil {
			s.unpin(row.GenerationID)
			return fetchResult{}, err
		}
	}
	result.row = row
	return result, nil
}

func (s *Service) countFollower(f fill) error {
	if f.warm {
		return nil
	}
	return s.db.AddFor(f.entry.MetricsID(), "shared_follower_requests", 1)
}

func (s *Service) runFetch(ctx context.Context, run *fetchRun, old *Row, f *flight) {
	defer s.wg.Done()
	if old != nil {
		defer s.unpin(old.GenerationID)
	}
	result := fetchResult{}
	var err error
	if !run.warm {
		err = s.db.AddFor(run.entry.MetricsID(), "miss_requests", 1)
	}
	if err == nil {
		result, err = s.fetch(ctx, run, old)
	}
	switch {
	case result.response != nil:
		result.response.Body = &finishBody{ReadCloser: result.response.Body, finish: run.finish}
	case result.stream == nil:
		run.finish()
	}
	s.mu.Lock()
	f.result = result
	f.err = err
	f.retry = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	f.resolved = true
	st := result.stream
	if st == nil || st.finished || st.published != "" {
		delete(s.flights, run.key)
	}
	var stop, closeFile bool
	if st != nil {
		// Hand one reader slot to each waiter, then release the flight's own.
		st.readers += f.waiters
		stop, closeFile = s.dropReaderLocked(st)
	}
	close(f.done)
	unused := f.waiters == 0
	s.mu.Unlock()
	if st != nil {
		st.drop(stop, closeFile)
	} else if unused {
		s.releaseFetchResult(f)
	}
}

// leaveFlight ends one caller's wait. took reports that the caller kept the
// reader slot a streamed resolution reserved for it. The last caller to leave
// an unresolved flight cancels its upstream operation.
func (s *Service) leaveFlight(f *flight, took bool) {
	s.mu.Lock()
	f.waiters--
	empty := f.waiters == 0
	resolved := f.resolved
	st := f.result.stream
	var stop, closeFile bool
	if resolved && st != nil && !took {
		stop, closeFile = s.dropReaderLocked(st)
	}
	s.mu.Unlock()
	if st != nil {
		st.drop(stop, closeFile)
		return
	}
	if !empty {
		return
	}
	if !resolved {
		f.cancel()
		<-f.done
		s.mu.Lock()
		empty = f.waiters == 0
		st = f.result.stream
		s.mu.Unlock()
		if st != nil || !empty {
			// A stream resolved concurrently; its cancelled fill cleans up itself.
			return
		}
	}
	s.releaseFetchResult(f)
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

type finishBody struct {
	io.ReadCloser
	once   sync.Once
	finish func()
}

func (b *finishBody) Close() error { err := b.ReadCloser.Close(); b.once.Do(b.finish); return err }
