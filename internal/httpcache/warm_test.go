package httpcache

import (
	"context"
	"fmt"
	"github.com/PMExtra/RedApp/internal/warmplan"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWarmValidatorsAndNoAccessTouch(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		head         int
		tag          bool
		expired      bool
	}{
		{"304", "not_modified", 304, true, false}, {"same", "not_modified", 200, true, false}, {"changed", "downloaded", 200, true, false}, {"405", "not_modified", 405, true, false}, {"501", "not_modified", 501, true, false}, {"unknown-fresh", "ttl_fallback", 200, false, false}, {"unknown-expired", "downloaded", 200, false, true}, {"failure", "stale_fallback", 503, true, false}, {"gone", "failed", 410, true, false}, {"no-store", "not_cacheable", 200, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var get atomic.Int64
			var warm atomic.Bool
			f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.tag {
					w.Header().Set("ETag", `"old"`)
				}
				w.Header().Set("Content-Length", "4")
				if warm.Load() && r.Method == "HEAD" {
					if tc.tag && r.Header.Get("If-None-Match") != `"old"` {
						t.Error("HEAD missing stored validator")
					}
					if tc.name == "changed" {
						w.Header().Set("ETag", `"new"`)
					}
					if tc.name == "no-store" {
						w.Header().Set("Cache-Control", "no-store")
					}
					w.WriteHeader(tc.head)
					return
				}
				get.Add(1)
				if warm.Load() && tc.tag && tc.name != "changed" {
					w.WriteHeader(304)
					return
				}
				if tc.name == "changed" && warm.Load() {
					w.Header().Set("ETag", `"new"`)
				}
				fmt.Fprint(w, "body")
			}), 300)
			budget := &warmplan.Budget{Max: 1000}
			first := f.s.Warm(context.Background(), f.entry, "/file", budget)
			if first.Status != "downloaded" || first.Bytes != 4 {
				t.Fatalf("cold %+v budget %d", first, budget.Used())
			}
			before := f.rows(t)[0]
			if before.LastAccessAt != nil {
				t.Fatal("prewarm touched popularity")
			}
			warm.Store(true)
			f.clock.Add(1)
			if tc.expired {
				f.clock.Add(400)
			}
			second := f.s.Warm(context.Background(), f.entry, "/file", &warmplan.Budget{Max: 1000})
			if second.Status != tc.status {
				t.Fatalf("result %+v expected %s", second, tc.status)
			}
			if tc.name == "changed" && get.Load() != 2 {
				t.Fatal("changed ETag did not force GET within fresh TTL")
			}
			if rows := f.rows(t); len(rows) > 0 {
				if rows[0].LastAccessAt != nil {
					t.Fatal("warm touched popularity")
				}
				if tc.name == "unknown-fresh" && !rows[0].ValidatedAt.Equal(before.ValidatedAt) {
					t.Fatal("TTL fallback refreshed validation")
				}
			}
		})
	}
}
func TestDiscoverOfficialFormatsAndEscapes(t *testing.T) {
	for _, format := range []string{"html", "nginx", "caddy"} {
		t.Run(format, func(t *testing.T) {
			f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/files/sub/" {
					fmt.Fprint(w, `<a href="b">b</a><a href="../">parent</a>`)
					return
				}
				switch format {
				case "html":
					fmt.Fprint(w, `<a href="a">a</a><a href="sub/">sub</a><a href="?C=N">sort</a><a href="../escape">parent</a><a href="%252e%252e/escape">double</a><a href="%2fescape">encoded slash</a><a href="https://elsewhere/">origin</a><a href="x#f">fragment</a><a href="a">duplicate</a>`)
				case "nginx":
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `[{"name":"a","type":"file","mtime":"Wed, 01 Jan 2025 00:00:00 GMT","size":4},{"name":"sub","type":"directory"},{"name":"../escape","type":"file"}]`)
				case "caddy":
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `[{"name":"a","url":"./a","size":4,"mod_time":"2025-01-01T00:00:00Z","mode":420,"is_dir":false,"is_symlink":false},{"name":"sub/","url":"./sub/","is_dir":true},{"name":"escape","url":"./%2fescape","is_dir":false}]`)
				}
			}), 300)
			var paths []string
			ignored := 0
			err := f.s.Discover(context.Background(), f.entry, []string{"/"}, warmplan.DefaultLimits(), &warmplan.Budget{Max: 10000}, func(p string) error { paths = append(paths, p); return nil }, func(string) { ignored++ })
			if err != nil || strings.Join(paths, ",") != "/a,/sub/b" || ignored == 0 {
				t.Fatalf("paths %v ignored %d err %v", paths, ignored, err)
			}
			limits := warmplan.DefaultLimits()
			limits.MaxDepth = 0
			err = f.s.Discover(context.Background(), f.entry, []string{"/"}, limits, &warmplan.Budget{Max: 10000}, func(string) error { return nil }, func(string) {})
			if err != warmplan.ErrLimited {
				t.Fatalf("depth limit %v", err)
			}
		})
	}
}

