package httpcache

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCleanupSeparatesNewReaderFromRetiredGenerationFlight(t *testing.T) {
	var calls atomic.Int64
	leaderEntered, coldEntered, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			io.WriteString(w, "retired-body")
		case 2:
			close(leaderEntered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		case 3:
			close(coldEntered)
			io.WriteString(w, "new-body")
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}), 0)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	if _, err := f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	oldID := f.rows(t)[0].GenerationID
	f.clock.Add(1)
	type reply struct {
		body string
		err  error
	}
	serve := func(done chan<- reply) {
		w := httptest.NewRecorder()
		err := f.s.Serve(w, httptest.NewRequest("GET", "http://redapp/file", nil), f.entry, "file")
		done <- reply{w.Body.String(), err}
	}
	leaderDone, coldDone := make(chan reply, 1), make(chan reply, 1)
	go serve(leaderDone)
	<-leaderEntered
	preview, err := f.s.Preview(f.entry, "fetched_at", f.s.now())
	if err != nil || preview.SelectedFiles != 1 {
		t.Fatal(preview, err)
	}
	result, err := f.s.Execute(f.entry, preview.ID)
	if err != nil || result.RetiredFiles != 1 || len(f.rows(t)) != 0 {
		t.Fatal(result, err)
	}
	// This reader was admitted after cleanup. It owns no pin on the retired
	// generation and must start a cold fetch while the old reader may finish.
	go serve(coldDone)
	select {
	case <-coldEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("post-cleanup reader joined the retired generation's blocked flight")
	}
	cold := <-coldDone
	if cold.err != nil || cold.body != "new-body" {
		t.Fatal("new admission received retired fallback", cold)
	}
	releaseOnce.Do(func() { close(release) })
	leader := <-leaderDone
	if leader.err != nil || leader.body != "retired-body" {
		t.Fatal("cleanup interrupted an already admitted reader", leader)
	}
	rows := f.rows(t)
	if len(rows) != 1 || rows[0].GenerationID == oldID {
		t.Fatal("old flight resurrected its retired generation", rows)
	}
	if f.budget.readers.Load() != 0 || f.budget.writers.Load() != 0 {
		t.Fatal("generation-separated flights leaked capacity")
	}
}

// Hold a completed cold fetch after publish has pinned its new generation but
// before sharedFetch can expose the result and remove its original empty key.
type publicationGateBudget struct {
	*testBudget
	published chan struct{}
	release   chan struct{}
	once      sync.Once
}

func (b *publicationGateBudget) AcquireHTTPWriter() (func(), error) {
	release, err := b.testBudget.AcquireHTTPWriter()
	if err != nil {
		return nil, err
	}
	block := false
	b.once.Do(func() { block = true })
	return func() {
		release()
		if block {
			close(b.published)
			<-b.release
		}
	}, nil
}

func TestCleanupSeparatesColdReaderAfterColdFlightPublication(t *testing.T) {
	var calls atomic.Int64
	coldEntered := make(chan struct{})
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			io.WriteString(w, "retired-body")
			return
		}
		close(coldEntered)
		io.WriteString(w, "new-body")
	}), 0)
	gate := &publicationGateBudget{testBudget: f.budget, published: make(chan struct{}), release: make(chan struct{})}
	f.s.budget = gate
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(gate.release) }) })
	type reply struct {
		body string
		err  error
	}
	serve := func(done chan<- reply) {
		w := httptest.NewRecorder()
		err := f.s.Serve(w, httptest.NewRequest("GET", "http://redapp/file", nil), f.entry, "file")
		done <- reply{w.Body.String(), err}
	}
	leaderDone := make(chan reply, 1)
	go serve(leaderDone)
	<-gate.published
	oldID := f.rows(t)[0].GenerationID
	f.clock.Add(1)
	preview, err := f.s.Preview(f.entry, "fetched_at", f.s.now())
	if err != nil || preview.SelectedFiles != 1 {
		t.Fatal(preview, err)
	}
	result, err := f.s.Execute(f.entry, preview.ID)
	if err != nil || result.RetiredFiles != 1 || len(f.rows(t)) != 0 {
		t.Fatal(result, err)
	}
	policy, err := f.s.readPolicy(f.entry)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &joinedContext{Context: context.WithValue(context.Background(), policyContextKey{}, policy), joined: make(chan struct{})}
	type outcome struct {
		result fetchResult
		err    error
	}
	followerDone := make(chan outcome, 1)
	go func() {
		result, err := f.s.sharedFetch(ctx, f.entry, "file", nil)
		followerDone <- outcome{result, err}
	}()
	<-ctx.joined
	// The caller can initially share the leader's empty-generation key, but it
	// must reject the retired result and retry independently after completion.
	releaseOnce.Do(func() { close(gate.release) })
	follower := <-followerDone
	if follower.result.row != nil {
		f.s.unpin(follower.result.row.GenerationID)
	}
	if !errors.Is(follower.err, ErrFetchAgain) {
		t.Fatal("post-cleanup cold follower accepted a retired cold-flight result", follower)
	}
	// Serve retries ErrFetchAgain from its next lookup. A real cold HTTP request
	// now obtains the new body independently; the original leader keeps its pin.
	cold, err := f.serve(t, "GET", http.Header{})
	leader := <-leaderDone
	if err != nil || cold.Body.String() != "new-body" || leader.err != nil || leader.body != "retired-body" {
		t.Fatal("cold admission/leader pin boundary violated", cold.Body.String(), err, leader)
	}
	rows := f.rows(t)
	if len(rows) != 1 || rows[0].GenerationID == oldID || calls.Load() != 2 {
		t.Fatal("retired publication was reused or resurrected", rows, calls.Load())
	}
}

