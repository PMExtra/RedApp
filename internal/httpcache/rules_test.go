package httpcache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
)

func setPolicy(t *testing.T, f *fixture, config cachepolicy.Config) {
	t.Helper()
	updated, err := f.db.SaveHTTPPolicy(f.app.Key, f.entry.Revision, config)
	if err != nil {
		t.Fatal(err)
	}
	f.app = updated
	f.entry.Revision = updated.Revision
	f.entry.RuntimeRevision = updated.RuntimeRevision
}
func servePath(t *testing.T, f *fixture, path string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	w := httptest.NewRecorder()
	err := f.s.Serve(w, httptest.NewRequest("GET", "http://redapp/"+path, nil), f.entry, path)
	return w, err
}
func registryFor(t *testing.T, entries ...application.Entry) *application.Registry {
	t.Helper()
	r, err := application.NewRegistry(entries)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPathRuleTTLAndCurrentListUseSamePolicy(t *testing.T) {
	var calls atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Cache-Control", "max-age=1")
		w.Header().Set("Age", "40")
		io.WriteString(w, "body")
	}), 300)
	config := cachepolicy.Empty()
	config.Rules = []cachepolicy.CacheRule{
		{Match: pathmatch.Spec{Type: "glob", Pattern: "/reports/"}, TTLSeconds: 600},
		{Match: pathmatch.Spec{Type: "glob", Pattern: "/"}, TTLSeconds: 120},
	}
	setPolicy(t, f, config)
	if _, err := servePath(t, f, "reports/day.txt"); err != nil {
		t.Fatal(err)
	}
	rows, err := f.s.ListEntry(f.entry)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	if !rows[0].FreshUntil.Equal(rows[0].ValidatedAt.Add(600 * time.Second)) {
		t.Fatal("explicit TTL clipped by header/default", rows[0])
	}
	f.clock.Add(50)
	if _, err = servePath(t, f, "reports/day.txt"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("first-match TTL not used for serving", calls.Load())
	}
	config.Rules[0].TTLSeconds = 10
	setPolicy(t, f, config)
	listed, err := f.s.ListEntry(f.entry)
	if err != nil || !listed[0].FreshUntil.Equal(listed[0].ValidatedAt.Add(10*time.Second)) {
		t.Fatal("row list retained old TTL", listed, err)
	}
	if _, err = servePath(t, f, "reports/day.txt"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("edited TTL did not invalidate freshness", calls.Load())
	}
}

func TestPatternPreviewFreezesMatchAndPolicyRevision(t *testing.T) {
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "body") }), 300)
	for _, path := range []string{"reports/day.txt", "reports/sub/month.txt", "elsewhere/day.txt"} {
		if _, err := servePath(t, f, path); err != nil {
			t.Fatal(err)
		}
	}
	f.clock.Add(300)
	match := pathmatch.Spec{Type: "glob", Pattern: "/reports/"}
	preview, err := f.s.PreviewCleanup(context.Background(), f.entry, "fetched_at", f.s.now().Add(-time.Second), match)
	if err != nil || preview.SelectedFiles != 2 || preview.Match != match {
		t.Fatal(preview, err)
	}
	var raw []byte
	if err = f.sql(t).QueryRow(`SELECT selection_json FROM http_cleanup_previews WHERE id=?`, preview.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var frozen PreviewCriteria
	if err = json.Unmarshal(raw, &frozen); err != nil || frozen.Match != match {
		t.Fatal(frozen, err)
	}
	config := cachepolicy.Empty()
	config.Rules = []cachepolicy.CacheRule{{Match: pathmatch.Spec{Type: "glob", Pattern: "/"}, TTLSeconds: 300}}
	setPolicy(t, f, config)
	if _, err = f.s.ExecuteCleanup(context.Background(), f.entry, preview.ID); !errors.Is(err, store.ErrSourceInactive) {
		t.Fatal("policy edit did not fence old preview", err)
	}
	preview, err = f.s.PreviewCleanup(context.Background(), f.entry, "fetched_at", f.s.now().Add(-time.Second), pathmatch.Spec{Type: "re2", Pattern: `/reports/[a-z]+\.txt`})
	if err != nil || preview.SelectedFiles != 1 {
		t.Fatal("full regex selected directory descendants", preview, err)
	}
	if _, err = f.s.PreviewCleanup(context.Background(), f.entry, "fetched_at", f.s.now(), pathmatch.Spec{Type: "glob", Pattern: "["}); !errors.Is(err, ErrInvalidCleanup) {
		t.Fatal("invalid match accepted", err)
	}
}

