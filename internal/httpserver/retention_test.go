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
	h.createApp("retention", "binary", "codex", map[string]any{"base_url": "http://retention.example", "cache_ttl_seconds": 60})
	for _, version := range []string{"1.0.0", "2.0.0", "10.0.0"} {
		h.request("GET", "/retention/binary/releases/"+version+"/asset.tgz", nil, 200, nil)
	}
	if _, err := h.server.catalog.Release(context.Background(), "retention/binary", "99.0.0"); err != nil {
		t.Fatal(err)
	}
	return h, fail
}

const retentionPath = "/admin/api/apps/retention/binary/retention"

// retentionPlanView is a retention preview with all its evaluated versions.
type retentionPlanView struct {
	retentionPreviewDTO
	Versions []retentionVersionDTO
}

func retentionPlan(t *testing.T, h *harness) retentionPlanView {
	t.Helper()
	data, headers := h.request("POST", retentionPath+"/preview", nil, 201, ifMatchHeader(h.appRevision("retention/binary")))
	p := retentionPlanView{retentionPreviewDTO: decodeJSONBody[retentionPreviewDTO](t, data)}
	if headers.Get("Location") != retentionPath+"/"+p.ID || p.ExecutedAt != nil || p.Result != nil || !p.ExpiresAt.After(p.CreatedAt) {
		t.Fatal("retention preview document", string(data), headers)
	}
	data, _ = h.request("GET", retentionPath+"/"+p.ID+"/items?limit=100", nil, 200, nil)
	p.Versions = decodeJSONBody[pageDTO[retentionVersionDTO]](t, data).Items
	return p
}
func setRetention(t *testing.T, h *harness, n int, enabled bool) {
	t.Helper()
	a, _ := h.server.store.Application("retention/binary")
	revision := h.patchApp(a.Key, map[string]any{"retention": map[string]any{"enabled": enabled, "keep_latest": n}})
	after, _ := h.server.store.Application(a.Key)
	if after.RuntimeRevision != a.RuntimeRevision || after.SourceEpoch != a.SourceEpoch || revision == a.Revision {
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
	data, _ := h.request("GET", retentionPath+"/"+p.ID+"/items?page=2&limit=1", nil, 200, nil)
	if page := decodeJSONBody[pageDTO[retentionVersionDTO]](t, data); page.Total != 3 || page.TotalPages != 3 || len(page.Items) != 1 || page.Items[0].Version != p.Versions[1].Version {
		t.Fatal("retention items page", string(data))
	}
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
	data, _ = h.request("POST", retentionPath+"/"+p.ID+"/execute", nil, 200, nil)
	executed := decodeJSONBody[retentionPreviewDTO](t, data)
	if executed.ExecutedAt == nil || executed.Result == nil || executed.Result.RetiredVersions != 1 || executed.Result.LogicalBytes != int64(len("binary:1.0.0")) || len(executed.Result.Selection) != 1 || executed.Result.Selection[0].Version != "1.0.0" {
		t.Fatal(string(data))
	}
	final := executed.Result
	if _, err = os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("retired unique blob still present", oldPath, err)
	}
	if again, _ := h.request("POST", retentionPath+"/"+p.ID+"/execute", nil, 200, nil); string(again) != string(data) {
		t.Fatal("repeated execution returned a different receipt", string(again))
	}
	if got, _ := h.request("GET", retentionPath+"/"+p.ID, nil, 200, nil); string(got) != string(data) {
		t.Fatal("receipt not readable", string(got))
	}
	data, _ = h.request("GET", retentionPath+"/status", nil, 200, nil)
	if status := decodeJSONBody[retentionStatusDTO](t, data); status.LastRun == nil || status.LastRun.Outcome != "success" || status.LastRun.Reason != nil || status.LastRun.SucceededAt == nil || status.LastRun.RetiredVersions != 1 {
		t.Fatal("retention status", string(data))
	}
	stats, _ := h.server.store.VersionStats(resource.Application)
	if stats["1.0.0"].ArtifactRequests == 0 {
		t.Fatal("download statistics removed")
	}
	h.close()
	restarted := newHarness(t, withDir(dir))
	defer restarted.close()
	entry, _ := restarted.server.registry.Lookup("retention/binary")
	job, err := restarted.server.store.Preview(entry.UID, store.PreviewRetention, p.ID, time.Now())
	if err != nil || job.ExecutedAt == nil || job.State != store.PreviewDone {
		t.Fatal(job, err)
	}
	repeat, err := restarted.server.maintenance.Execute(context.Background(), entry.Descriptor.ID, p.ID)
	if err != nil || repeat.LogicalBytes != final.LogicalBytes {
		t.Fatal(repeat, err)
	}
	restarted.login(h.password)
	data, _ = restarted.request("GET", retentionPath+"/status", nil, 200, nil)
	if status := decodeJSONBody[retentionStatusDTO](t, data); status.LastRun == nil || status.LastRun.Outcome != "success" {
		t.Fatal(string(data))
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
				h.patchApp(a.Key, map[string]any{"base_url": "https://changed.example"})
			case "disabled":
				h.setAppEnabled(a.Key, false)
			case "channel":
				_, err := h.sql().Exec(`UPDATE channels SET version='10.0.0' WHERE app_id=?`, entry.StorageID())
				if err != nil {
					t.Fatal(err)
				}
			case "expired_channel":
				h.sql().Exec(`UPDATE channels SET expires_at_s=? WHERE app_id=?`, time.Now().Add(-time.Hour).Unix(), entry.StorageID())
			case "expired_preview":
				h.sql().Exec(`UPDATE previews SET expires_at_s=1 WHERE id=?`, p.ID)
			case "ttl":
				h.patchApp(a.Key, map[string]any{"cache_ttl_seconds": 1})
			}
			switch change {
			case "expired_preview":
				h.expectError("POST", retentionPath+"/"+p.ID+"/execute", nil, 404, codePreviewNotFound, nil)
			case "disabled":
				h.expectError("POST", retentionPath+"/"+p.ID+"/execute", nil, 409, codeApplicationDisabled, nil)
			default:
				h.expectError("POST", retentionPath+"/"+p.ID+"/execute", nil, 409, codePreviewStale, nil)
			}
			var count int
			if err := h.sql().QueryRow(`SELECT count(*) FROM generations WHERE app_id=? AND retired_at_s IS NOT NULL`, entry.StorageID()).Scan(&count); err != nil || count != 0 {
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
	h.sql().QueryRow(`SELECT count(*) FROM previews`).Scan(&count)
	if count != 0 {
		t.Fatal("disabled schedule ran")
	}
	fail.Store(true)
	setRetention(t, h, 1, true)
	h.server.maintenance.Pass(context.Background())
	a, _ := h.server.registry.Lookup("retention/binary")
	h.sql().QueryRow(`SELECT count(*) FROM generations WHERE app_id=? AND retired_at_s IS NOT NULL`, a.StorageID()).Scan(&count)
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
	if _, err = h.sql().Exec(`UPDATE previews SET executed_at_s=? WHERE id=?`, time.Now().Add(-25*time.Hour).Unix(), p.ID); err != nil {
		t.Fatal(err)
	}
	h.expectError("POST", retentionPath+"/"+p.ID+"/execute", nil, 404, codePreviewNotFound, nil)
	h.expectError("GET", retentionPath+"/"+p.ID, nil, 404, codePreviewNotFound, nil)
	h.server.maintenance.Pass(context.Background())
	var count int
	h.sql().QueryRow(`SELECT count(*) FROM previews WHERE id=?`, p.ID).Scan(&count)
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
			want := 201
			if !valid {
				want = 502
			}
			data, _ := h.request("POST", "/admin/api/apps/"+app.Key+"/retention/preview", nil, want, ifMatchHeader(app.Revision))
			entry, _ := h.server.registry.Lookup(app.Key)
			var count int
			if err = h.sql().QueryRow(`SELECT count(*) FROM generations WHERE app_id=?`, entry.StorageID()).Scan(&count); err != nil || count != 0 {
				t.Fatal("metadata created binary generation", count, err)
			}
			if valid {
				preview := decodeJSONBody[retentionPreviewDTO](t, data)
				if preview.SelectedVersions != 0 {
					t.Fatal(string(data))
				}
				for _, channel := range []string{"latest", "stable"} {
					row, err := h.server.store.Channel(entry.StorageID(), channel)
					if err != nil || row.Version != "2.1.285" {
						t.Fatal(row, err)
					}
				}
				h.request("POST", "/admin/api/apps/"+app.Key+"/retention/"+preview.ID+"/execute", nil, 200, nil)
			} else {
				if code := errorCodeOf(t, data); code != string(codeChannelsUnverified) {
					t.Fatal(code)
				}
				h.sql().QueryRow(`SELECT count(*) FROM previews WHERE app_uid=?`, entry.UID).Scan(&count)
				if count != 0 {
					t.Fatal("created preview with unverified channel")
				}
			}
		})
	}
}

