package download

import (
	"bytes"
	"context"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestTwoApplicationsShareLimitsAndKeepCleanupSeparate(t *testing.T) {
	payload := bytes.Repeat([]byte("same version and asset, separate origins"), 1000)
	var counts [2]atomic.Int32
	dir := t.TempDir()
	db := openStore(t, dir)
	clients := map[string]*distributor.Client{}
	for i, id := range []string{testApp, otherApp} {
		idx := i
		client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { counts[idx].Add(1); w.Write(payload) }))
		clients[id] = client
	}
	defer db.Close()
	m, e := NewApplications(dir, db, clients)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	resources := map[string]Resource{}
	for id, c := range clients {
		r := onApp(resource(c, payload), id)
		authorize(t, m, r)
		resources[id] = r
	}
	var wg sync.WaitGroup
	for id := range clients {
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				r, _, e := m.Acquire(context.Background(), resources[id])
				if e != nil {
					t.Error(e)
					return
				}
				defer r.Close()
				b, e := io.ReadAll(r)
				if e != nil || !bytes.Equal(b, payload) {
					t.Errorf("%s payload: %v", id, e)
				}
			}(id)
		}
	}
	wg.Wait()
	if counts[0].Load() != 1 || counts[1].Load() != 1 {
		t.Fatal("per-application shared download failed")
	}
	if resources[testApp].ID == resources[otherApp].ID {
		t.Fatal("identities collided")
	}
	wrong := resources[otherApp]
	wrong.Application, wrong.SourceFence = testApp, sourceFences[testApp]
	if r, _, e := m.Acquire(context.Background(), wrong); e == nil {
		r.Close()
		t.Fatal("foreign upstream was served from cache")
	}
	wrong.Application = "app/00000000000000000000000000000000-e1"
	if r, _, e := m.Acquire(context.Background(), wrong); e == nil {
		r.Close()
		t.Fatal("unknown app")
	}
	old, _, e := m.Acquire(context.Background(), resources[testApp])
	if e != nil {
		t.Fatal(e)
	}
	defer old.Close()
	job, e := m.Preview(testApp, map[string]bool{resources[testApp].ID: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Cleanup(testApp, job.ID); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(collect(t, m, resources[otherApp]), payload) || counts[1].Load() != 1 {
		t.Fatal("cleanup affected other app")
	}
	if !bytes.Equal(collect(t, m, resources[testApp]), payload) || counts[0].Load() != 1 {
		t.Fatal("new generation not independent")
	}
	if b, e := io.ReadAll(old); e != nil || !bytes.Equal(b, payload) {
		t.Fatal("old reader failed to drain", e)
	}
	// Global reader limit, not a separate quota for each application.
	m.mu.Lock()
	m.maxReaders = 1
	m.mu.Unlock()
	if r, _, e := m.Acquire(context.Background(), resources[otherApp]); e == nil {
		r.Close()
		t.Fatal("application bypassed global reader limit")
	}
	old.Close()
	m.Close()
	reopened, e := NewApplications(dir, db, clients)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if e = reopened.Cleanup(testApp, job.ID); e != nil {
		t.Fatal("completed cleanup receipt was not idempotent", e)
	}
	for id := range clients {
		if !bytes.Equal(collect(t, reopened, resources[id]), payload) {
			t.Fatal("restart or old snapshot damaged an app", id)
		}
	}
	if counts[0].Load() != 1 || counts[1].Load() != 1 {
		t.Fatal("restart/old cleanup forced valid objects to download again")
	}
}

// One origin and one digest intentionally serve two applications. Neither source
// identity nor display labels may join their authorization or physical cache.
func TestLogicalBindingsReuseWithinApplicationAndSurviveMove(t *testing.T) {
	payload := []byte("same bytes across immutable logical releases")
	var requests atomic.Int32
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.Write(payload) }))
	dir := t.TempDir()
	db := openStore(t, dir)
	clients := map[string]*distributor.Client{testApp: c, otherApp: c}
	m, e := NewApplications(dir, db, clients)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { m.Close(); db.Close() }()
	first := resource(c, payload)
	authorize(t, m, first)
	collect(t, m, first)
	second := first
	second.Version = "0.2.0"
	second.ID = LogicalIdentity(second.Application, second.Version, second.Key)
	authorize(t, m, second)
	rd, hit, e := m.Acquire(context.Background(), second)
	if e != nil {
		t.Fatal(e)
	}
	if !hit || rd.Kind != "cache_hit" {
		t.Fatal("verified same-application blob was not reused")
	}
	io.Copy(io.Discard, rd)
	rd.Close()
	foreign := onApp(first, otherApp)
	authorize(t, m, foreign)
	collect(t, m, foreign)
	if requests.Load() != 2 {
		t.Fatalf("requests=%d, want two application-owned downloads", requests.Load())
	}
	if m.current[first.ID].Path != m.current[second.ID].Path || m.current[first.ID].Path == m.current[foreign.ID].Path {
		t.Fatal("incorrect physical blob scope")
	}
	forged := first
	forged.Source = testutil.SourceURL(c, "unapproved")
	if rd, _, e = m.Acquire(context.Background(), forged); e == nil {
		rd.Close()
		t.Fatal("changed source bypassed persisted authorization")
	}
	forged = first
	forged.Labels = map[string]string{"app": "anthropic/claude-code"}
	if !bytes.Equal(collect(t, m, forged), payload) {
		t.Fatal("display labels changed ownership")
	}
	// A different logical head keeps a lease on a subsequently corrupted blob.
	// Repair must fail that reader explicitly and preserve the foreign application.
	old, _, e := m.Acquire(context.Background(), second)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Truncate(m.current[first.ID].Path, 3); e != nil {
		old.Close()
		t.Fatal(e)
	}
	if !bytes.Equal(collect(t, m, first), payload) {
		old.Close()
		t.Fatal("shared corrupt blob could not be repaired online")
	}
	if _, e = io.ReadAll(old); e == nil {
		old.Close()
		t.Fatal("old corrupt inode was reported as verified")
	}
	old.Close()
	collect(t, m, second)
	collect(t, m, foreign)
	if requests.Load() != 3 {
		t.Fatal("repair redownloaded another application or could not reuse repaired content", requests.Load())
	}
	preview, e := m.Preview(testApp, map[string]bool{first.ID: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if summary, _ := store.ReleaseSummary(preview); preview.SelectedBytes != int64(len(payload)) || summary.ReclaimableBytes != 0 {
		t.Fatal("shared-blob reclamation estimate is incorrect", preview)
	}
	if e = m.Cleanup(foreign.Application, preview.ID); e == nil {
		t.Fatal("cross-application cleanup was accepted")
	}
	if e = m.Cleanup(testApp, preview.ID); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(collect(t, m, second), payload) || !bytes.Equal(collect(t, m, foreign), payload) {
		t.Fatal("cleanup removed another logical resource")
	}
	global, _ := db.Counters()
	app, _ := db.CountersFor(metricsIDs[testApp])
	other, _ := db.CountersFor(metricsIDs[otherApp])
	if global["upstream_bytes"] != 3*int64(len(payload)) || app["upstream_bytes"] != 2*int64(len(payload)) || other["upstream_bytes"] != int64(len(payload)) {
		t.Fatal("global/application traffic counters diverged", global, app, other)
	}
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	moved := filepath.Join(t.TempDir(), "relocated")
	if e = os.Rename(dir, moved); e != nil {
		t.Fatal(e)
	}
	db = openStore(t, moved)
	m, e = NewApplications(moved, db, clients)
	if e != nil {
		t.Fatal(e)
	}
	collect(t, m, second)
	collect(t, m, foreign)
	if e = m.Cleanup(testApp, preview.ID); e != nil {
		t.Fatal("cleanup receipt did not survive restart", e)
	}
	if requests.Load() != 3 {
		t.Fatal("moving data directory invalidated a verified blob")
	}
}

func TestEqualDigestsDoNotMergeActiveLogicalWriters(t *testing.T) {
	payload := []byte("complete content reuse starts only after verification")
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Write(payload)
	}))
	m, _, _ := setup(t, c)
	a := authorizedResource(t, m, c, payload)
	b := a
	b.Version = "0.2.0"
	b.ID = LogicalIdentity(b.Application, b.Version, b.Key)
	authorize(t, m, b)
	ra, _, e := m.Acquire(context.Background(), a)
	if e != nil {
		t.Fatal(e)
	}
	defer ra.Close()
	rb, _, e := m.Acquire(context.Background(), b)
	if e != nil {
		t.Fatal(e)
	}
	defer rb.Close()
	// Both origins must be contacted while neither response can complete.
	await(t, started)
	await(t, started)
	if ra.g.ID == rb.g.ID {
		t.Fatal("active logical resources were coalesced by digest")
	}
	releaseOnce.Do(func() { close(release) })
	for _, reader := range []*Reader{ra, rb} {
		got, e := io.ReadAll(reader)
		if e != nil || !bytes.Equal(got, payload) {
			t.Fatal("logical writer did not finish", e)
		}
	}
	if ra.g.Path != rb.g.Path {
		t.Fatal("completed same-app blobs did not converge after verification")
	}
}
