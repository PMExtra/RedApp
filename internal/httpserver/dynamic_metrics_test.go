package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/history"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
)

func TestDynamicMetricsAndListsKeepStorageAndPublicNamespacesSeparate(t *testing.T) {
	payload := []byte("dynamic scope fixture")
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(payload) }))
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	vendor, err := db.CreateVendor(store.VendorInput{ID: "publisher", Name: store.LocalizedText{En: "Publisher", ZhCN: "发布者"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	makeApp := func(id, provider string) store.Application {
		t.Helper()
		a, err := db.CreateApplication(vendor.ID, store.ApplicationInput{ID: id, Name: store.LocalizedText{En: id, ZhCN: id}, Provider: provider, BaseURL: client.Base.String(), CacheTTLSeconds: 60, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	app := makeApp("codex", "codex")
	other := makeApp("claude", "claude-code")
	general := makeApp("files", "http-cache")
	pool := distributor.NewPool()
	s := &Server{store: db, dataDir: dir, started: time.Now()}
	reload := func() {
		t.Helper()
		vendors, e := db.Vendors(true)
		if e != nil {
			t.Fatal(e)
		}
		apps, e := db.Applications(true)
		if e != nil {
			t.Fatal(e)
		}
		s.registry, e = builtin.NewDynamic(vendors, apps, pool)
		if e != nil {
			t.Fatal(e)
		}
	}
	reload()
	s.downloads, err = download.NewApplications(dir, db, map[string]*distributor.Client{app.StorageID(): client})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.downloads.Close() })
	cache := func(a store.Application, c *distributor.Client, version string) {
		t.Helper()
		sum := sha256.Sum256(payload)
		hash := hex.EncodeToString(sum[:])
		size := int64(len(payload))
		source, e := db.Source(a.StorageID())
		if e != nil {
			t.Fatal(e)
		}
		bound := store.Resource{AppID: a.StorageID(), Version: version, Key: "artifact", SourceURL: testutil.SourceURL(c, "artifact"), SHA256: hash, ExpectedSize: &size}
		if e = db.PutRelease(store.ReleaseMetadata{AppID: a.StorageID(), Version: version, Raw: []byte("{}"), TrustRevision: 1, FetchedAt: time.Now()}, []store.Resource{bound}, source.Fence()); e != nil {
			t.Fatal(e)
		}
		r := download.Resource{Application: a.StorageID(), MetricsID: a.MetricsID(), SourceFence: source.Fence(), Version: version, Key: bound.Key, Source: bound.SourceURL, Hash: hash, Size: &size, ID: download.LogicalIdentity(a.StorageID(), version, bound.Key)}
		reader, _, e := s.downloads.Acquire(context.Background(), r)
		if e != nil {
			t.Fatal(e)
		}
		_, e = io.Copy(io.Discard, reader)
		reader.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
	cache(app, client, "1.0.0")
	if err = db.SeenFor(app.StorageID(), "1.0.1"); err != nil {
		t.Fatal(err)
	}
	oldStorage := app.StorageID()
	app, err = db.UpdateApplication(app.Key, app.Revision, store.ApplicationChanges{Name: app.Name, Description: app.Description, Icon: app.Icon, BaseURL: app.BaseURL + "/mirror", CacheTTLSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	base, err := url.Parse(app.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	replacement := &distributor.Client{Base: base, HTTP: client.HTTP}
	if err = s.downloads.RegisterUpstreams(map[string]*distributor.Client{app.StorageID(): replacement}); err != nil {
		t.Fatal(err)
	}
	reload()
	cache(app, replacement, "2.0.0")
	for _, ownerVersion := range [][2]string{{app.StorageID(), "2.0.1"}, {other.StorageID(), "3.0.0"}, {general.StorageID(), "must-not-be-a-general-version"}} {
		if err = db.SeenFor(ownerVersion[0], ownerVersion[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.AddFor(app.MetricsID(), "artifact_requests", 7); err != nil {
		t.Fatal(err)
	}
	if err = db.AddFor(other.MetricsID(), "artifact_requests", 11); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{app.MetricsID(), other.MetricsID()} {
		if err = db.RecordEvent(store.Event{AppID: owner, Category: "metadata", Code: "metadata_fetch_failed", Message: "fixture"}); err != nil {
			t.Fatal(err)
		}
	}
	metricValue := func(metrics []history.Metric, key string) *float64 {
		t.Helper()
		for _, metric := range metrics {
			if metric.Key == key {
				return metric.Value
			}
		}
		t.Fatalf("metric %s missing", key)
		return nil
	}
	_, global, err := s.globalMetrics()
	if err != nil {
		t.Fatal(err)
	}
	if versions := metricValue(global, "versions.total"); versions == nil || *versions != 3 {
		t.Fatal("global versions omitted a provider or counted old epochs", versions)
	}
	entry, _ := s.registry.LookupAny(app.Key)
	appMetrics, err := s.appMetrics(entry)
	if err != nil {
		t.Fatal(err)
	}
	if total, requests, versions := metricValue(appMetrics, "resources.total"), metricValue(appMetrics, "counters.artifact_requests"), metricValue(appMetrics, "versions.total"); *total != 2 || *requests != 7 || *versions != 2 {
		t.Fatal("app metrics did not aggregate the stable UID across epochs", *total, *requests, *versions)
	}
	originals := s.downloads.Snapshot()
	if len(originals) != 2 {
		t.Fatal(originals)
	}
	for _, view := range originals {
		if view.Resource.Application != oldStorage && view.Resource.Application != app.StorageID() {
			t.Fatal("response mapping mutated manager", view.Resource)
		}
		if view.Resource.MetricScope() != app.MetricsID() {
			t.Fatal("metric scope changed", view.Resource)
		}
	}
	generalEntry, _ := s.registry.LookupAny(general.Key)
	generalMetrics, err := s.appMetrics(generalEntry)
	if err != nil {
		t.Fatal(err)
	}
	if metricValue(generalMetrics, "versions.total") != nil {
		t.Fatal("GeneralHttp versions metric fabricated a value")
	}
	s.history, err = history.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.SampleHistory(ctx, func(e error) { t.Fatal(e) })
	var value int64
	if err = db.DB.QueryRow("SELECT CAST(value AS INTEGER) FROM metric_samples WHERE scope='app' AND app_id=? AND metric='counters.artifact_requests'", app.MetricsID()).Scan(&value); err != nil || value != 7 {
		t.Fatal("history sampler did not use stable UID", value, err)
	}
	if err = db.DB.QueryRow("SELECT count(*) FROM metric_samples WHERE scope='app' AND (app_id=? OR (app_id=? AND metric='versions.total'))", app.Key, general.MetricsID()).Scan(&value); err != nil || value != 0 {
		t.Fatal("history sampled public namespace or inapplicable metric", value, err)
	}

	// Management remains usable for disabled applications and preserves public
	// labels while current-source pages exclude retained historical cache.
	app, err = db.UpdateApplication(app.Key, app.Revision, store.ApplicationChanges{Name: app.Name, Description: app.Description, Icon: app.Icon, BaseURL: app.BaseURL, CacheTTLSeconds: 60, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	reload()
	entry, _ = s.registry.LookupAny(app.Key)
	if _, err = s.appMetrics(entry); err != nil {
		t.Fatal("disabled status inaccessible", err)
	}
}
