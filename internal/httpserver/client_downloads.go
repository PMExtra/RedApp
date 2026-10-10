package httpserver

import (
	"net/http"
	"sync"

	"github.com/PMExtra/RedApp/internal/auth"
)

// clientDownloads bounds the concurrent file downloads of each client. A
// response holds its slot for as long as it streams, and every write may
// extend its deadline, so without a per-client bound one anonymous client
// could hold every shared reader slot (max_readers) indefinitely.
type clientDownloads struct {
	max    int
	mu     sync.Mutex
	active map[string]int // client key (auth.ClientKey) -> downloads in progress
}

func newClientDownloads(max int) *clientDownloads {
	return &clientDownloads{max: max, active: map[string]int{}}
}

// acquire reserves one download slot for key. It reports false when the
// client already has max downloads in progress; release returns the slot.
func (c *clientDownloads) acquire(key string) (release func(), ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active[key] >= c.max {
		return nil, false
	}
	c.active[key]++
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.active[key]--; c.active[key] == 0 {
				delete(c.active, key)
			}
		})
	}, true
}

// admitDownload reserves a download slot for the requesting client, or
// answers TRANSFER_CAPACITY and reports false.
func (s *Server) admitDownload(w http.ResponseWriter, r *http.Request) (release func(), ok bool) {
	release, ok = s.downloadSlots.acquire(auth.ClientKey(s.proxies.ClientIP(r)))
	if !ok {
		s.fail(w, r, codeTransferCapacity, nil, "Too many concurrent downloads from this client; retry later")
	}
	return release, ok
}
