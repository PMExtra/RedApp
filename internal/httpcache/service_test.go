package httpcache

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/spool"
	"github.com/PMExtra/RedApp/internal/store"
)

type testBudget struct {
	readers, writers atomic.Int64
	limit            int64
	// readerAcquired, when set, is signalled for every reader lease.
	readerAcquired atomic.Pointer[chan struct{}]
}

func (b *testBudget) AcquireHTTPReader() (func(), error) {
	b.readers.Add(1)
	if acquired := b.readerAcquired.Load(); acquired != nil {
		select {
		case *acquired <- struct{}{}:
		default:
		}
	}
	var once sync.Once
	return func() { once.Do(func() { b.readers.Add(-1) }) }, nil
}
func (b *testBudget) AcquireHTTPWriter() (func(), error) {
	b.writers.Add(1)
	var once sync.Once
	return func() { once.Do(func() { b.writers.Add(-1) }) }, nil
}
func (b *testBudget) MaxArtifactBytes() int64 { return b.limit }

// fastRetry keeps the retry bounds of production with millisecond backoff.
var fastRetry = spool.Retry{Attempts: 6, Base: time.Millisecond, Max: 10 * time.Millisecond}

type fixture struct {
	dir    string
	s      *Service
	db     *store.Store
	entry  application.Entry
	app    store.Application
	vendor store.Vendor
	clock  atomic.Int64
	budget *testBudget
}

// allPaths matches every application-relative path.
var allPaths = pathmatch.Spec{Type: "glob", Pattern: "/"}