// Refresh wraps its input context, so joinedContext cannot observe its wait.
// This test-only, bounded barrier observes the actual sharedFetch wait frame;
// it adds no production hook and never logs complete goroutine stacks.
func waitForRefreshSharedFlight(t *testing.T) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	buffer := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buffer, true)
		for _, stack := range strings.Split(string(buffer[:n]), "\n\n") {
			lines := strings.SplitN(stack, "\n", 3)
			if len(lines) >= 2 && strings.Contains(lines[1], "(*Service).sharedFetch(") && strings.Contains(stack, "(*Service).refreshExisting(") {
				return
			}
		}
		select {
		case <-deadline.C:
			t.Fatal("forced refresh did not join the ordinary in-flight validation")
		case <-tick.C:
		}
	}
}

func TestRefreshSharesOrdinaryGenerationValidationResult(t *testing.T) {
	for _, stale := range []bool{false, true} {
		name := "not_modified"
		if stale {
			name = "stale_fallback"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int64
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				w.Header().Set("ETag", `"stable"`)
				if n == 1 {
					io.WriteString(w, "body")
					return
				}
				if n == 2 {
					close(entered)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				if stale {
					w.WriteHeader(http.StatusServiceUnavailable)
				} else {
					w.WriteHeader(http.StatusNotModified)
				}
			}), 0)
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			if _, err := f.serve(t, "GET", http.Header{}); err != nil {
				t.Fatal(err)
			}
			before := f.rows(t)[0]
			f.clock.Add(1)
			ordinaryDone := make(chan error, 1)
			go func() {
				ordinaryDone <- f.s.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "http://redapp/file", nil), f.entry, "file")
			}()
			<-entered
			type refreshed struct {
				item RefreshItem
				err  error
			}
			refreshDone := make(chan refreshed, 1)
			go func() {
				item, err := f.s.Refresh(context.Background(), f.entry, "/file")
				refreshDone <- refreshed{item, err}
			}()
			waitForRefreshSharedFlight(t)
			releaseOnce.Do(func() { close(release) })
			if err := <-ordinaryDone; err != nil {
				t.Fatal(err)
			}
			force := <-refreshDone
			if force.err != nil || force.item.Status != name || force.item.GenerationID != before.GenerationID || calls.Load() != 2 {
				t.Fatal("forced refresh mislabeled or repeated the shared outcome", force, calls.Load())
			}
			after := f.rows(t)[0]
			if stale && !after.ValidatedAt.Equal(before.ValidatedAt) || !stale && !after.ValidatedAt.After(before.ValidatedAt) {
				t.Fatal("shared validation timestamp disagrees with its outcome")
			}
			if len(warningEvents(t, f, "cache_rule_override")) != 0 {
				t.Fatal("TTL zero emitted an override warning")
			}
			wantWarnings := 0
			if stale {
				wantWarnings = 1
			}
			if len(warningEvents(t, f, "stale_cache_fallback")) != wantWarnings {
				t.Fatal("shared flight duplicated or omitted actual fallback warning")
			}
		})
	}
}

func TestColdLookupRetriesAfterAnotherFlightPublishes(t *testing.T) {
	var calls atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, "body")
	}), 300)
	// A caller observes a miss, then pauses before joining the shared fetch.
	old, err := f.s.lookup(f.entry.StorageID(), "file")
	if err != nil || old != nil {
		t.Fatal(old, err)
	}
	// Another caller completes and removes its flight before the first resumes.
	if _, err = f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	result, err := f.s.sharedFetch(context.Background(), f.entry, "file", old)
	if result.row != nil {
		f.s.unpin(result.row.GenerationID)
	}
	if !errors.Is(err, ErrFetchAgain) || calls.Load() != 1 {
		t.Fatal("late cold caller repeated an already published fetch", calls.Load(), err)
	}
	response, err := f.serve(t, "GET", http.Header{})
	if err != nil || response.Body.String() != "body" || calls.Load() != 1 {
		t.Fatal("retry did not reuse the current cache", calls.Load(), err)
	}
}
