package httpcache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
)

func TestManualRefreshForcesValidationWithoutRecordingAccess(t *testing.T) {
	var calls, mode atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if mode.Load() == 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		tag, body := `"v1"`, "original"
		if mode.Load() == 1 {
			tag, body = `"v2"`, "replacement"
		}
		w.Header().Set("ETag", tag)
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		io.WriteString(w, body)
	}), 300)
	if _, err := f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	before := f.rows(t)[0]
	f.clock.Add(120)
	item, err := f.s.Refresh(context.Background(), f.entry, "/file")
	if err != nil || item.Status != "not_modified" || calls.Load() != 2 {
		t.Fatalf("fresh resource was not explicitly validated: %+v, %v, calls=%d", item, err, calls.Load())
	}
	after := f.rows(t)[0]
	if after.GenerationID != before.GenerationID || !after.FetchedAt.Equal(before.FetchedAt) || after.accessBucket != before.accessBucket {
		t.Fatal("304/manual access changed body identity or last-access time", before, after)
	}
	mode.Store(1)
	item, err = f.s.Refresh(context.Background(), f.entry, "/file")
	if err != nil || item.Status != "refreshed" || item.GenerationID == before.GenerationID {
		t.Fatal("updated body was not reported as refreshed", item, err)
	}
	before = f.rows(t)[0]
	mode.Store(2)
	item, err = f.s.Refresh(context.Background(), f.entry, "/file")
	if err != nil || item.Status != "stale_fallback" || item.GenerationID != before.GenerationID {
		t.Fatal("failure fallback was reported as a successful refresh", item, err)
	}
	after = f.rows(t)[0]
	if !before.ValidatedAt.Equal(after.ValidatedAt) || before.accessBucket != after.accessBucket {
		t.Fatal("failure fallback changed freshness/access time")
	}
	config := cachepolicy.Empty()
	config.StaleFallback = false
	updated, err := f.db.SaveHTTPPolicy(f.app.Key, f.entry.Revision, config)
	if err != nil {
		t.Fatal(err)
	}
	f.entry.Revision = updated.Revision
	f.entry.RuntimeRevision = updated.RuntimeRevision
	item, err = f.s.Refresh(context.Background(), f.entry, "/file")
	if err == nil || item.Status != "failed" {
		t.Fatal("disabled failure fallback was ignored", item, err)
	}
	if _, err := os.Stat(f.s.bodyPath(before.GenerationID)); err != nil {
		t.Fatal("failed refresh removed the complete old body", err)
	}
	count := calls.Load()
	for _, path := range []string{"/missing", "/../escape", "file", "/", "/file?query=1"} {
		if _, err := f.s.Refresh(context.Background(), f.entry, path); err == nil {
			t.Fatalf("accepted missing/noncanonical refresh path %q", path)
		}
	}
	if calls.Load() != count {
		t.Fatal("manual refresh crawled a missing path")
	}
	if f.budget.readers.Load() != 0 || f.budget.writers.Load() != 0 {
		t.Fatal("refresh leaked shared capacity")
	}
}

func TestManualRefreshReportsUncacheableResponseReason(t *testing.T) {
	for _, conditional := range []bool{false, true} {
		for _, test := range []struct {
			header, value, reason string
		}{
			{"Set-Cookie", "session=private", "set-cookie"},
			{"Vary", "Origin", "unsupported-vary"},
			{"Cache-Control", "no-store", "source-no-store"},
			{"Cache-Control", "private", "source-private"},
		} {
			t.Run(fmt.Sprintf("%s/conditional=%t", test.reason, conditional), func(t *testing.T) {
				var changed atomic.Bool
				f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("ETag", `"stable"`)
					if changed.Load() {
						w.Header().Set(test.header, test.value)
						if conditional && r.Header.Get("If-None-Match") != "" {
							w.WriteHeader(http.StatusNotModified)
							return
						}
					}
					io.WriteString(w, "body")
				}), 300)
				if _, err := f.serve(t, "GET", http.Header{}); err != nil {
					t.Fatal(err)
				}
				changed.Store(true)
				item, err := f.s.Refresh(context.Background(), f.entry, "/file")
				if err != nil || item.Status != "skipped" || item.Reason != test.reason {
					t.Fatalf("expected explicit uncacheable reason %q: %+v, %v", test.reason, item, err)
				}
				if f.budget.readers.Load() != 0 || f.budget.writers.Load() != 0 {
					t.Fatal("uncacheable refresh leaked shared capacity")
				}
			})
		}
	}
}

