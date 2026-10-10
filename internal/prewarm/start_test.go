package prewarm

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/catalog"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/httpcache"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
	"github.com/PMExtra/RedApp/internal/warmplan"
)

// newService returns a worker for one HTTP cache application, key cache/files.
func newService(t *testing.T) (*Service, application.Entry) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	upstream, origin := testutil.Upstream(t, http.NotFoundHandler())
	app := testutil.App(t, db, "cache/files", store.ApplicationInput{Provider: application.HttpCache, BaseURL: origin.URL, CacheTTLSeconds: 60})
	entry := testutil.Entry(t, db, app.Key, application.Entry{Descriptor: application.Descriptor{DefaultChannelTTLSeconds: 60}, Upstream: upstream})
	registry, err := application.NewRegistry([]application.Entry{entry})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := download.NewApplications(dir, db, map[string]*distributor.Client{entry.StorageID(): upstream})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	cache, err := httpcache.New(dir, db, manager)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cache.Close() })
	worker, err := New(db, registry, catalog.New(db, registry), manager, cache)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	return worker, entry
}

// matchAll selects every cached path; with an empty cache the job finishes
// without contacting the upstream.
func matchAll(requestID string) warmplan.Input {
	return warmplan.Input{RequestID: strings.Repeat(requestID, 32), Match: &pathmatch.Spec{Type: "glob", Pattern: "/"}}
}

// A job whose terminal state is persisted still holds the worker slot until
// its goroutine exits. A start in that window waits for the slot instead of
// reporting the finished job as busy.
func TestStartWaitsForFinishedJobToReleaseSlot(t *testing.T) {
	persisted, release := make(chan struct{}), make(chan struct{})
	first := true
	jobFinished = func() {
		if first {
			first = false
			close(persisted)
			<-release
		}
	}
	t.Cleanup(func() { jobFinished = nil })
	worker, entry := newService(t)
	var once sync.Once
	releaseSlot := func() { once.Do(func() { close(release) }) }
	// Runs before the worker closes, so a failed assertion cannot block Close.
	t.Cleanup(releaseSlot)
	job, created, err := worker.Start(context.Background(), entry.Descriptor.ID, matchAll("a"), false)
	if err != nil || !created {
		t.Fatal(job, created, err)
	}
	<-persisted
	if status, err := worker.Status(entry.UID, job.ID); err != nil || status.State != "completed" {
		t.Fatal("terminal state not persisted", status, err)
	}
	// A start that gives up while waiting reports its own cancellation, not busy.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err = worker.Start(cancelled, entry.Descriptor.ID, matchAll("b"), false); !errors.Is(err, context.Canceled) {
		t.Fatal("start during slot release", err)
	}
	type result struct {
		job     store.PrewarmJob
		created bool
		err     error
	}
	started := make(chan result, 1)
	go func() {
		next, created, err := worker.Start(context.Background(), entry.Descriptor.ID, matchAll("c"), false)
		started <- result{next, created, err}
	}()
	releaseSlot()
	next := <-started
	if next.err != nil || !next.created || next.job.ID == job.ID {
		t.Fatal("start after slot release", next)
	}
}
