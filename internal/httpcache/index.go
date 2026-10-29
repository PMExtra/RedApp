package httpcache

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/store"
)

// maxIndexBody bounds one source directory listing, as for prewarm discovery.
const maxIndexBody = 2 << 20

// IndexEntry is one direct child of a source directory. Names are decoded
// path segments; Size and Modified are known only from JSON listings.
type IndexEntry struct {
	Name      string     `json:"name"`
	Directory bool       `json:"directory"`
	Size      *int64     `json:"size,omitempty"`
	Modified  *time.Time `json:"modified,omitempty"`
}

// Index is the listing of one directory of an application's source, either
// fetched now or the last successful listing when Stale.
type Index struct {
	Entries   []IndexEntry
	Truncated bool
	Stale     bool
	FetchedAt time.Time
}

// ErrIndexUnsupported reports a source that answered a directory request
// with something other than a listing RedApp can read.
var ErrIndexUnsupported = errors.New("source does not list this directory")

type indexFlight struct {
	done  chan struct{}
	index Index
	err   error
}

// Index lists dir ("" is the root, otherwise an application-relative path
// without slashes at either end) from the source, never from cached files:
// a partially cached directory is still listed in full. A listing younger
// than the application's cache TTL is reused. When every source fails, the
// last successful listing of the current source epoch is returned as stale.
func (s *Service) Index(ctx context.Context, entry application.Entry, dir string, limit int) (Index, error) {
	ctx, finish, err := s.db.ApplicationWork(ctx, entry.StorageID())
	if err != nil {
		return Index{}, err
	}
	defer finish()
	if err = s.begin(entry); err != nil {
		return Index{}, err
	}
	defer s.wg.Done()
	stored, storedErr := s.storedIndex(entry, dir)
	if storedErr != nil && !errors.Is(storedErr, store.ErrNotFound) {
		return Index{}, storedErr
	}
	ttl := time.Duration(entry.Descriptor.DefaultChannelTTLSeconds) * time.Second
	if storedErr == nil && s.now().Before(stored.FetchedAt.Add(ttl)) {
		return truncateIndex(stored, limit), nil
	}
	key := entry.StorageID() + "\x00" + dir
	s.mu.Lock()
	if s.indexFlights == nil {
		s.indexFlights = map[string]*indexFlight{}
	}
	flight, joined := s.indexFlights[key]
	if !joined {
		flight = &indexFlight{done: make(chan struct{})}
		s.indexFlights[key] = flight
		// begin already holds a count, so Close cannot be past wg.Wait.
		s.wg.Add(1)
		go s.runIndexFlight(flight, key, entry, dir)
	}
	s.mu.Unlock()
	select {
	case <-flight.done:
	case <-ctx.Done():
		return Index{}, ctx.Err()
	}
	index, err := flight.index, flight.err
	if err == nil {
		return truncateIndex(index, limit), nil
	}
	var status *UpstreamStatusError
	if storedErr == nil && !errors.Is(err, ErrIndexUnsupported) && !errors.As(err, &status) {
		stored.Stale = true
		return truncateIndex(stored, limit), nil
	}
	return Index{}, err
}

// runIndexFlight fetches for every waiter of a flight, including the one that
// started it, in its own goroutine. Its application work lease, derived from
// the service, is cancelled by application deletion and service shutdown, but
// not when a waiting client leaves.
func (s *Service) runIndexFlight(flight *indexFlight, key string, entry application.Entry, dir string) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.indexFlights, key)
		s.mu.Unlock()
		close(flight.done)
	}()
	ctx, finish, err := s.db.ApplicationWork(s.ctx, entry.StorageID())
	if err != nil {
		flight.err = err
		return
	}
	defer finish()
	flight.index, flight.err = s.fetchIndex(ctx, entry, dir)
}

func truncateIndex(index Index, limit int) Index {
	if len(index.Entries) > limit {
		index.Entries, index.Truncated = index.Entries[:limit], true
	}
	return index
}