func waitRefresh(t *testing.T, f *fixture, id string) MaintenancePreview {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		preview, err := f.s.LookupPreview(f.entry.StorageID(), "refresh", id)
		if err != nil {
			t.Fatal(err)
		}
		if preview.State == "done" || preview.State == "failed" {
			return preview
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("refresh worker did not finish")
	return MaintenancePreview{}
}

func TestBatchRefreshPagesFrozenSelectionAndDurableReceipt(t *testing.T) {
	var calls, validations atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("ETag", `"stable"`)
		if r.Header.Get("If-None-Match") != "" {
			validations.Add(1)
			enterOnce.Do(func() { close(entered) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
		io.WriteString(w, r.URL.Path)
	}), 300)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	seed := func(path string) {
		t.Helper()
		if err := f.s.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "http://redapp/"+path, nil), f.entry, path); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 26; i++ {
		seed(fmt.Sprintf("batch/%02d", i))
	}
	preview, err := f.s.PreviewRefresh(context.Background(), f.entry, pathmatch.Spec{Type: "glob", Pattern: "/batch/"})
	if err != nil || preview.SelectedFiles != 26 {
		t.Fatal(preview, err)
	}
	page, err := f.s.PreviewItems(f.entry.StorageID(), "refresh", preview.ID, "", 25)
	if err != nil || len(page.Items) != 25 || page.NextCursor == "" || page.TotalFiles != 26 {
		t.Fatal(page, err)
	}
	last, err := f.s.PreviewItems(f.entry.StorageID(), "refresh", preview.ID, page.NextCursor, 25)
	if err != nil || len(last.Items) != 1 || last.NextCursor != "" {
		t.Fatal(last, err)
	}
	seed("batch/late")
	old, err := f.s.lookup(f.entry.StorageID(), last.Items[0].Path)
	if err != nil || old == nil {
		t.Fatal(old, err)
	}
	if err = f.s.retire(old); err != nil {
		t.Fatal(err)
	}
	f.s.unpin(old.GenerationID)
	ctx, cancel := context.WithCancel(context.Background())
	started, err := f.s.ExecuteRefresh(ctx, f.entry, preview.ID)
	if err != nil || started.State != "running" {
		t.Fatal(started, err)
	}
	<-entered
	cancel() // The background operation belongs to the service, not this request.
	if _, err = f.s.ExecuteRefresh(context.Background(), f.entry, preview.ID); !errors.Is(err, ErrRefreshBusy) {
		t.Fatal("running refresh was started twice", err)
	}
	f.clock.Add(601) // Starting before expiry permits a long-running batch to finish.
	releaseOnce.Do(func() { close(release) })
	finished := waitRefresh(t, f, preview.ID)
	if finished.State != "done" || finished.CompletedFiles != 26 || finished.FailedFiles != 0 || validations.Load() != 25 {
		t.Fatal(finished, validations.Load())
	}
	var summary RefreshSummary
	if err := json.Unmarshal(finished.result, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.SelectedFiles != 26 || summary.CompletedFiles != 26 || summary.NotModified != 25 || summary.Skipped != 1 || summary.Refreshed != 0 || summary.StaleFallback != 0 {
		t.Fatal(summary)
	}
	before := calls.Load()
	repeated, err := f.s.ExecuteRefresh(context.Background(), f.entry, preview.ID)
	if err != nil || repeated.State != "done" || calls.Load() != before {
		t.Fatal("receipt repeated upstream work", repeated, err)
	}
	var cursor string
	statuses := map[string]int{}
	for {
		page, err = f.s.PreviewItems(f.entry.StorageID(), "refresh", preview.ID, cursor, 25)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			statuses[item.ResultStatus]++
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if statuses["not_modified"] != 25 || statuses["skipped"] != 1 || statuses["pending"] != 0 {
		t.Fatal(statuses)
	}
}

func TestBatchRefreshRejectsStalePolicyAndStopsAfterSourceFence(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var block atomic.Bool
	var releaseOnce sync.Once
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if block.Load() {
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
	if _, err := f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	preview, err := f.s.PreviewRefresh(context.Background(), f.entry, pathmatch.Spec{Type: "glob", Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}
	changedPolicy := cachepolicy.Empty()
	changedPolicy.StaleFallback = false
	updated, err := f.db.SaveHTTPPolicy(f.app.Key, f.entry.Revision, changedPolicy)
	if err != nil {
		t.Fatal(err)
	}
	f.entry.Revision = updated.Revision
	f.entry.RuntimeRevision = updated.RuntimeRevision
	if _, err = f.s.ExecuteRefresh(context.Background(), f.entry, preview.ID); !errors.Is(err, store.ErrSourceInactive) {
		t.Fatal("old policy preview executed", err)
	}
	preview, err = f.s.PreviewRefresh(context.Background(), f.entry, pathmatch.Spec{Type: "glob", Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}
	block.Store(true)
	if _, err = f.s.ExecuteRefresh(context.Background(), f.entry, preview.ID); err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err = f.db.SaveHTTPPolicy(f.app.Key, f.entry.Revision, cachepolicy.Empty()); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(release) })
	finished := waitRefresh(t, f, preview.ID)
	if finished.State != "failed" {
		t.Fatal("source fence change left job active", finished)
	}
	if rows := f.rows(t); len(rows) != 1 {
		t.Fatal("failed refresh removed original body", rows)
	}
}

func TestManualRefreshCannotRepublishAfterCleanup(t *testing.T) {
	var block atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if block.Load() {
			close(entered)
			<-release
		}
		io.WriteString(w, "contents")
	}), 300)
	if _, err := f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	block.Store(true)
	done := make(chan error, 1)
	go func() { _, err := f.s.Refresh(context.Background(), f.entry, "/file"); done <- err }()
	<-entered
	old, err := f.s.lookup(f.entry.StorageID(), "file")
	if err != nil || old == nil {
		t.Fatal(old, err)
	}
	if err := f.s.retire(old); err != nil {
		t.Fatal(err)
	}
	f.s.unpin(old.GenerationID)
	close(release)
	if err := <-done; !errors.Is(err, ErrUpstream) {
		t.Fatal("retired generation was republished", err)
	}
	if len(f.rows(t)) != 0 {
		t.Fatal("cleanup was undone by explicit refresh")
	}
}