func TestAutomaticFirstMatchAndAccessRecheck(t *testing.T) {
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "body") }), 10000)
	if _, err := f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(3600)
	config := cachepolicy.Empty()
	config.AutoCleanup = []cachepolicy.CleanupRule{
		{Match: pathmatch.Spec{Type: "glob", Pattern: "/file"}, Basis: "fetched_at", AgeSeconds: 86400},
		{Match: pathmatch.Spec{Type: "glob", Pattern: "/"}, Basis: "fetched_at", AgeSeconds: 60},
	}
	setPolicy(t, f, config)
	registry := registryFor(t, f.entry)
	f.s.cleanupPass(context.Background(), registry, func(err error) { t.Error(err) })
	if len(f.rows(t)) != 1 || f.s.CleanupStatus().RetiredFiles != 0 {
		t.Fatal("later eligible rule overrode first path match")
	}
	config.AutoCleanup = []cachepolicy.CleanupRule{{Match: pathmatch.Spec{Type: "glob", Pattern: "/"}, Basis: "last_access", AgeSeconds: 60}}
	setPolicy(t, f, config)
	policy, err := f.s.readPolicy(f.entry)
	if err != nil {
		t.Fatal(err)
	}
	id, scanned, _, err := f.s.previewAutomatic(context.Background(), f.entry, policy, 0)
	if err != nil || id == "" || scanned != 1 {
		t.Fatal(id, scanned, err)
	}
	if _, err = f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	result, err := f.s.ExecuteCleanup(context.Background(), f.entry, id)
	if err != nil || result.SkippedAccessed != 1 || result.RetiredFiles != 0 {
		t.Fatal("recent access lost", result, err)
	}
	// Inactive sources can still be cleaned manually, but never by automation.
	id, _, _, err = f.s.previewAutomatic(context.Background(), f.entry, policy, 0)
	if err != nil {
		t.Fatal(err)
	}
	if id != "" {
		t.Fatal("recent access selected again")
	}
	f.clock.Add(300)
	id, _, _, err = f.s.previewAutomatic(context.Background(), f.entry, policy, 0)
	if err != nil || id == "" {
		t.Fatal(id, err)
	}
	changed, err := f.db.UpdateApplication(f.app.Key, f.entry.Revision, store.ApplicationChanges{Name: f.app.Name, BaseURL: f.app.BaseURL, CacheTTLSeconds: 10000, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	f.entry.Revision = changed.Revision
	f.entry.RuntimeRevision = changed.RuntimeRevision
	f.entry.Enabled = false
	if _, err = f.s.ExecuteCleanup(context.Background(), f.entry, id); !errors.Is(err, store.ErrSourceInactive) {
		t.Fatal("disabled source auto cleaned", err)
	}
	f.s.cleanupPass(context.Background(), registryFor(t, f.entry), func(err error) { t.Error(err) })
	if len(f.rows(t)) != 1 || f.s.CleanupStatus().ConfiguredApps != 0 {
		t.Fatal("disabled app entered automatic pass")
	}
}

func TestAutomaticBoundsCursorAndEmptyDefault(t *testing.T) {
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "unused") }), 300)
	tx, err := f.sql(t).Begin()
	if err != nil {
		t.Fatal(err)
	}
	old := f.s.now().Add(-time.Hour).Unix()
	for i := 0; i < 1120; i++ {
		path := fmt.Sprintf("aaa/%04d", i)
		if i >= 1000 {
			path = fmt.Sprintf("zzz/%04d", i-1000)
		}
		_, err = tx.Exec(`INSERT INTO http_cache_generations(id,storage_id,path,sha256,size_bytes,fetched_at_s,validated_at_s,last_access_bucket_s,fresh_until_s,headers_json,is_current) VALUES(?,?,?,?,0,?,?,0,?,'{}',1)`, testID(t), f.entry.StorageID(), path, strings.Repeat("a", 64), old, old, old)
		if err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	f.s.cleanupPass(context.Background(), registryFor(t, f.entry), func(err error) { t.Error(err) })
	if len(f.rows(t)) != 1120 || f.s.CleanupStatus().ScannedFiles != 0 {
		t.Fatal("empty default selected data")
	}
	config := cachepolicy.Empty()
	config.AutoCleanup = []cachepolicy.CleanupRule{{Match: pathmatch.Spec{Type: "glob", Pattern: "/zzz/"}, Basis: "fetched_at", AgeSeconds: 60}}
	setPolicy(t, f, config)
	registry := registryFor(t, f.entry)
	f.s.cleanupPass(context.Background(), registry, func(err error) { t.Error(err) })
	status := f.s.CleanupStatus()
	if status.ScannedFiles != 1000 || status.RetiredFiles != 0 {
		t.Fatal("scan bound changed", status)
	}
	f.s.cleanupPass(context.Background(), registry, func(err error) { t.Error(err) })
	status = f.s.CleanupStatus()
	if status.ScannedFiles != 100 || status.RetiredFiles != 100 {
		t.Fatal("cursor or retire limit failed", status)
	}
	f.s.cleanupPass(context.Background(), registry, func(err error) { t.Error(err) })
	status = f.s.CleanupStatus()
	if status.RetiredFiles != 20 || len(f.rows(t)) != 1000 {
		t.Fatal("tail starved after page cap", status)
	}
}

func TestAutomaticWaitsForIntervalAndCancelsWithClose(t *testing.T) {
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "body") }), 300)
	if _, err := f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(3600)
	config := cachepolicy.Empty()
	config.AutoCleanup = []cachepolicy.CleanupRule{{Match: pathmatch.Spec{Type: "glob", Pattern: "/"}, Basis: "fetched_at", AgeSeconds: 60}}
	setPolicy(t, f, config)
	registry := registryFor(t, f.entry)
	done := make(chan struct{})
	go func() {
		f.s.RunCleanup(context.Background(), registry, func(err error) { t.Error(err) })
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for !f.s.CleanupStatus().Running && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if !f.s.CleanupStatus().Running {
		t.Fatal("scheduler did not start")
	}
	if f.s.CleanupStatus().PassesTotal != 0 || len(f.rows(t)) != 1 {
		t.Fatal("startup immediately deleted cached data")
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop with service")
	}
	if f.s.CleanupStatus().Running {
		t.Fatal("scheduler stayed running after shutdown")
	}
}
