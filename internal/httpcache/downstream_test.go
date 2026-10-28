package httpcache

import (
	"io"
	"net/http"
	"sync/atomic"
	"testing"
)

// A downstream shared cache, such as another RedApp instance, may keep a
// representation only for the freshness remaining here, so lifetimes never
// accumulate across chained caches.
func TestDownstreamCacheControlCarriesRemainingFreshness(t *testing.T) {
	var failing atomic.Bool
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		io.WriteString(w, "body")
	}), 300)
	step := func(name, method string, advance int64, want string) {
		t.Helper()
		f.clock.Add(advance)
		w, err := f.serve(t, method, http.Header{})
		if err != nil || w.Header().Get("Cache-Control") != want {
			t.Fatalf("%s: Cache-Control %q, want %q (%v)", name, w.Header().Get("Cache-Control"), want, err)
		}
	}
	step("uncached HEAD", http.MethodHead, 0, "max-age=300")
	step("stream", http.MethodGet, 0, "max-age=300")
	step("fresh hit", http.MethodGet, 100, "max-age=200")
	step("fresh HEAD", http.MethodHead, 0, "max-age=200")
	step("revalidated", http.MethodGet, 250, "max-age=300")
	failing.Store(true)
	step("stale fallback", http.MethodGet, 400, "max-age=0")
}

func TestDownstreamCacheControlKeepsUnstorableResponsesPrivate(t *testing.T) {
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		io.WriteString(w, "body")
	}), 300)
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		w, err := f.serve(t, method, http.Header{})
		if err != nil || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: Cache-Control %q (%v)", method, w.Header().Get("Cache-Control"), err)
		}
	}
}