func TestRetentionPreviewRequiresSavedRevisionAndValidPreview(t *testing.T) {
	h, _ := retentionFixture(t, t.TempDir())
	data, _ := h.request("GET", retentionPath+"/status", nil, 200, nil)
	if status := decodeJSONBody[retentionStatusDTO](t, data); status.LastRun != nil || status.NextCheckAt != nil {
		t.Fatal("status before any run", string(data))
	}
	setRetention(t, h, 1, false)
	revision := h.appRevision("retention/binary")
	h.expectError("POST", retentionPath+"/preview", nil, 400, codeIfMatchRequired, nil)
	h.expectError("POST", retentionPath+"/preview", nil, 400, codeIfMatchRequired, map[string]string{"If-Match": "*"})
	h.expectError("POST", retentionPath+"/preview", nil, 409, codeRevisionConflict, ifMatchHeader(revision-1))
	p := retentionPlan(t, h)
	data, _ = h.request("GET", retentionPath+"/"+p.ID, nil, 200, nil)
	if got := decodeJSONBody[retentionPreviewDTO](t, data); got.ID != p.ID || got.SelectedVersions != 1 || got.Result != nil || got.LogicalBytes != int64(len("binary:1.0.0")) {
		t.Fatal(string(data))
	}
	data, _ = h.request("GET", retentionPath+"/"+p.ID+"/items?page=5", nil, 200, nil)
	if page := decodeJSONBody[pageDTO[retentionVersionDTO]](t, data); page.Page != 5 || page.Total != 3 || len(page.Items) != 0 {
		t.Fatal(string(data))
	}
	if names := strings.Join([]string{p.Versions[0].Version, p.Versions[1].Version, p.Versions[2].Version}, " "); names != "10.0.0 2.0.0 1.0.0" || !p.Versions[2].Selected || p.Versions[2].Reasons[0] != "outside_latest_n" {
		t.Fatal("retention items are newest first", p.Versions)
	}
	missing := strings.Repeat("e", 32)
	for _, path := range []string{"", "/items"} {
		h.expectError("GET", retentionPath+"/"+missing+path, nil, 404, codePreviewNotFound, nil)
	}
	h.expectError("POST", retentionPath+"/"+missing+"/execute", nil, 404, codePreviewNotFound, nil)
	h.expectError("GET", retentionPath+"/not-an-id", nil, 400, codeInvalidPath, nil)
	h.expectError("GET", retentionPath+"/"+p.ID+"/items?limit=0", nil, 400, codeInvalidQuery, nil)
	cleanup := decodeJSONBody[versionCleanupDTO](t, mustBody(h.request("POST", "/admin/api/apps/retention/binary/version-cleanup/preview", map[string]any{"minimum_version": "2.0.0"}, 201, nil)))
	h.expectError("GET", retentionPath+"/"+cleanup.ID, nil, 404, codePreviewNotFound, nil)
	h.expectError("POST", "/admin/api/apps/retention/binary/version-cleanup/"+p.ID+"/execute", nil, 404, codePreviewNotFound, nil)

	h.setAppEnabled("retention/binary", false)
	h.expectError("POST", retentionPath+"/preview", nil, 409, codeApplicationDisabled, ifMatchHeader(h.appRevision("retention/binary")))
	h.setVendorEnabled("retention", false)
	h.setAppEnabled("retention/binary", true)
	h.expectError("POST", retentionPath+"/preview", nil, 409, codeApplicationDisabled, ifMatchHeader(h.appRevision("retention/binary")))
	h.markDeleted("retention/binary")
	h.expectError("POST", retentionPath+"/preview", nil, 409, codeEntityDeleted, ifMatchHeader(h.appRevision("retention/binary")))
	h.expectError("POST", retentionPath+"/"+p.ID+"/execute", nil, 409, codeEntityDeleted, nil)
	h.request("GET", retentionPath+"/status", nil, 200, nil)
}