func (s *Service) storedIndex(entry application.Entry, dir string) (Index, error) {
	row, err := s.db.HTTPCacheIndex(entry.UID, dir)
	if err != nil {
		return Index{}, err
	}
	if row.SourceEpoch != entry.SourceEpoch {
		return Index{}, store.ErrNotFound
	}
	index := Index{Truncated: row.Truncated, FetchedAt: row.FetchedAt}
	if err = json.Unmarshal(row.Entries, &index.Entries); err != nil {
		return Index{}, err
	}
	return index, nil
}

// fetchIndex reads the listing from the sources in strategy order, moving on
// only when a source fails before answering, and stores it.
func (s *Service) fetchIndex(ctx context.Context, entry application.Entry, dir string) (Index, error) {
	attempts, err := s.sourceAttempts(entry)
	if err != nil {
		return Index{}, err
	}
	var last error = ErrUpstream
	for _, attempt := range attempts {
		if ctx.Err() != nil {
			return Index{}, ctx.Err()
		}
		index, retry, err := s.fetchIndexFrom(ctx, entry, attempt.Client, dir)
		if err == nil {
			payload, encodeErr := json.Marshal(index.Entries)
			if encodeErr != nil {
				return Index{}, encodeErr
			}
			if err = s.db.SaveHTTPCacheIndex(entry.UID, dir, store.HTTPCacheIndex{SourceEpoch: entry.SourceEpoch, Entries: payload, Truncated: index.Truncated, FetchedAt: index.FetchedAt}); err != nil {
				return Index{}, err
			}
			return index, nil
		}
		if !retry {
			return Index{}, err
		}
		last = err
	}
	return Index{}, last
}

func (s *Service) fetchIndexFrom(ctx context.Context, entry application.Entry, client *distributor.Client, dir string) (index Index, retry bool, err error) {
	release, err := s.budget.AcquireHTTPWriter()
	if err != nil {
		return Index{}, false, err
	}
	defer release()
	resp, err := client.Index(ctx, dir)
	if err != nil {
		if e := s.upstreamFailure(entry, dir+"/", 0, err); e != nil {
			return Index{}, false, e
		}
		return Index{}, errors.Is(err, distributor.ErrConnection), err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 500:
		if e := s.upstreamFailure(entry, dir+"/", resp.StatusCode, nil); e != nil {
			return Index{}, false, e
		}
		return Index{}, true, ErrUpstream
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return Index{}, false, &UpstreamStatusError{Status: resp.StatusCode}
	case resp.StatusCode != http.StatusOK || resp.Uncompressed || resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity":
		return Index{}, false, ErrIndexUnsupported
	}
	body, err := io.ReadAll(io.LimitReader(&upstreamBody{ReadCloser: resp.Body, s: s, app: entry.MetricsID()}, maxIndexBody+1))
	if err != nil {
		return Index{}, true, err
	}
	if len(body) > maxIndexBody {
		return Index{}, false, ErrIndexUnsupported
	}
	links, err := parseIndex(body, resp.Header.Get("Content-Type"))
	if err != nil {
		return Index{}, false, ErrIndexUnsupported
	}
	parent := "/"
	if dir != "" {
		parent = "/" + dir + "/"
	}
	seen := map[IndexEntry]bool{}
	index = Index{Entries: []IndexEntry{}, FetchedAt: s.now().UTC()}
	for _, link := range links {
		// Only safe links to direct children of this directory are listed:
		// never parents, siblings, other hosts or paths outside the source.
		p, err := discoveredPath(client, resp.Request.URL, link)
		if err != nil || !strings.HasPrefix(p, parent) || p == parent {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(p, parent), "/")
		if name == "" || strings.Contains(name, "/") || path.Clean(name) != name {
			continue
		}
		key := IndexEntry{Name: name, Directory: link.Directory}
		if seen[key] {
			continue
		}
		seen[key] = true
		index.Entries = append(index.Entries, IndexEntry{Name: name, Directory: link.Directory, Size: link.Size, Modified: link.Modified})
	}
	sort.SliceStable(index.Entries, func(i, j int) bool {
		a, b := index.Entries[i], index.Entries[j]
		if a.Directory != b.Directory {
			return a.Directory
		}
		return a.Name < b.Name
	})
	return index, false, nil
}