func testID(t *testing.T) string {
	t.Helper()
	id, err := fsutil.RandomID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newFixture(t *testing.T, h http.Handler, ttl int) *fixture {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	vendor, err := db.CreateVendor(store.VendorInput{ID: "vendor", Name: store.LocalizedText{En: "Vendor", ZhCN: "发布者"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	app, err := db.CreateApplication(vendor.ID, store.ApplicationInput{ID: "app", Name: store.LocalizedText{En: "App", ZhCN: "应用"}, Provider: application.HttpCache, BaseURL: server.URL + "/files", CacheTTLSeconds: ttl, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	client, _ := distributor.NewPool().NewClient(app.BaseURL, distributor.GeneralHTTP)
	f := &fixture{dir: dir, db: db, app: app, vendor: vendor, budget: &testBudget{limit: 1024}}
	f.entry = application.Entry{Descriptor: application.Descriptor{ID: app.Key, DefaultChannelTTLSeconds: ttl}, UID: app.UID, SourceEpoch: app.SourceEpoch, Revision: app.Revision, VendorRevision: vendor.Revision, RuntimeRevision: app.RuntimeRevision, VendorRuntimeRevision: vendor.RuntimeRevision, Provider: application.HttpCache, Enabled: true, Upstream: client}
	f.clock.Store(time.Now().Unix())
	f.s, err = New(dir, db, f.budget, WithClock(func() time.Time { return time.Unix(f.clock.Load(), 0).UTC() }), WithTransferPolicy(distributor.DefaultIdleTimeout, fastRetry))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.s.Close() })
	return f
}
func (f *fixture) serve(t *testing.T, method string, headers http.Header) (*httptest.ResponseRecorder, error) {
	t.Helper()
	r := httptest.NewRequest(method, "http://redapp.example/vendor/app/file", nil)
	r.Header = headers
	w := httptest.NewRecorder()
	err := f.s.Serve(w, r, f.entry, "file")
	return w, err
}

// serveAborting serves a GET whose response may be aborted after it started,
// as the server does when a streamed body fails.
func (f *fixture) serveAborting(t *testing.T, headers http.Header) (w *httptest.ResponseRecorder, err error, aborted bool) {
	t.Helper()
	defer func() {
		if caught := recover(); caught != nil {
			if caught != http.ErrAbortHandler {
				panic(caught)
			}
			aborted = true
		}
	}()
	w, err = f.serve(t, "GET", headers)
	return w, err, false
}

func (f *fixture) rows(t *testing.T) []Row {
	t.Helper()
	rows, err := f.s.listRows(f.entry.StorageID())
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// consume reads a shared fetch result to the end as a client would, waits
// for a streamed body to be published, and releases the result.
func (f *fixture) consume(result fetchResult) (string, error) {
	ctx := context.Background()
	var body []byte
	var err error
	switch {
	case result.stream != nil:
		defer f.s.leaveStream(result.stream)
		if body, err = io.ReadAll(result.stream.body.NewReader(ctx)); err == nil {
			_, err = f.s.awaitPublication(ctx, result.stream)
		}
	case result.row != nil:
		defer f.s.unpin(result.row.GenerationID)
		file, _, openErr := f.s.bodies().Open(result.row.GenerationID)
		if openErr != nil {
			return "", openErr
		}
		defer file.Close()
		body, err = io.ReadAll(file)
	case result.response != nil:
		defer result.response.Body.Close()
		body, err = io.ReadAll(result.response.Body)
	}
	return string(body), err
}

func TestCacheValidatorsHeadRangeAndStaleFailures(t *testing.T) {
	var calls atomic.Int64
	var mode atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch mode.Load() {
		case 1:
			if r.Header.Get("If-None-Match") != `"origin-v1"` {
				t.Error("stored upstream validator not used")
			}
			w.Header().Set("ETag", `"origin-v1"`)
			w.WriteHeader(304)
			return
		case 2:
			w.WriteHeader(503)
			return
		case 3:
			w.WriteHeader(404)
			return
		case 4:
			w.Header().Set("Content-Length", "100")
			io.WriteString(w, "short")
			return
		case 5:
			w.Header().Set("ETag", `"different"`)
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"origin-v1"`)
		w.Header().Set("Content-Length", "8")
		io.WriteString(w, "contents")
	}), 300)
	first, err := f.serve(t, "GET", http.Header{})
	if err != nil || first.Body.String() != "contents" {
		t.Fatal(first, err)
	}
	rows := f.rows(t)
	if len(rows) != 1 || rows[0].LastAccessAt == nil {
		t.Fatal(rows)
	}
	fetched := rows[0].FetchedAt
	for _, tc := range []struct {
		method  string
		headers http.Header
		status  int
		body    string
	}{
		{"HEAD", http.Header{}, 200, ""}, {"GET", http.Header{"If-None-Match": {`"origin-v1"`}}, 304, ""},
		{"GET", http.Header{"Range": {"bytes=2-4"}}, 206, "nte"}, {"GET", http.Header{"Range": {"bytes=2-4"}, "If-Range": {`"other"`}}, 200, "contents"},
		{"GET", http.Header{"Range": {"bytes=0-1,4-5"}}, 200, "contents"}, {"GET", http.Header{"Range": {"bytes=99-100"}}, 416, ""},
	} {
		w, err := f.serve(t, tc.method, tc.headers)
		if err != nil || w.Code != tc.status || (tc.body != "" && w.Body.String() != tc.body) {
			t.Fatalf("%s %#v: %d %q %v", tc.method, tc.headers, w.Code, w.Body.String(), err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("fresh cache unexpectedly used upstream", calls.Load())
	}
	// A current TTL reduction applies to already stored entries.
	f.entry.Descriptor.DefaultChannelTTLSeconds = 0
	mode.Store(1)
	f.clock.Add(400)
	if _, err = f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	rows = f.rows(t)
	if !rows[0].FetchedAt.Equal(fetched) || !rows[0].ValidatedAt.After(fetched) {
		t.Fatal("304 reset fetched time", rows)
	}
	mode.Store(5)
	if _, err = f.serve(t, "GET", http.Header{}); !errors.Is(err, ErrUpstream) {
		t.Fatal("mismatched 304 accepted", err)
	}
	mode.Store(2)
	if w, err := f.serve(t, "GET", http.Header{}); err != nil || w.Body.String() != "contents" {
		t.Fatal("stale fallback lost", w.Body.String(), err)
	}
	// A response that fails after it started streaming cannot fall back: its
	// client sees an aborted transfer and the stored body stays current.
	stored := f.rows(t)[0]
	mode.Store(4)
	if _, err, aborted := f.serveAborting(t, http.Header{}); !aborted && err == nil {
		t.Fatal("truncated response completed")
	}
	if rows := f.rows(t); len(rows) != 1 || rows[0].GenerationID != stored.GenerationID {
		t.Fatal("truncated response replaced the stored body", rows)
	}
	mode.Store(2)
	if w, err := f.serve(t, "GET", http.Header{}); err != nil || w.Body.String() != "contents" {
		t.Fatal("stale fallback lost after a truncated response", w.Body.String(), err)
	}
	mode.Store(3)
	_, err = f.serve(t, "GET", http.Header{})
	var status *UpstreamStatusError
	if !errors.As(err, &status) || !status.NotFound() || len(f.rows(t)) != 0 {
		t.Fatal("404 served stale", err)
	}
}

func TestCacheSingleflightAndUnsharedResponses(t *testing.T) {
	for _, policy := range []string{"cacheable", "no-store", "private", "cookie", "vary"} {
		t.Run(policy, func(t *testing.T) {
			var calls atomic.Int64
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				once.Do(func() { close(entered) })
				<-release
				switch policy {
				case "no-store", "private":
					w.Header().Set("Cache-Control", policy)
				case "cookie":
					w.Header().Add("Set-Cookie", "secret=neverforward")
				case "vary":
					w.Header().Set("Vary", "Authorization")
				}
				io.WriteString(w, "body")
			}), 300)
			var wg sync.WaitGroup
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					w, err := f.serve(t, "GET", http.Header{})
					if err != nil || w.Body.String() != "body" || w.Header().Get("Set-Cookie") != "" {
						t.Errorf("bad response: %v", err)
					}
				}()
			}
			<-entered
			close(release)
			wg.Wait()
			want := int64(4)
			if policy == "cacheable" {
				want = 1
			}
			if calls.Load() != want {
				t.Fatalf("upstream calls %d want %d", calls.Load(), want)
			}
			rows := f.rows(t)
			if policy == "cacheable" && len(rows) != 1 || policy != "cacheable" && len(rows) != 0 {
				t.Fatal(rows)
			}
			if policy != "cacheable" {
				files, _ := os.ReadDir(f.s.dir)
				if len(files) != 0 {
					t.Fatal("unshared response reached disk", files)
				}
			}
			if f.budget.readers.Load() != 0 || f.budget.writers.Load() != 0 {
				t.Fatal("capacity leaked")
			}
		})
	}
}

func TestHeadAndRequestCacheDirectives(t *testing.T) {
	var calls atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("ETag", `"tag"`)
		w.Header().Set("Content-Length", "4")
		if r.Method != "HEAD" {
			io.WriteString(w, "body")
		}
	}), 300)
	w, err := f.serve(t, "HEAD", http.Header{"If-None-Match": {`"tag"`}})
	if err != nil || w.Code != 304 || len(f.rows(t)) != 0 {
		t.Fatal("cold HEAD conditional or allocation", w.Code, err)
	}
	w, err = f.serve(t, "GET", http.Header{"Cache-Control": {"only-if-cached"}})
	if !errors.Is(err, ErrCacheMiss) || w.Code != 200 || w.Body.Len() != 0 || calls.Load() != 1 {
		t.Fatal("only-if-cached contacted origin", w.Code, err)
	}
	// A client request directive never selects a private upstream transfer:
	// the shared response is stored under the shared policy.
	if w, err = f.serve(t, "GET", http.Header{"Cache-Control": {"no-store"}}); err != nil || w.Body.String() != "body" {
		t.Fatal(w.Body.String(), err)
	}
	if len(f.rows(t)) != 1 || calls.Load() != 2 {
		t.Fatal("request no-store bypassed the shared cache", calls.Load())
	}
	for _, h := range []http.Header{
		{"Cache-Control": {"no-store"}},
		{"Cache-Control": {"no-cache"}},
		{"Cache-Control": {"max-age=0"}},
		{"Cache-Control": {"max-age=0, must-revalidate"}},
		{"Pragma": {"no-cache"}},
	} {
		if w, err = f.serve(t, "GET", h); err != nil || w.Code != 200 || w.Body.String() != "body" {
			t.Fatal(h, w.Code, err)
		}
		if w, err = f.serve(t, "HEAD", h); err != nil || w.Code != 200 {
			t.Fatal(h, w.Code, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("request directive forced an upstream request for a fresh entry", calls.Load())
	}
	if w, err = f.serve(t, "GET", http.Header{"Cache-Control": {"only-if-cached"}}); err != nil || w.Code != 200 || calls.Load() != 2 {
		t.Fatal("only-if-cached missed a fresh entry", w.Code, err)
	}
}

// Anonymous clients share one upstream transfer even when every request asks
// the cache to bypass storage.
func TestConcurrentRequestNoStoreSharesOneUpstreamFetch(t *testing.T) {
	const readers = 8
	var calls atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		io.WriteString(w, "body")
	}), 300)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	type reply struct {
		body string
		err  error
	}
	done := make(chan reply, readers)
	serve := func() {
		w, err := f.serve(t, "GET", http.Header{"Cache-Control": {"no-store, no-cache"}, "Pragma": {"no-cache"}})
		done <- reply{w.Body.String(), err}
	}
	go serve()
	<-entered
	for i := 1; i < readers; i++ {
		go serve()
	}
	waitForFlightWaiters(t, f.s, readers)
	releaseOnce.Do(func() { close(release) })
	for i := 0; i < readers; i++ {
		if got := <-done; got.err != nil || got.body != "body" {
			t.Fatal(got)
		}
	}
	if calls.Load() != 1 || len(f.rows(t)) != 1 {
		t.Fatal("request no-store multiplied upstream transfers", calls.Load())
	}
}

// An uncacheable shared result is claimed by one reader; every other waiter
// makes its own direct transfer instead of consuming the retry bound.
func TestConcurrentReadersOfUncacheablePathAllSucceed(t *testing.T) {
	const readers = 32
	var calls atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		io.WriteString(w, "body")
	}), 300)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	type reply struct {
		body string
		err  error
	}
	done := make(chan reply, readers)
	serve := func() {
		w, err := f.serve(t, "GET", http.Header{})
		done <- reply{w.Body.String(), err}
	}
	go serve()
	<-entered
	for i := 1; i < readers; i++ {
		go serve()
	}
	waitForFlightWaiters(t, f.s, readers)
	releaseOnce.Do(func() { close(release) })
	for i := 0; i < readers; i++ {
		if got := <-done; got.err != nil || got.body != "body" {
			t.Fatal(got)
		}
	}
	if calls.Load() != readers || len(f.rows(t)) != 0 {
		t.Fatal("uncacheable readers were not served by direct transfers", calls.Load())
	}
	if f.budget.readers.Load() != 0 || f.budget.writers.Load() != 0 {
		t.Fatal("capacity leaked")
	}
}

// waitForFlightWaiters observes admission into one shared flight. It polls
// in-memory state under a deadline; correctness never depends on timing.
func waitForFlightWaiters(t *testing.T, s *Service, n int) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		s.mu.Lock()
		joined := 0
		for _, f := range s.flights {
			joined += f.waiters
		}
		s.mu.Unlock()
		if joined == n {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("readers did not join the shared flight", joined)
		case <-tick.C:
		}
	}
}

// waitFor polls in-memory state under a deadline until ready reports true;
// correctness never depends on timing.
func waitFor(t *testing.T, failure string, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !ready() {
		select {
		case <-deadline.C:
			t.Fatal(failure)
		case <-tick.C:
		}
	}
}

// Repeated ErrFetchAgain ends with an error instead of looping forever.
func TestServeBoundsFetchAgainRetries(t *testing.T) {
	var calls atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, "body")
	}), 300)
	// A completed flight whose published generation has already disappeared
	// makes every joining caller retry with current storage state.
	done := make(chan struct{})
	close(done)
	churned := &flight{done: done, resolved: true, cancel: func() {}, result: fetchResult{row: &Row{GenerationID: "collected"}}}
	f.s.mu.Lock()
	f.s.flights[flightKey(f.entry, "file", "")] = churned
	f.s.mu.Unlock()
	w, err := f.serve(t, "GET", http.Header{})
	if !errors.Is(err, ErrFetchContended) || w.Code != 200 || w.Body.Len() != 0 || calls.Load() != 0 {
		t.Fatal("unbounded or misreported ErrFetchAgain retry", err, w.Code, calls.Load())
	}
	if f.budget.readers.Load() != 0 || f.budget.writers.Load() != 0 {
		t.Fatal("capacity leaked")
	}
}

func TestStaleFallbackDespiteSourceRevalidationDirectives(t *testing.T) {
	for _, directive := range []string{"no-cache", "must-revalidate", "proxy-revalidate", "s-maxage=0"} {
		t.Run(directive, func(t *testing.T) {
			var mode, calls atomic.Int64
			f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if mode.Load() != 0 && r.Header.Get("If-None-Match") != `"old"` {
					t.Error("revalidation omitted stored validator")
				}
				switch mode.Load() {
				case 1:
					w.Header().Set("ETag", `"old"`)
					w.WriteHeader(304)
					return
				case 2:
					w.WriteHeader(503)
					return
				case 3:
					w.Header().Set("ETag", `"new"`)
					io.WriteString(w, "updated")
					return
				}
				w.Header().Set("Cache-Control", directive)
				w.Header().Set("ETag", `"old"`)
				io.WriteString(w, "old")
			}), 0)
			if _, err := f.serve(t, "GET", http.Header{}); err != nil {
				t.Fatal(err)
			}
			initial := f.rows(t)[0]
			f.clock.Add(1)
			mode.Store(1)
			if w, err := f.serve(t, "GET", http.Header{}); err != nil || w.Code != 200 || w.Body.String() != "old" || calls.Load() != 2 {
				t.Fatal("normal validation bypassed", w.Code, err, calls.Load())
			}
			validated := f.rows(t)[0]
			if validated.GenerationID != initial.GenerationID || !validated.FetchedAt.Equal(initial.FetchedAt) || !validated.ValidatedAt.After(initial.ValidatedAt) {
				t.Fatal("304 validation did not preserve the body", validated)
			}
			mode.Store(2)
			for _, request := range []struct {
				method  string
				headers http.Header
				status  int
				body    string
			}{
				{"GET", http.Header{}, 200, "old"},
				{"HEAD", http.Header{}, 200, ""},
				{"GET", http.Header{"Range": {"bytes=1-2"}}, 206, "ld"},
				{"GET", http.Header{"If-None-Match": {`"old"`}}, 304, ""},
			} {
				f.clock.Add(60)
				before := calls.Load()
				w, err := f.serve(t, request.method, request.headers)
				if err != nil || w.Code != request.status || w.Body.String() != request.body || calls.Load() != before+1 {
					t.Fatalf("failure fallback or repeated validation lost: %s status=%d body=%q calls=%d err=%v", request.method, w.Code, w.Body.String(), calls.Load(), err)
				}
				row := f.rows(t)[0]
				if row.GenerationID != validated.GenerationID || row.SHA256 != validated.SHA256 || !row.FetchedAt.Equal(validated.FetchedAt) || !row.ValidatedAt.Equal(validated.ValidatedAt) {
					t.Fatal("fallback changed the cached representation or freshness", row)
				}
			}
			mode.Store(3)
			before := calls.Load()
			if w, err := f.serve(t, "GET", http.Header{}); err != nil || w.Code != 200 || w.Body.String() != "updated" || calls.Load() != before+1 {
				t.Fatal("source recovery did not replace stale body", w.Code, err)
			}
			if row := f.rows(t)[0]; row.GenerationID == validated.GenerationID || row.SHA256 == validated.SHA256 {
				t.Fatal("recovered body was not published", row)
			}
		})
	}
}

func TestSourceRetirementFence(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; io.WriteString(w, "body") }), 0)
	done := make(chan error, 1)
	go func() { _, err := f.serve(t, "GET", http.Header{}); done <- err }()
	<-entered
	changes := store.ApplicationChanges{Name: f.app.Name, BaseURL: f.app.BaseURL, CacheTTLSeconds: 0, Enabled: false}
	if _, err := f.db.UpdateApplication(f.app.Key, f.app.Revision, changes); err != nil {
		t.Fatal(err)
	}
	close(release)
	// The request admitted before the change receives its response, which
	// is never published for the disabled application.
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(f.rows(t)) != 0 {
		t.Fatal("disabled generation persisted")
	}
}

func TestCleanupAccessRecheckPinsReceiptAndRefreshFence(t *testing.T) {
	var block atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if block.Load() {
			close(entered)
			<-release
		}
		io.WriteString(w, "body")
	}), 300)
	if _, err := f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(400)
	preview, err := f.s.PreviewCleanup(context.Background(), f.entry, "last_access", f.s.now().Add(-time.Second), allPaths)
	if err != nil || preview.SelectedFiles != 1 {
		t.Fatal(preview, err)
	}
	if _, err = f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	result, err := f.s.ExecuteCleanup(context.Background(), f.entry, preview.ID)
	if err != nil || result.SkippedChanged+result.SkippedAccessed != 1 {
		t.Fatal(result, err)
	}
	f.clock.Add(400)
	preview, err = f.s.PreviewCleanup(context.Background(), f.entry, "fetched_at", f.s.now().Add(-time.Second), allPaths)
	if err != nil {
		t.Fatal(err)
	}
	row, err := f.s.lookup(context.Background(), f.entry.StorageID(), "file")
	if err != nil {
		t.Fatal(err)
	}
	result, err = f.s.ExecuteCleanup(context.Background(), f.entry, preview.ID)
	if err != nil || result.RetiredFiles != 1 {
		t.Fatal(result, err)
	}
	if _, err = os.Stat(f.s.bodyPath(row.GenerationID)); err != nil {
		t.Fatal("pinned body removed", err)
	}
	f.s.unpin(row.GenerationID)
	if _, err = os.Stat(f.s.bodyPath(row.GenerationID)); !os.IsNotExist(err) {
		t.Fatal("unpinned retired body not collected", err)
	}
	again, err := f.s.ExecuteCleanup(context.Background(), f.entry, preview.ID)
	if err != nil || !reflect.DeepEqual(result, again) {
		t.Fatal("receipt not idempotent", again, err)
	}
	if _, err = f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(400)
	block.Store(true)
	done := make(chan error, 1)
	go func() { _, err := f.serve(t, "GET", http.Header{}); done <- err }()
	<-entered
	preview, err = f.s.PreviewCleanup(context.Background(), f.entry, "fetched_at", f.s.now().Add(-time.Second), allPaths)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ExecuteCleanup(context.Background(), f.entry, preview.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	// The admitted request still receives the changed body, which may no
	// longer replace the entry the cleanup retired.
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if len(f.rows(t)) != 0 {
		t.Fatal("cleanup resurrected current body")
	}
}

func TestBoundsCancellationRecoveryAndPolicyParsing(t *testing.T) {
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		io.WriteString(w, "too-large-body")
	}), 300)
	f.budget.limit = 4
	if _, err := f.serve(t, "GET", http.Header{}); !errors.Is(err, download.ErrArtifactLimit) {
		t.Fatal("unknown body exceeded limit", err)
	}
	if len(f.rows(t)) != 0 {
		t.Fatal("oversized body published")
	}
	now := time.Now()
	for _, h := range []http.Header{{"Cache-Control": {"max-age=0, max-age=300"}}, {"Cache-Control": {"public"}, "Expires": {"0"}}} {
		if freshness(h, 300, now).After(now) {
			t.Fatal("invalid freshness extended lifetime", h)
		}
	}
	entered := make(chan struct{})
	g := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }), 300)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "http://redapp/file", nil).WithContext(ctx)
	done := make(chan error, 1)
	go func() { done <- g.s.Serve(httptest.NewRecorder(), req, g.entry, "file") }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if g.budget.readers.Load() != 0 || g.budget.writers.Load() != 0 {
		t.Fatal("cancel leaked capacity")
	}
	// Refuse a symlinked cache directory, without touching its destination.
	other := t.TempDir()
	symlinkDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(symlinkDir, "objects"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(symlinkDir, "objects", "http")); err == nil {
		if _, err = New(symlinkDir, g.db, g.budget); err == nil {
			t.Fatal("symlink cache directory accepted")
		}
	}
}

func TestActiveSnapshotCountersAndNoStoreInterruptedBody(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var mode atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode.Load() == 1 {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Length", "100")
			io.WriteString(w, "partial")
			return
		}
		w.Header().Set("Content-Length", "8")
		io.WriteString(w, "body")
		w.(http.Flusher).Flush()
		close(entered)
		<-release
		io.WriteString(w, "more")
	}), 300)
	done := make(chan error, 1)
	go func() { _, err := f.serve(t, "GET", http.Header{}); done <- err }()
	<-entered
	// The writer is visible once the response headers arrived and while it
	// waits for the rest of the body.
	waitFor(t, "active HTTP transfer invisible", func() bool {
		views, err := f.s.Files()
		if err != nil {
			t.Fatal(err)
		}
		active := 0
		for _, v := range views {
			if v.ActiveWriter && v.State == "downloading" && v.Scope == f.entry.MetricsID() {
				active++
			}
		}
		return active == 1
	})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	counts, err := f.db.CountersFor(f.entry.MetricsID())
	if err != nil {
		t.Fatal(err)
	}
	if counts["artifact_requests"] != 2 || counts["upstream_bytes"] != 8 || counts["downstream_bytes"] != 16 || counts["cache_hit_requests"] != 1 || counts["miss_requests"] != 1 {
		t.Fatal("HTTP counters differ", counts)
	}
	mode.Store(1)
	f.clock.Add(400)
	aborted := false
	func() {
		defer func() {
			if p := recover(); p == http.ErrAbortHandler {
				aborted = true
			} else if p != nil {
				panic(p)
			}
		}()
		_, err = f.serve(t, "GET", http.Header{})
	}()
	if !aborted {
		t.Fatal("private truncated body was not aborted", err)
	}
	if len(f.rows(t)) != 0 {
		t.Fatal("no-store header did not detach stale head")
	}
	files, _ := os.ReadDir(f.s.dir)
	if len(files) != 0 {
		t.Fatal("no-store bytes stored on disk", files)
	}
	if f.budget.readers.Load() != 0 || f.budget.writers.Load() != 0 {
		t.Fatal("private failure leaked leases")
	}
}

func TestSourceEpochIsolationAndRecovery(t *testing.T) {
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "first") }), 300)
	if _, err := f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	firstRow := f.rows(t)[0]
	if err := os.Remove(f.s.bodyPath(firstRow.GenerationID)); err != nil {
		t.Fatal(err)
	}
	if w, err := f.serve(t, "GET", http.Header{}); err != nil || w.Body.String() != "first" {
		t.Fatal("missing complete body did not refetch", err)
	}
	oldEntry := f.entry
	next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "second") }))
	defer next.Close()
	changed, err := f.db.UpdateApplication(f.app.Key, f.app.Revision, store.ApplicationChanges{Name: f.app.Name, BaseURL: next.URL + "/files", CacheTTLSeconds: 300, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	f.entry.Revision = changed.Revision
	f.entry.RuntimeRevision = changed.RuntimeRevision
	f.entry.SourceEpoch = changed.SourceEpoch
	f.entry.Upstream, _ = distributor.NewPool().NewClient(changed.BaseURL, distributor.GeneralHTTP)
	w, err := f.serve(t, "GET", http.Header{})
	if err != nil || w.Body.String() != "second" {
		t.Fatal("new source inherited cached bytes", w.Body.String(), err)
	}
	oldRows, err := f.s.listRows(oldEntry.StorageID())
	if err != nil || len(oldRows) != 1 {
		t.Fatal("source edit removed old data", oldRows, err)
	}
	if err = f.s.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "http://redapp/file", nil), oldEntry, "file"); !errors.Is(err, store.ErrSourceInactive) {
		t.Fatal("old epoch admitted", err)
	}
	current := f.rows(t)[0]
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(filepath.Dir(f.s.dir))
	recovered, err := New(dir, f.db, f.budget)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := recovered.listRows(f.entry.StorageID()); err != nil || len(rows) != 1 {
		t.Fatal("complete body lost on restart", rows, err)
	}
	recovered.Close()
	if err = os.WriteFile(f.s.bodyPath(current.GenerationID), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err = New(dir, f.db, f.budget)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	// Recovery does not hash bodies; their first use does and replaces the
	// corrupt one with a fresh transfer.
	w = httptest.NewRecorder()
	if err = recovered.Serve(w, httptest.NewRequest("GET", "http://redapp/file", nil), f.entry, "file"); err != nil || w.Body.String() != "second" {
		t.Fatal("corrupt observed hash accepted", w.Body.String(), err)
	}
	if rows, err := recovered.listRows(f.entry.StorageID()); err != nil || len(rows) != 1 || rows[0].GenerationID == current.GenerationID {
		t.Fatal("corrupt body not replaced", rows, err)
	}
	if rows, err := recovered.listRows(oldEntry.StorageID()); err != nil || len(rows) != 1 {
		t.Fatal("recovery deleted unrelated historical source", rows, err)
	}
}