func TestWarmCancellationPreservesPublicSharedFlight(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4")
		fmt.Fprint(w, "bo")
		w.(http.Flusher).Flush()
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
			return
		case <-release:
			fmt.Fprint(w, "dy")
		}
	}), 300)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	warmDone := make(chan warmplan.Item, 1)
	go func() { warmDone <- f.s.Warm(ctx, f.entry, "/file", &warmplan.Budget{Max: 100}) }()
	<-started
	publicDone := make(chan string, 1)
	go func() {
		w := httptest.NewRecorder()
		err := f.s.Serve(w, httptest.NewRequest("GET", "http://redapp/file", nil), f.entry, "file")
		if err != nil {
			publicDone <- err.Error()
			return
		}
		publicDone <- w.Body.String()
	}()
	deadline := time.Now().Add(2 * time.Second)
	joined := false
	for time.Now().Before(deadline) {
		f.s.mu.Lock()
		for _, flight := range f.s.flights {
			joined = len(flight.waiters) >= 2
		}
		f.s.mu.Unlock()
		if joined {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !joined {
		close(release)
		t.Fatal("public request did not share warm flight")
	}
	cancel()
	<-warmDone
	close(release)
	if body := <-publicDone; body != "body" {
		t.Fatal("warm cancellation cancelled public fetch", body)
	}
}

func TestWarmReadLimitPreservesPublicSharedFlight(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		fmt.Fprint(w, "bo")
		w.(http.Flusher).Flush()
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
			return
		case <-release:
			fmt.Fprint(w, "dy")
		}
	}), 300)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	warmDone := make(chan warmplan.Item, 1)
	go func() { warmDone <- f.s.Warm(ctx, f.entry, "/file", &warmplan.Budget{Max: 3}) }()
	<-started
	publicDone := make(chan string, 1)
	go func() {
		w := httptest.NewRecorder()
		err := f.s.Serve(w, httptest.NewRequest("GET", "http://redapp/file", nil), f.entry, "file")
		if err != nil {
			publicDone <- err.Error()
			return
		}
		publicDone <- w.Body.String()
	}()
	deadline := time.Now().Add(2 * time.Second)
	joined := false
	for time.Now().Before(deadline) {
		f.s.mu.Lock()
		for _, flight := range f.s.flights {
			joined = len(flight.waiters) >= 2
		}
		f.s.mu.Unlock()
		if joined {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !joined {
		close(release)
		t.Fatal("public request did not share warm flight")
	}
	close(release)
	item := <-warmDone
	if item.Reason != "read_limit" {
		t.Fatal(item)
	}
	if body := <-publicDone; body != "body" {
		t.Fatal("warm cancellation cancelled public fetch", body)
	}
}

func TestWarmSelectedMirrorGetAndPerSourceValidators(t *testing.T) {
	var warm atomic.Bool
	var second atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !warm.Load() {
			w.Header().Set("ETag", `"first"`)
			fmt.Fprint(w, "first")
			return
		}
		if r.Method == "HEAD" {
			w.WriteHeader(503)
			return
		}
		t.Error("GET returned to failed initial mirror")
		w.WriteHeader(503)
	}), 300)
	additionalSource(t, f, "ordered", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			t.Error("validators crossed source")
		}
		w.Header().Set("ETag", `"second"`)
		w.Header().Set("Content-Length", "6")
		if r.Method == "HEAD" {
			return
		}
		second.Add(1)
		fmt.Fprint(w, "second")
	}))
	if item := f.s.Warm(context.Background(), f.entry, "/file", &warmplan.Budget{Max: 100}); item.Status != "downloaded" {
		t.Fatal(item)
	}
	warm.Store(true)
	f.clock.Add(400)
	if item := f.s.Warm(context.Background(), f.entry, "/file", &warmplan.Budget{Max: 100}); item.Status != "downloaded" || second.Load() != 1 {
		t.Fatal(item, second.Load())
	}
}
func TestIndexRedirectBoundaryAndBodyLimit(t *testing.T) {
	for _, mode := range []string{"redirect", "oversize", "unknown-length"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					http.Redirect(w, r, "/outside/", 302)
				case "oversize":
					w.Header().Set("Content-Length", "2097153")
					fmt.Fprint(w, "x")
				case "unknown-length":
					w.(http.Flusher).Flush()
					fmt.Fprint(w, strings.Repeat("x", 50))
				}
			}), 300)
			budget := &warmplan.Budget{Max: 10}
			err := f.s.Discover(context.Background(), f.entry, []string{"/"}, warmplan.DefaultLimits(), budget, func(string) error { t.Fatal("unsafe listing emitted files"); return nil }, func(string) {})
			if err == nil {
				t.Fatal("unsafe/limited directory accepted")
			}
			if mode != "redirect" && err != warmplan.ErrLimited {
				t.Fatal(err)
			}
			if budget.Used() > budget.Max {
				t.Fatal("task budget exceeded")
			}
		})
	}
}

