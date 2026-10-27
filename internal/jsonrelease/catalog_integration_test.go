package jsonrelease_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/catalog"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/jsonrelease"
	"github.com/PMExtra/RedApp/internal/prewarm"
	"github.com/PMExtra/RedApp/internal/releaseconfig"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/store/storetest"
	"github.com/PMExtra/RedApp/internal/warmplan"
)

func TestCatalogRefreshAndPrecisePrewarmUseOneSnapshot(t *testing.T) {
	ctx := context.Background()
	body := []byte("isolated installer fixture")
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	var failed atomic.Bool
	var origin string
	var downloaded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metadata":
			if failed.Load() {
				w.WriteHeader(503)
				return
			}
			fmt.Fprintf(w, `{"version":"1.2.3","build":"abc","artifacts":[{"key":"windows-x64-user","os":"windows","arch":"x64","format":"exe","scope":"user","url":%q,"sha256":%q,"size":%d}]}`, origin+"/files/setup.exe", hash, len(body))
		case "/files/setup.exe":
			downloaded.Add(1)
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			w.Write(body)
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	origin = upstream.URL
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	vendor, err := db.CreateVendor(store.VendorInput{ID: "fixture", Name: store.LocalizedText{En: "Fixture", ZhCN: "测试"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	config := releaseconfig.Config{DefaultChannel: "stable", RefreshSeconds: 60, Channels: map[string]releaseconfig.Channel{"stable": {Query: ".body", Requests: []releaseconfig.Request{{URL: origin + "/metadata"}}}}, Sources: releaseconfig.Sources{Metadata: []releaseconfig.Source{{Origin: origin, Prefix: "/"}}, Artifacts: []releaseconfig.Source{{Origin: origin, Prefix: "/files/"}}}, Prewarm: []releaseconfig.PrewarmSelection{{Channel: "stable", Keys: []string{"windows-x64-user"}}}}
	app, err := db.CreateApplication(vendor.ID, store.ApplicationInput{ID: "editor", Name: vendor.Name, Provider: application.JSONRelease, JSONRelease: &config, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	client, err := distributor.NewPool().NewClient(origin, distributor.ConfiguredRelease)
	if err != nil {
		t.Fatal(err)
	}
	protocol, err := jsonrelease.NewProtocol(config, jsonrelease.NewDiscoverer(jsonrelease.NewRunner(), client))
	if err != nil {
		t.Fatal(err)
	}
	artifactClient, err := client.WithReleaseSources(func(raw string) bool { return releaseconfig.Allows(config.Sources.Artifacts, raw) })
	if err != nil {
		t.Fatal(err)
	}
	entry := application.Entry{UID: app.UID, Revision: app.Revision, VendorRevision: vendor.Revision, RuntimeRevision: app.RuntimeRevision, VendorRuntimeRevision: vendor.RuntimeRevision, SourceEpoch: app.SourceEpoch, Enabled: true, Provider: application.JSONRelease, Protocol: protocol, Upstream: artifactClient, Descriptor: application.Descriptor{ID: app.Key, TrustRevision: 1, DefaultChannelTTLSeconds: 60, Channels: []string{"latest", "stable"}}}
	registry, err := application.NewRegistry([]application.Entry{entry})
	if err != nil {
		t.Fatal(err)
	}
	service := catalog.New(db, registry)
	first, err := service.Release(ctx, app.Key, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if downloaded.Load() != 0 {
		t.Fatal("discovery downloaded installer")
	}
	channel, err := db.Channel(app.StorageID(), "stable")
	if err != nil || channel.Version != first.Version {
		t.Fatal(channel, err)
	}
	manager, err := download.NewApplications(dir, db, registry.Upstreams())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	worker, err := prewarm.New(db, registry, service, manager, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	job, _, err := worker.Start(ctx, app.Key, warmplan.Input{RequestID: strings.Repeat("a", 32), Target: first.Version, ArtifactKeys: []string{"windows-x64-user"}, Limits: warmplan.DefaultLimits()}, false)
	if err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	job, err = worker.Wait(wait, app.UID, job.ID)
	if err != nil || job.State != "completed" || job.Succeeded != 1 {
		t.Fatal(job, err)
	}
	if downloaded.Load() != 1 {
		t.Fatal("wrong prewarm downloads", downloaded.Load())
	}
	if _, err = service.Authorize(ctx, app.Key, first.Version, "windows-arm64-user"); err == nil {
		t.Fatal("missing key silently substituted")
	}
	failed.Store(true)
	if _, err = service.Refresh(ctx, app.Key, "stable"); err == nil {
		t.Fatal("failed refresh reported success")
	}
	sql := storetest.Open(t, dir)
	if _, err = sql.Exec(`UPDATE channels SET fetched_at_s=1,expires_at_s=2 WHERE app_id=?`, app.StorageID()); err != nil {
		t.Fatal(err)
	}
	stale, err := service.Release(ctx, app.Key, "latest")
	if err != nil || stale.Version != first.Version {
		t.Fatal("old verified snapshot lost", stale.Version, err)
	}
	status, err := db.ReleaseRefresh(app.StorageID(), "stable")
	if err != nil || !status.Failed || status.Success == nil {
		t.Fatal(status, err)
	}
	// The minute scheduler respects the last attempt and starts only the exact
	// selection from a successful metadata commit, with a source epoch fence.
	failed.Store(false)
	callbacks := 0
	warm := func(_ context.Context, key, storage, version string, keys []string) error {
		callbacks++
		if key != app.Key || storage != app.StorageID() || version != first.Version || len(keys) != 1 || keys[0] != "windows-x64-user" {
			t.Fatal(key, storage, version, keys)
		}
		return prewarm.ErrBusy
	}
	service.RefreshDue(ctx, warm)
	if callbacks != 0 {
		t.Fatal("fresh attempt retried")
	}
	if _, err = sql.Exec(`UPDATE release_refresh_status SET attempted_at_s=1 WHERE app_uid=?`, app.UID); err != nil {
		t.Fatal(err)
	}
	service.RefreshDue(ctx, warm)
	if callbacks != 1 {
		t.Fatal("due selection not triggered", callbacks)
	}
	status, err = db.ReleaseRefresh(app.StorageID(), "stable")
	if err != nil || status.Failed {
		t.Fatal("prewarm failure invalidated metadata", status, err)
	}
	if err = worker.WarmSnapshot(ctx, app.Key, "wrong-source", first.Version, []string{"windows-x64-user"}); err == nil {
		t.Fatal("prewarm crossed source epoch")
	}
	// A failed refresh must not revoke already pinned authorization.
	resource, err := service.Authorize(ctx, app.Key, first.Version, "windows-x64-user")
	if err != nil || resource.Hash != hash {
		t.Fatal(resource, err)
	}
}