func TestPermanentDeletionCancelsWholeRefreshAndSharedFollowers(t *testing.T) {
	started := make(chan struct{})
	var refresh atomic.Bool
	var calls atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if refresh.Load() {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-r.Context().Done()
			return
		}
		w.Header().Set("Cache-Control", "max-age=3600")
		io.WriteString(w, "cached")
	}), 300)
	for _, path := range []string{"file", "second"} {
		if err := f.s.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "http://local/"+path, nil), f.entry, path); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := f.s.PreviewRefresh(context.Background(), f.entry, pathmatch.Spec{Type: "glob", Pattern: "/**"})
	if err != nil || preview.SelectedFiles != 2 {
		t.Fatal(preview, err)
	}
	refresh.Store(true)
	if _, err = f.s.ExecuteRefresh(context.Background(), f.entry, preview.ID); err != nil {
		t.Fatal(err)
	}
	<-started
	// Join the same refresh flight as a public reader of the now-stale entry.
	f.clock.Add(3601)
	follower := make(chan error, 1)
	go func() {
		r := httptest.NewRequest("GET", "http://local/file", nil)
		follower <- f.s.Serve(httptest.NewRecorder(), r, f.entry, "file")
	}()
	deadline := time.Now().Add(time.Second)
	for f.budget.readers.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.budget.readers.Load() != 2 {
		t.Fatal("follower did not enter")
	}
	uid, drained, err := f.db.PrepareApplicationDeletion(f.app.Key, f.app.Revision)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("batch or follower did not exit")
	}
	if err = <-follower; err == nil {
		t.Fatal("follower survived cancellation")
	}
	if calls.Load() != 1 || f.budget.readers.Load() != 0 || f.budget.writers.Load() != 0 {
		t.Fatal("next batch restarted or leases leaked", calls.Load())
	}
	if err = f.db.FinishApplicationDeletion(uid); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ExecuteRefresh(context.Background(), f.entry, preview.ID); err == nil {
		t.Fatal("stale batch restarted")
	}
}
