package httpcache

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/PMExtra/RedApp/internal/application"
)

// ErrFetchAgain asks an admitted caller to look up its resource again. A
// canceled leader or an unshareable response cannot supply that caller's body.
var ErrFetchAgain = errors.New("HTTP cache fetch must be retried with current storage state")

// sharedFetch is the common upstream operation for public requests and manual
// refresh. It bypasses freshness; callers decide whether validation is needed.
// The caller retains its old pin. A returned row has a separate pin, and a
// returned direct response owns its body/writer lease until Body.Close.
func (s *Service) sharedFetch(ctx context.Context, entry application.Entry, path string, old *Row) (fetchResult, error) {
	generation := ""
	if old != nil {
		generation = old.GenerationID
	}
	// Readers already holding a generation may finish together after cleanup.
	// A later cold reader owns no such pin and must not inherit that flight's
	// retired fallback. The same boundary keeps refresh validation tied to the
	// generation selected by its frozen preview.
	key := entry.StorageID() + "\x00" + path + "\x00" + strconv.FormatInt(entry.Revision, 10) + "/" + strconv.FormatInt(entry.VendorRevision, 10) + "\x00" + generation
	s.mu.Lock()
	current := s.flights[key]
	if current == nil {
		// A cold lookup may predate a flight that has already published and
		// exited. Check while holding the flight lock so that caller retries
		// freshness/policy against current storage instead of fetching twice.
		if old == nil {
			var exists bool
			err := s.db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM http_cache_generations WHERE storage_id=? AND path=? AND is_current=1)`, entry.StorageID(), path).Scan(&exists)
			if err != nil || exists {
				s.mu.Unlock()
				if err != nil {
					return fetchResult{}, err
				}
				return fetchResult{}, ErrFetchAgain
			}
		}
		current = &flight{done: make(chan struct{})}
		s.flights[key] = current
		s.mu.Unlock()
		var result fetchResult
		err := s.db.AddFor(entry.MetricsID(), "miss_requests", 1)
		if err == nil {
			result, err = s.fetch(ctx, entry, path, old, true)
		}
		s.mu.Lock()
		if err == nil && result.row != nil && result.temporary == "" {
			current.rowID = result.row.GenerationID
			current.stale = result.stale
		}
		current.err = err
		current.retry = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		delete(s.flights, key)
		close(current.done)
		s.mu.Unlock()
		return result, err
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return fetchResult{}, ctx.Err()
	case <-current.done:
	}
	if current.retry {
		return fetchResult{}, ErrFetchAgain
	}
	if current.err != nil {
		return fetchResult{}, current.err
	}
	if current.rowID == "" {
		return fetchResult{}, ErrFetchAgain
	}
	if err := s.db.AddFor(entry.MetricsID(), "shared_follower_requests", 1); err != nil {
		return fetchResult{}, err
	}
	row, err := s.pin(current.rowID)
	if errors.Is(err, sql.ErrNoRows) {
		return fetchResult{}, ErrFetchAgain
	}
	if err != nil {
		return fetchResult{}, err
	}
	// A cold leader can publish and be cleaned before completing its flight.
	// A follower with no preexisting pin must not inherit that retired result;
	// callers already holding old may still finish their admitted read.
	if old == nil && !row.current {
		s.unpin(row.GenerationID)
		return fetchResult{}, ErrFetchAgain
	}
	return fetchResult{row: row, stale: current.stale, status: 200}, nil
}
