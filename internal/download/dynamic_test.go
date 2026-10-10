package download

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
)

func dynamicApplication(t *testing.T, db *store.Store, client *distributor.Client) store.Application {
	t.Helper()
	name := store.LocalizedText{En: "Example", ZhCN: "示例"}
	vendor, err := db.CreateVendor(store.VendorInput{ID: "example", Name: name, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	app, err := db.CreateApplication(vendor.ID, store.ApplicationInput{ID: "tool", Name: name, Provider: "codex", BaseURL: client.Base.String(), CacheTTLSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func dynamicResource(t *testing.T, db *store.Store, app store.Application, client *distributor.Client, payload []byte) Resource {
	t.Helper()
	source, err := db.Source(app.StorageID())
	if err != nil {
		t.Fatal(err)
	}
	r := resource(client, payload)
	r.Application, r.MetricsID, r.SourceFence = app.StorageID(), app.MetricsID(), source.Fence()
	r.ID = LogicalIdentity(r.Application, r.Version, r.Key)
	return r
}

func appChanges(a store.Application) store.ApplicationChanges {
	return store.ApplicationChanges{Name: a.Name, Description: a.Description, Icon: a.Icon, BaseURL: a.BaseURL, CacheTTLSeconds: a.CacheTTLSeconds, Enabled: a.Enabled}
}

func TestDynamicEpochRecoveryPreservesHistoricalDataAndMetrics(t *testing.T) {
	payload := []byte("verified immutable artifact across two source epochs")
	var requests atomic.Int32
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.Write(payload) }))
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// An empty installation starts before its first dynamic source is registered.
	m, err := NewApplications(dir, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { m.Close() }()
	app := dynamicApplication(t, db, client)
	clients := map[string]*distributor.Client{app.StorageID(): client}
	if err = m.RegisterUpstreams(clients); err != nil {
		t.Fatal(err)
	}
	first := dynamicResource(t, db, app, client, payload)
	authorize(t, m, first)
	collect(t, m, first)
	// Editing presentation fences writers, but already verified immutable content
	// remains reusable without rewriting generations or fetching it again.
	changes := appChanges(app)
	changes.Name.En = "Renamed display label"
	app, err = db.UpdateApplication(app.Key, app.Revision, changes)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = NewApplications(dir, db, clients)
	if err != nil {
		t.Fatal(err)
	}
	freshAdmission := dynamicResource(t, db, app, client, payload)
	collect(t, m, freshAdmission)
	if requests.Load() != 1 || len(m.Snapshot()) != 1 {
		t.Fatal("presentation revision discarded verified immutable content")
	}
	changes = appChanges(app)
	changes.BaseURL += "/replacement"
	app, err = db.UpdateApplication(app.Key, app.Revision, changes)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse(app.BaseURL)
	secondClient := &distributor.Client{Base: base, HTTP: client.HTTP}
	clients[app.StorageID()] = secondClient
	if err = m.RegisterUpstreams(clients); err != nil {
		t.Fatal(err)
	}
	second := dynamicResource(t, db, app, secondClient, payload)
	authorize(t, m, second)
	collect(t, m, second)
	if first.ID == second.ID || requests.Load() != 2 {
		t.Fatal("source namespaces reused a previous epoch's head")
	}
	counters, err := db.CountersFor(app.MetricsID())
	if err != nil || counters["upstream_bytes"] != 2*int64(len(payload)) {
		t.Fatalf("epoch metrics split: %v %v", counters, err)
	}
	if reader, _, err := m.Acquire(context.Background(), first); !errors.Is(err, store.ErrSourceInactive) {
		if reader != nil {
			reader.Close()
		}
		t.Fatalf("inactive source admitted: %v", err)
	}
	for _, component := range []string{"icons", "http"} {
		path := filepath.Join(dir, "objects", component)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "owned-by-another-component"), payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.DeleteApplication(app.Key, app.Revision); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = NewApplications(dir, db, clients)
	if err != nil {
		t.Fatal(err)
	}
	for _, component := range []string{"icons", "http"} {
		got, err := os.ReadFile(filepath.Join(dir, "objects", component, "owned-by-another-component"))
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("release recovery changed %s-owned data: %v", component, err)
		}
	}
	views := m.Snapshot()
	if len(views) != 2 || requests.Load() != 2 {
		t.Fatalf("tombstoned recovery deleted or fetched data: views=%d requests=%d", len(views), requests.Load())
	}
	for _, view := range views {
		if view.Current || view.ActiveWriter {
			t.Fatal("inactive recovered source exposed an active head")
		}
		if _, err := os.Stat(view.Path); err != nil {
			t.Fatalf("historical cache file removed: %v", err)
		}
	}
	// Explicit historical cleanup is allowed while disabled/tombstoned, but a
	// vendor edit after preview requires a new preview even in that state.
	job, err := m.Preview(first.Application, map[string]bool{first.ID: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	vendor, err := db.Vendor("example")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.UpdateVendor(vendor.ID, vendor.Revision, store.VendorChanges{Name: vendor.Name, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Cleanup(first.Application, job.ID); !errors.Is(err, store.ErrPreviewStale) {
		t.Fatalf("stale cleanup preview accepted: %v", err)
	}
	job, err = m.Preview(first.Application, map[string]bool{first.ID: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Cleanup(first.Application, job.ID); err != nil {
		t.Fatal(err)
	}
	if len(m.Snapshot()) != 1 {
		t.Fatal("explicit cleanup affected a different historical source")
	}
}

func TestFencedWriterDrainsAdmittedReaderWithoutPublishing(t *testing.T) {
	payload := bytes.Repeat([]byte("old writer can finish its admitted reader"), 200)
	ready, finish := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.Write(payload[:1])
			w.(http.Flusher).Flush()
			close(ready)
			<-finish
			w.Write(payload[1:])
		} else {
			w.Write(payload)
		}
	}))
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := dynamicApplication(t, db, client)
	m, err := NewApplications(dir, db, map[string]*distributor.Client{app.StorageID(): client})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	old := dynamicResource(t, db, app, client, payload)
	authorize(t, m, old)
	reader, _, err := m.Acquire(context.Background(), old)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	<-ready
	changes := appChanges(app)
	changes.Enabled = false
	app, err = db.UpdateApplication(app.Key, app.Revision, changes)
	if err != nil {
		t.Fatal(err)
	}
	changes.Enabled = true
	app, err = db.UpdateApplication(app.Key, app.Revision, changes)
	if err != nil {
		t.Fatal(err)
	}
	close(finish)
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("admitted reader failed after disable/enable: %v", err)
	}
	for _, view := range m.Snapshot() {
		if view.Current || !view.Retired {
			t.Fatal("old writer published under new admission revision")
		}
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	fresh := dynamicResource(t, db, app, client, payload)
	if got := collect(t, m, fresh); !bytes.Equal(got, payload) {
		t.Fatal("new admission could not fetch")
	}
	if requests.Load() != 2 {
		t.Fatal("old writer became the new revision's cache head")
	}
}

func TestDormantCacheIsReverifiedWhenApplicationIsReenabled(t *testing.T) {
	payload := []byte("valid immutable bytes before disabling this application")
	var requests atomic.Int32
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.Write(payload) }))
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := dynamicApplication(t, db, client)
	clients := map[string]*distributor.Client{app.StorageID(): client}
	m, err := NewApplications(dir, db, clients)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { m.Close() }()
	r := dynamicResource(t, db, app, client, payload)
	authorize(t, m, r)
	collect(t, m, r)
	path := m.Snapshot()[0].Path
	changes := appChanges(app)
	changes.Enabled = false
	app, err = db.UpdateApplication(app.Key, app.Revision, changes)
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	// Same-length corruption cannot be caught by the fast cache-hit size check.
	if err = os.WriteFile(path, bytes.Repeat([]byte("x"), len(payload)), 0600); err != nil {
		t.Fatal(err)
	}
	m, err = NewApplications(dir, db, clients)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Snapshot()) != 1 {
		t.Fatal("inactive recovery deleted retained data")
	}
	changes.Enabled = true
	app, err = db.UpdateApplication(app.Key, app.Revision, changes)
	if err != nil {
		t.Fatal(err)
	}
	fresh := dynamicResource(t, db, app, client, payload)
	if got := collect(t, m, fresh); !bytes.Equal(got, payload) {
		t.Fatal("dormant corrupted content was served after enabling")
	}
	views := m.Snapshot()
	if requests.Load() != 2 || len(views) != 1 || !views[0].Current {
		t.Fatalf("dormant cache repair did not establish a valid head: requests=%d views=%v", requests.Load(), views)
	}
}

