package download

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
)

// verificationBarrier blocks every manager-owned verification before hashing.
type verificationBarrier struct {
	hashes  atomic.Int32
	entered chan struct{}
	release chan struct{}
	waiters chan struct{}
}

func newVerificationBarrier() *verificationBarrier {
	return &verificationBarrier{entered: make(chan struct{}, 8), release: make(chan struct{}), waiters: make(chan struct{}, 8)}
}

func (b *verificationBarrier) fault(point string, _ *Generation) {
	switch point {
	case "verification.before_hash":
		b.hashes.Add(1)
		b.entered <- struct{}{}
		<-b.release
	case "acquire.wait_verification":
		b.waiters <- struct{}{}
	}
}

// dormantFixture leaves one complete, dormant head after disabling,
// restarting and re-enabling its dynamic application.
func dormantFixture(t *testing.T, payload []byte, requests *atomic.Int32, fault func(string, *Generation)) (*Manager, Resource, string) {
	t.Helper()
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.Write(payload) }))
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	app := dynamicApplication(t, db, client)
	clients := map[string]*distributor.Client{app.StorageID(): client}
	m, err := NewApplications(dir, db, clients)
	if err != nil {
		t.Fatal(err)
	}
	r := dynamicResource(t, db, app, client, payload)
	authorize(t, m, r)
	collect(t, m, r)
	path := m.Snapshot()[0].Path
	changes := appChanges(app)
	changes.Enabled = false
	if app, err = db.UpdateApplication(app.Key, app.Revision, changes); err != nil {
		t.Fatal(err)
	}
	m.Close()
	hook := &faultHook{}
	if m, err = NewApplications(dir, db, clients, withTrace(hook.trace)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	if fault != nil {
		hook.set(fault)
	}
	changes.Enabled = true
	if app, err = db.UpdateApplication(app.Key, app.Revision, changes); err != nil {
		t.Fatal(err)
	}
	return m, dynamicResource(t, db, app, client, payload), path
}

func TestCancelledWaiterDoesNotRetireDormantCache(t *testing.T) {
	payload := []byte("valid dormant bytes that must survive a disconnected client")
	var requests atomic.Int32
	barrier := newVerificationBarrier()
	m, r, path := dormantFixture(t, payload, &requests, barrier.fault)
	ctx, cancel := context.WithCancel(context.Background())
	acquired := make(chan error, 1)
	go func() {
		rd, _, err := m.Acquire(ctx, r)
		if err == nil {
			rd.Close()
		}
		acquired <- err
	}()
	await(t, barrier.entered)
	cancel()
	select {
	case err := <-acquired:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled waiter returned an unexpected result", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled waiter kept waiting for verification")
	}
	close(barrier.release)
	if got := collect(t, m, r); !bytes.Equal(got, payload) {
		t.Fatal("dormant cache content changed")
	}
	if _, err := os.Stat(path); err != nil || requests.Load() != 1 || barrier.hashes.Load() != 1 {
		t.Fatalf("valid dormant blob was retired or re-downloaded: stat=%v requests=%d hashes=%d", err, requests.Load(), barrier.hashes.Load())
	}
	for _, view := range m.Snapshot() {
		if view.Retired || !view.Current || view.State != "complete" {
			t.Fatal("dormant head was not re-admitted", view)
		}
	}
}

func TestConcurrentVerificationRunsOnceWithoutBlockingOtherResources(t *testing.T) {
	payload := []byte("shared immutable artifact")
	other := []byte("unrelated artifact")
	var mu sync.Mutex
	requests := map[string]int{}
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/other") {
			w.Write(other)
			return
		}
		w.Write(payload)
	}))
	hook := &faultHook{}
	m, _, _ := setup(t, c, withTrace(hook.trace))
	first := authorizedResource(t, m, c, payload)
	collect(t, m, first)
	// A second logical resource with the same digest reuses the published blob,
	// which must be verified before it is trusted.
	reuse := first
	reuse.Version = "0.2.0"
	reuse.ID = LogicalIdentity(reuse.Application, reuse.Version, reuse.Key)
	authorize(t, m, reuse)
	unrelated := resource(c, other)
	unrelated.Source, unrelated.Version = testutil.SourceURL(c, "other"), "0.3.0"
	unrelated.ID = LogicalIdentity(unrelated.Application, unrelated.Version, unrelated.Key)
	authorize(t, m, unrelated)

	barrier := newVerificationBarrier()
	hook.set(barrier.fault)
	const waiters = 3
	results := make(chan []byte, waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			rd, _, err := m.Acquire(context.Background(), reuse)
			if err != nil {
				results <- nil
				return
			}
			defer rd.Close()
			b, _ := io.ReadAll(rd)
			results <- b
		}()
	}
	await(t, barrier.entered)
	for i := 0; i < waiters; i++ {
		await(t, barrier.waiters)
	}
	// The verification is blocked mid-flight; unrelated work still proceeds.
	if got := collect(t, m, unrelated); !bytes.Equal(got, other) {
		t.Fatal("unrelated resource was not served during verification")
	}
	if len(m.Snapshot()) == 0 {
		t.Fatal("snapshot unavailable during verification")
	}
	close(barrier.release)
	for i := 0; i < waiters; i++ {
		if got := <-results; !bytes.Equal(got, payload) {
			t.Fatal("waiter did not receive verified content", got)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if barrier.hashes.Load() != 1 || requests["/asset"] != 1 {
		t.Fatalf("verification or download duplicated: hashes=%d requests=%v", barrier.hashes.Load(), requests)
	}
}