func TestWarmTTLFallbackRechecksRetiredGeneration(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			once.Do(func() { close(entered) })
			<-release
			return
		}
		fmt.Fprint(w, "body")
	}), 300)
	if item := f.s.Warm(context.Background(), f.entry, "/file", &warmplan.Budget{Max: 100}); item.Status != "downloaded" {
		t.Fatal(item)
	}
	old := f.rows(t)[0].GenerationID
	done := make(chan warmplan.Item, 1)
	go func() { done <- f.s.Warm(context.Background(), f.entry, "/file", &warmplan.Budget{Max: 100}) }()
	<-entered
	f.clock.Add(1)
	preview, err := f.s.Preview(f.entry, "fetched_at", f.s.now())
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.s.Execute(f.entry, preview.ID)
	if err != nil || result.RetiredFiles != 1 {
		t.Fatal(result, err)
	}
	close(release)
	if item := <-done; item.Status != "downloaded" {
		t.Fatal("retired generation accepted as TTL fallback", item)
	}
	if rows := f.rows(t); len(rows) != 1 || rows[0].GenerationID == old {
		t.Fatal(rows)
	}
}

func TestWarmFreshDifferentMirrorIsUnknownNotChanged(t *testing.T) {
	var warm atomic.Bool
	var gets atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if warm.Load() {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("ETag", `"first"`)
		w.Header().Set("Last-Modified", "Wed, 01 Jan 2025 00:00:00 GMT")
		fmt.Fprint(w, "first")
	}), 300)
	additionalSource(t, f, "ordered", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			t.Error("cross-source validators sent")
		}
		w.Header().Set("ETag", `"other"`)
		w.Header().Set("Last-Modified", "Thu, 02 Jan 2025 00:00:00 GMT")
		w.Header().Set("Content-Length", "9")
		if r.Method != "HEAD" {
			gets.Add(1)
			fmt.Fprint(w, "different")
		}
	}))
	if item := f.s.Warm(context.Background(), f.entry, "/file", &warmplan.Budget{Max: 100}); item.Status != "downloaded" {
		t.Fatal(item)
	}
	before := f.rows(t)[0]
	warm.Store(true)
	f.clock.Add(1)
	if item := f.s.Warm(context.Background(), f.entry, "/file", &warmplan.Budget{Max: 100}); item.Status != "ttl_fallback" || gets.Load() != 0 {
		t.Fatal(item, gets.Load())
	}
	if after := f.rows(t)[0]; !after.ValidatedAt.Equal(before.ValidatedAt) || !after.FreshUntil.Equal(before.FreshUntil) {
		t.Fatal("unknown mirror extended TTL")
	}
}
func TestWarmSameSourceETagOverridesConflictingLastModified(t *testing.T) {
	var warm atomic.Bool
	var gets atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"same"`)
		w.Header().Set("Content-Length", "4")
		w.Header().Set("Last-Modified", "Wed, 01 Jan 2025 00:00:00 GMT")
		if warm.Load() {
			w.Header().Set("Last-Modified", "Thu, 02 Jan 2025 00:00:00 GMT")
		}
		if r.Method == "HEAD" {
			return
		}
		gets.Add(1)
		fmt.Fprint(w, "body")
	}), 300)
	f.s.Warm(context.Background(), f.entry, "/file", &warmplan.Budget{Max: 100})
	warm.Store(true)
	if item := f.s.Warm(context.Background(), f.entry, "/file", &warmplan.Budget{Max: 100}); item.Status != "not_modified" || gets.Load() != 1 {
		t.Fatal(item, gets.Load())
	}
}