func TestPermanentDeletionCancelsReleaseAcrossOldEpochAndVerification(t *testing.T) {
	for _, stage := range []string{"transfer", "verify"} {
		t.Run(stage, func(t *testing.T) {
			payload := []byte("immutable verified bytes")
			started := make(chan struct{})
			client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if stage == "transfer" {
					w.Write(payload[:1])
					w.(http.Flusher).Flush()
					close(started)
					<-r.Context().Done()
					return
				}
				w.Write(payload)
			}))
			dir := t.TempDir()
			db, err := store.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			app := dynamicApplication(t, db, client)
			hook := &faultHook{}
			m, err := NewApplications(dir, db, map[string]*distributor.Client{app.StorageID(): client}, withTrace(hook.trace))
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if stage == "verify" {
				hook.set(func(point string, g *Generation) {
					if point == "download.before_verify" {
						close(started)
						<-g.ctx.Done()
					}
				})
			}
			r := dynamicResource(t, db, app, client, payload)
			authorize(t, m, r)
			reader, _, err := m.Acquire(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := io.Copy(io.Discard, reader); reader.Close(); done <- err }()
			<-started
			changes := appChanges(app)
			changes.BaseURL += "/new-source"
			app, err = db.UpdateApplication(app.Key, app.Revision, changes)
			if err != nil {
				t.Fatal(err)
			}
			if app.StorageID() == r.Application {
				t.Fatal("fixture did not change epoch")
			}
			uid, drained, err := db.PrepareApplicationDeletion(app.Key, app.Revision)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-drained:
			case <-time.After(2 * time.Second):
				t.Fatal("release did not drain")
			}
			if err = <-done; err == nil {
				t.Fatal("canceled release succeeded")
			}
			if err = db.FinishApplicationDeletion(uid, func(remove func() error) error { return m.PurgeApplication(uid, remove) }); err != nil {
				t.Fatal(err)
			}
			if err = db.ProcessPendingDeletes(dir); err != nil {
				t.Fatal(err)
			}
			if _, _, err = m.Acquire(context.Background(), r); err == nil {
				t.Fatal("old epoch restarted")
			}
			if len(m.Snapshot()) != 0 {
				t.Fatal("generation survived purge")
			}
		})
	}
}
