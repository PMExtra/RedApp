package catalog_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/catalog"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
)

func dynamicEntry(t *testing.T, db *store.Store, client *distributor.Client) (application.Entry, store.Application) {
	t.Helper()
	name := store.LocalizedText{En: "Vendor", ZhCN: "厂商"}
	vendor, err := db.CreateVendor(store.VendorInput{ID: "example", Name: name, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	app, err := db.CreateApplication(vendor.ID, store.ApplicationInput{ID: "tool", Name: name, Provider: "codex", BaseURL: client.Base.String(), CacheTTLSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return application.Entry{Descriptor: descriptor(app.Key, "latest"), Protocol: codex.NewProtocol(client), Upstream: client, UID: app.UID, Provider: app.Provider, Revision: app.Revision, VendorRevision: vendor.Revision, RuntimeRevision: app.RuntimeRevision, VendorRuntimeRevision: vendor.RuntimeRevision, SourceEpoch: app.SourceEpoch, Enabled: true}, app
}

// Even when disable/enable returns to the same source epoch, the delayed flight
// must neither recruit a new request nor write its earlier admission to storage.
func TestDynamicMetadataFlightFencesVendorDisableEnable(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var base string
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Write(releaseJSON(base, strings.Repeat("a", 64)))
	}))
	base = client.Base.String()
	db := openStore(t)
	entry, app := dynamicEntry(t, db, client)
	reg := registry(t, entry)
	service := catalog.New(db, reg)
	result := make(chan error, 1)
	go func() { _, err := service.Release(context.Background(), app.Key, "latest"); result <- err }()
	<-started
	vendor, err := db.Vendor("example")
	if err != nil {
		t.Fatal(err)
	}
	vendor, err = db.UpdateVendor(vendor.ID, vendor.Revision, store.VendorChanges{Name: vendor.Name, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	vendor, err = db.UpdateVendor(vendor.ID, vendor.Revision, store.VendorChanges{Name: vendor.Name, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	entry.VendorRevision = vendor.Revision
	entry.VendorRuntimeRevision = vendor.RuntimeRevision
	if err = reg.Replace([]application.Entry{entry}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err = <-result; !errors.Is(err, store.ErrSourceInactive) {
		t.Fatalf("old metadata admission published: %v", err)
	}
	if _, err = db.Release(app.StorageID(), "1.2.3"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("old release persisted: %v", err)
	}
	if _, err = db.Channel(app.StorageID(), "latest"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("old channel persisted: %v", err)
	}
}

func TestHistoricalCleanupCandidatesRequireSourceOwnership(t *testing.T) {
	var upstreamCalls atomic.Int32
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { upstreamCalls.Add(1); w.WriteHeader(503) }))
	db := openStore(t)
	entry, app := dynamicEntry(t, db, client)
	oldStorage := app.StorageID()
	changes := store.ApplicationChanges{Name: app.Name, BaseURL: app.BaseURL + "/next", CacheTTLSeconds: 60, Enabled: false}
	app, err := db.UpdateApplication(app.Key, app.Revision, changes)
	if err != nil {
		t.Fatal(err)
	}
	entry.RuntimeRevision = app.RuntimeRevision
	entry.SourceEpoch, entry.Revision, entry.Enabled = app.SourceEpoch, app.Revision, false
	other, err := db.CreateApplication("example", store.ApplicationInput{ID: "other", Name: app.Name, Provider: "codex", BaseURL: app.BaseURL, CacheTTLSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	reg := registry(t, entry)
	service := catalog.New(db, reg)
	views := []download.View{}
	for _, source := range []string{oldStorage, app.StorageID(), other.StorageID()} {
		r := download.Resource{Application: source, Version: "1.2.3", Key: "asset.tgz", ID: download.LogicalIdentity(source, "1.2.3", "asset.tgz")}
		views = append(views, download.View{Generation: download.Generation{Resource: r}})
	}
	ids, unknown, err := service.CandidatesForSource(app.Key, oldStorage, "2.0.0", views)
	if err != nil || len(unknown) != 0 || len(ids) != 1 || !ids[views[0].Resource.ID] {
		t.Fatalf("historical selection: %v %v %v", ids, unknown, err)
	}
	current, _, err := service.Candidates(app.Key, "2.0.0", views)
	if err != nil || len(current) != 1 || !current[views[1].Resource.ID] {
		t.Fatalf("inactive current source selection: %v %v", current, err)
	}
	if _, _, err = service.CandidatesForSource(app.Key, other.StorageID(), "2.0.0", views); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("foreign source accepted: %v", err)
	}
	if _, _, err = service.CandidatesForSource(app.Key, identity.StorageID(app.UID, 999), "2.0.0", views); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("nonexistent source accepted: %v", err)
	}
	if upstreamCalls.Load() != 0 {
		t.Fatal("management of inactive history made an upstream request")
	}
}

func TestPermanentDeletionCancelsMetadataFlightAndRejectsStaleEntry(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(stopped) }))
	db := openStore(t)
	entry, app := dynamicEntry(t, db, client)
	service := catalog.New(db, registry(t, entry))
	done := make(chan error, 1)
	go func() { _, err := service.Release(context.Background(), app.Key, "latest"); done <- err }()
	<-started
	uid, drained, err := db.PrepareApplicationDeletion(app.Key, app.Revision)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("metadata flight did not drain")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("metadata origin was not canceled")
	}
	if err = <-done; err == nil {
		t.Fatal("canceled flight succeeded")
	}
	if err = db.FinishApplicationDeletion(uid); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Release(context.Background(), app.Key, "latest"); !errors.Is(err, store.ErrSourceInactive) {
		t.Fatal("stale registry admitted metadata", err)
	}
}
