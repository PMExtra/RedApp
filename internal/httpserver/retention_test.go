package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/releasemaintenance"
	"github.com/PMExtra/RedApp/internal/store"
)

type retentionFixtureControl struct {
	atomic.Bool
	block       atomic.Bool
	started     chan struct{}
	release     chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
}

func (c *retentionFixtureControl) finish() { c.releaseOnce.Do(func() { close(c.release) }) }
func retentionFixture(t *testing.T, dir string) (*harness, *retentionFixtureControl) {
	t.Helper()
	h := newHarness(t, withDir(dir))
	h.login(h.password)
	fail := &retentionFixtureControl{started: make(chan struct{}), release: make(chan struct{})}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		version := "2.0.0"
		if len(parts) >= 3 && parts[0] == "releases" {
			version = parts[1]
		}
		if parts[0] == "channels" && fail.Load() {
			http.Error(w, "unavailable", 503)
			return
		}
		body := []byte("binary:" + version)
		if strings.HasSuffix(r.URL.Path, "release.json") || parts[0] == "channels" {
			digest := sha256.Sum256(body)
			extraDigest := sha256.Sum256([]byte("extra:" + version))
			json.NewEncoder(w).Encode(codex.Release{Tag: "rust-v" + version, Assets: []codex.Asset{{Name: "asset.tgz", Digest: "sha256:" + hex.EncodeToString(digest[:]), URL: "http://retention.example/releases/" + version + "/asset.tgz"}, {Name: "extra.tgz", Digest: "sha256:" + hex.EncodeToString(extraDigest[:]), URL: "http://retention.example/releases/" + version + "/extra.tgz"}}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "extra.tgz") {
			body = []byte("extra:" + version)
		}
		if fail.block.Load() && version == "1.0.0" && strings.HasSuffix(r.URL.Path, "extra.tgz") {
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			w.Write(body[:2])
			w.(http.Flusher).Flush()
			fail.startOnce.Do(func() { close(fail.started) })
			<-fail.release
			w.Write(body[2:])
			return
		}
		w.Write(body)
	}))
	t.Cleanup(proxy.Close)
	t.Cleanup(fail.finish)
	if err := h.server.pool.SetProxy(distributor.ProxyUpdate{Mode: "url", URL: proxy.URL}, h.server.pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	h.createVendor("retention")
	v, _ := h.server.store.Vendor("retention")
	h.request("PATCH", "/admin/api/vendors/retention", map[string]any{"revision": v.Revision, "enabled": true}, 200, nil)
	h.createApp("retention", "binary", "codex", map[string]any{"base_url": "http://retention.example", "cache_ttl_seconds": 60, "enabled": true})
	for _, version := range []string{"1.0.0", "2.0.0", "10.0.0"} {
		h.request("GET", "/retention/binary/releases/"+version+"/asset.tgz", nil, 200, nil)
	}
	if _, err := h.server.catalog.Release(context.Background(), "retention/binary", "99.0.0"); err != nil {
		t.Fatal(err)
	}
	return h, fail
}
func retentionPlan(t *testing.T, h *harness) releasemaintenance.Preview {
	t.Helper()
	a, _ := h.server.store.Application("retention/binary")
	data, _ := h.request("POST", "/admin/api/apps/retention/binary/retention/preview", map[string]any{"revision": a.Revision}, 200, nil)
	var p releasemaintenance.Preview
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	data, _ = h.request("GET", "/admin/api/apps/retention/binary/retention/"+p.ID+"/items?limit=100", nil, 200, nil)
	var page struct {
		Items []store.RetentionVersion `json:"items"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	p.Versions = page.Items
	return p
}
func setRetention(t *testing.T, h *harness, n int, enabled bool) {
	t.Helper()
	a, _ := h.server.store.Application("retention/binary")
	cfg, err := h.server.store.PatchApplicationConfiguration(a.Key, store.ConfigurationPatch{Revision: a.Revision, Set: map[string]json.RawMessage{"retention": encodeJSON(map[string]any{"enabled": enabled, "keep_latest": n})}})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := h.server.store.Application(a.Key)
	if after.RuntimeRevision != a.RuntimeRevision || after.SourceEpoch != a.SourceEpoch || cfg.Revision == a.Revision {
		t.Fatal("retention changed runtime fence", a, after)
	}
}
func TestRetentionAPIProtectsReadersAndPersistsReceipt(t *testing.T) {
	dir := t.TempDir()
	h, _ := retentionFixture(t, dir)
	setRetention(t, h, 1, false)
	p := retentionPlan(t, h)
	if p.SelectedVersions != 1 || len(p.Versions) != 3 {
		t.Fatal(p)
	}
	h.request("GET", "/admin/api/apps/retention/binary/retention/"+p.ID+"/items?page=1&limit=1", nil, 200, nil)
	resource, err := h.server.catalog.Authorize(context.Background(), "retention/binary", "1.0.0", "asset.tgz")
	if err != nil {
		t.Fatal(err)
	}
	oldPath := ""
	for _, v := range h.server.downloads.SnapshotFor(resource.Application, resource.Version) {
		if v.Current {
			oldPath = v.Path
		}
	}
	reader, _, err := h.server.downloads.Acquire(context.Background(), resource)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := h.server.maintenance.Execute(context.Background(), "retention/binary", p.ID)
	if err != nil || receipt.RetiredVersions != 0 || receipt.Skipped["1.0.0"] != "in_use_at_execution" {
		t.Fatal(receipt, err)
	}
	reader.Close()
	p = retentionPlan(t, h)
	data, _ := h.request("POST", "/admin/api/apps/retention/binary/retention/"+p.ID+"/execute", map[string]any{}, 200, nil)
	var final store.RetentionReceipt
	json.Unmarshal(data, &final)
	if final.RetiredVersions != 1 || final.LogicalBytes != int64(len("binary:1.0.0")) {
		t.Fatal(string(data))
	}
	if _, err = os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("retired unique blob still present", oldPath, err)
	}
	h.request("POST", "/admin/api/apps/retention/binary/retention/"+p.ID+"/execute", map[string]any{}, 200, nil)
	stats, _ := h.server.store.VersionStats(resource.Application)
	if stats["1.0.0"].ArtifactRequests == 0 {
		t.Fatal("download statistics removed")
	}
	h.close()
	restarted := newHarness(t, withDir(dir))
	defer restarted.close()
	entry, _ := restarted.server.registry.Lookup("retention/binary")
	job, err := restarted.server.store.CleanupPreview(entry.StorageID(), p.ID)
	if err != nil || job.ExecutedAt == nil || len(job.Result) == 0 {
		t.Fatal(job, err)
	}
	repeat, err := restarted.server.maintenance.Execute(context.Background(), entry.Descriptor.ID, p.ID)
	if err != nil || repeat.LogicalBytes != final.LogicalBytes {
		t.Fatal(repeat, err)
	}
	raw, _ := restarted.server.store.RetentionStatus(entry.UID)
	if !strings.Contains(string(raw), "success") {
		t.Fatal(string(raw))
	}
}
func TestRetentionRejectsChangedPolicySourceAndChannels(t *testing.T) {
	for _, change := range []string{"retention", "source", "disabled", "channel", "expired_channel", "expired_preview", "ttl"} {
		t.Run(change, func(t *testing.T) {
			h, _ := retentionFixture(t, t.TempDir())
			setRetention(t, h, 1, false)
			p := retentionPlan(t, h)
			a, _ := h.server.store.Application("retention/binary")
			entry, _ := h.server.registry.Lookup(a.Key)
			switch change {
			case "retention":
				setRetention(t, h, 2, false)
			case "source":
				h.request("PATCH", "/admin/api/apps/"+a.Key, map[string]any{"revision": a.Revision, "base_url": "https://changed.example"}, 200, nil)
			case "disabled":
				h.request("PATCH", "/admin/api/apps/"+a.Key, map[string]any{"revision": a.Revision, "enabled": false}, 200, nil)
			case "channel":
				_, err := h.server.store.DB.Exec(`UPDATE channels SET version='10.0.0' WHERE app_id=?`, entry.StorageID())
				if err != nil {
					t.Fatal(err)
				}
			case "expired_channel":
				h.server.store.DB.Exec(`UPDATE channels SET expires_at_s=? WHERE app_id=?`, time.Now().Add(-time.Hour).Unix(), entry.StorageID())
			case "expired_preview":
				h.server.store.DB.Exec(`UPDATE cleanup_previews SET expires_at_s=0 WHERE id=?`, p.ID)
			case "ttl":
				_, err := h.server.store.PatchApplicationConfiguration(a.Key, store.ConfigurationPatch{Revision: a.Revision, Set: map[string]json.RawMessage{"cache_ttl_seconds": encodeJSON(1)}})
				if err != nil {
					t.Fatal(err)
				}
			}
			h.request("POST", "/admin/api/apps/retention/binary/retention/"+p.ID+"/execute", map[string]any{}, 409, nil)
			var count int
			if err := h.server.store.DB.QueryRow(`SELECT count(*) FROM generations WHERE app_id=? AND retired_at_s IS NOT NULL`, entry.StorageID()).Scan(&count); err != nil || count != 0 {
				t.Fatal("retired despite invalid preview", count, err)
			}
		})
	}
}
func TestRetentionUnverifiedChannelDeletesNothingAndDisabledScheduleDoesNothing(t *testing.T) {
	h, fail := retentionFixture(t, t.TempDir())
	setRetention(t, h, 1, false)
	h.server.maintenance.Pass(context.Background())
	var count int
	h.server.store.DB.QueryRow(`SELECT count(*) FROM cleanup_previews`).Scan(&count)
	if count != 0 {
		t.Fatal("disabled schedule ran")
	}
	fail.Store(true)
	setRetention(t, h, 1, true)
	h.server.maintenance.Pass(context.Background())
	a, _ := h.server.registry.Lookup("retention/binary")
	h.server.store.DB.QueryRow(`SELECT count(*) FROM generations WHERE app_id=? AND retired_at_s IS NOT NULL`, a.StorageID()).Scan(&count)
	if count != 0 {
		t.Fatal(count)
	}
	raw, _ := h.server.store.RetentionStatus(a.UID)
	if !strings.Contains(string(raw), "channel_unavailable") {
		t.Fatal(string(raw))
	}
	fail.Store(false)
	h.server.maintenance.Pass(context.Background())
	raw, _ = h.server.store.RetentionStatus(a.UID)
	if !strings.Contains(string(raw), `"retired_versions":1`) {
		t.Fatal(string(raw))
	}
}

func TestRetentionNewWriterProtectsWholeVersionWithoutExpandingFrozenSelection(t *testing.T) {
	h, control := retentionFixture(t, t.TempDir())
	setRetention(t, h, 1, false)
	p := retentionPlan(t, h)
	control.block.Store(true)
	r, err := h.server.catalog.Authorize(context.Background(), "retention/binary", "1.0.0", "extra.tgz")
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := h.server.downloads.Acquire(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-control.started:
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not start")
	}
	reader.Close()
	receipt, err := h.server.maintenance.Execute(context.Background(), "retention/binary", p.ID)
	if err != nil || receipt.RetiredVersions != 0 || receipt.Skipped["1.0.0"] != "in_use_at_execution" {
		t.Fatal(receipt, err)
	}
	control.finish()
	drain, _, err := h.server.downloads.Acquire(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	defer drain.Close()
	if _, err = io.Copy(io.Discard, drain); err != nil {
		t.Fatal(err)
	}
	for _, v := range h.server.downloads.SnapshotFor(r.Application, r.Version) {
		if v.Retired {
			t.Fatal("part of protected version retired", v)
		}
	}
}

func TestRetentionScheduledPassRetiresAtMost100Versions(t *testing.T) {
	h, _ := retentionFixture(t, t.TempDir())
	for version := 3; version <= 107; version++ {
		if version == 10 {
			continue
		}
		h.request("GET", fmt.Sprintf("/retention/binary/releases/%d.0.0/asset.tgz", version), nil, 200, nil)
	}
	setRetention(t, h, 1, true)
	service := h.server.maintenance
	service.Pass(context.Background())
	entry, _ := h.server.registry.Lookup("retention/binary")
	raw, _ := h.server.store.RetentionStatus(entry.UID)
	var status releasemaintenance.Status
	json.Unmarshal(raw, &status)
	if status.RetiredVersions != 100 {
		t.Fatal(string(raw))
	}
	service.Pass(context.Background())
	raw, _ = h.server.store.RetentionStatus(entry.UID)
	json.Unmarshal(raw, &status)
	if status.RetiredVersions != 5 {
		t.Fatal(string(raw))
	}
}

func TestRetentionReceiptExpiresAndEmptySelectionSucceeds(t *testing.T) {
	h, _ := retentionFixture(t, t.TempDir())
	setRetention(t, h, 3, false)
	p := retentionPlan(t, h)
	receipt, err := h.server.maintenance.Execute(context.Background(), "retention/binary", p.ID)
	if err != nil || receipt.RetiredVersions != 0 || len(receipt.Selection) != 0 {
		t.Fatal(receipt, err)
	}
	if _, err = h.server.store.DB.Exec(`UPDATE cleanup_previews SET executed_at_s=? WHERE id=?`, time.Now().Add(-25*time.Hour).Unix(), p.ID); err != nil {
		t.Fatal(err)
	}
	h.request("POST", "/admin/api/apps/retention/binary/retention/"+p.ID+"/execute", map[string]any{}, 409, nil)
	h.server.maintenance.Pass(context.Background())
	var count int
	h.server.store.DB.QueryRow(`SELECT count(*) FROM cleanup_previews WHERE id=?`, p.ID).Scan(&count)
	if count != 0 {
		t.Fatal("expired receipt not pruned while schedule disabled")
	}
}

func TestRetentionClaudeUsesVerifiedChannelsAndNeverWarmsMetadataOnlyRelease(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			h, _ := retentionFixture(t, t.TempDir())
			manifest, err := os.ReadFile("../apps/claude/testdata/manifest.json")
			if err != nil {
				t.Fatal(err)
			}
			signature, err := os.ReadFile("../apps/claude/testdata/manifest.json.sig")
			if err != nil {
				t.Fatal(err)
			}
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/latest", "/stable":
					io.WriteString(w, "2.1.285")
				case "/2.1.285/manifest.json":
					w.Write(manifest)
				case "/2.1.285/manifest.json.sig":
					if valid {
						w.Write(signature)
					} else {
						io.WriteString(w, "invalid signature")
					}
				default:
					t.Errorf("retention attempted binary download: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer proxy.Close()
			if err = h.server.pool.SetProxy(distributor.ProxyUpdate{Mode: "url", URL: proxy.URL}, h.server.pool.Proxy().Revision); err != nil {
				t.Fatal(err)
			}
			app := h.createApp("retention", "claude", "claude-code", map[string]any{"base_url": "http://retention.example", "cache_ttl_seconds": 60, "enabled": true})
			want := 200
			if !valid {
				want = 409
			}
			data, _ := h.request("POST", "/admin/api/apps/"+app.Key+"/retention/preview", map[string]any{"revision": app.Revision}, want, nil)
			entry, _ := h.server.registry.Lookup(app.Key)
			var count int
			if err = h.server.store.DB.QueryRow(`SELECT count(*) FROM generations WHERE app_id=?`, entry.StorageID()).Scan(&count); err != nil || count != 0 {
				t.Fatal("metadata created binary generation", count, err)
			}
			if valid {
				var preview releasemaintenance.Preview
				json.Unmarshal(data, &preview)
				if preview.SelectedVersions != 0 {
					t.Fatal(string(data))
				}
				for _, channel := range []string{"latest", "stable"} {
					row, err := h.server.store.Channel(entry.StorageID(), channel)
					if err != nil || row.Version != "2.1.285" {
						t.Fatal(row, err)
					}
				}
				h.request("POST", "/admin/api/apps/"+app.Key+"/retention/"+preview.ID+"/execute", map[string]any{}, 200, nil)
			} else {
				h.server.store.DB.QueryRow(`SELECT count(*) FROM cleanup_previews WHERE app_id=?`, entry.StorageID()).Scan(&count)
				if count != 0 {
					t.Fatal("created preview with unverified channel")
				}
			}
		})
	}
}
