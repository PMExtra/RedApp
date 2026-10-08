package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/warmplan"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func warmJob(t *testing.T, h *directoryHarness, key, id string) store.PrewarmJob {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := h.request("GET", "/admin/api/apps/"+key+"/prewarm/"+id, nil, 200, nil)
		var job store.PrewarmJob
		if err := json.Unmarshal(raw, &job); err != nil {
			t.Fatal(err)
		}
		if job.State != "running" {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("prewarm task did not finish")
	return store.PrewarmJob{}
}
func startWarm(t *testing.T, h *directoryHarness, key string, input any, status int) store.PrewarmJob {
	t.Helper()
	raw, _ := h.request("POST", "/admin/api/apps/"+key+"/prewarm/start", input, status, nil)
	var job store.PrewarmJob
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	return job
}
func warmApp(t *testing.T, h *directoryHarness, provider, base string) {
	h.createVendor("warm")
	v, _ := h.server.DB.Vendor("warm")
	h.request("PATCH", "/admin/api/vendors/warm", map[string]any{"revision": v.Revision, "enabled": true}, 200, nil)
	h.createApp("warm", "app", provider, map[string]any{"base_url": base, "enabled": true, "cache_ttl_seconds": 300})
}
func TestPrewarmHTTPBackgroundIdempotencyPagesAndRetry(t *testing.T) {
	var gets atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/root/" {
			for n := 0; n < 30; n++ {
				fmt.Fprintf(w, `<a href="f%d">f</a>`, n)
			}
			return
		}
		w.Header().Set("ETag", `"one"`)
		w.Header().Set("Content-Length", "4")
		if r.Method == "HEAD" {
			w.WriteHeader(304)
			return
		}
		gets.Add(1)
		fmt.Fprint(w, "body")
	}))
	defer origin.Close()
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	warmApp(t, h, "http-cache", origin.URL+"/root")
	status, _, _ := h.raw("POST", "/admin/api/apps/warm/app/prewarm/start", strings.NewReader("{\"request_id\":\""+strings.Repeat("f", 32)+"\",\"manifest\":\"/bad\xff\"}"), "application/json", nil)
	if status != 400 {
		t.Fatal("invalid UTF-8 manifest accepted", status)
	}
	input := map[string]any{"request_id": strings.Repeat("a", 32), "indexes": []string{"/"}}
	first := startWarm(t, h, "warm/app", input, 200)
	done := warmJob(t, h, "warm/app", first.ID)
	if done.State != "completed" || done.Completed != 30 || done.Succeeded != 30 || gets.Load() != 30 {
		t.Fatal(done, gets.Load())
	}
	again := startWarm(t, h, "warm/app", input, 200)
	if again.ID != first.ID {
		t.Fatal("idempotency created another task")
	}
	input["paths"] = []string{"/different"}
	startWarm(t, h, "warm/app", input, 409)
	raw, _ := h.request("GET", "/admin/api/apps/warm/app/prewarm/"+first.ID+"/items", nil, 200, nil)
	var page struct {
		Items []struct{ Status string }
		Total int
	}
	json.Unmarshal(raw, &page)
	if len(page.Items) != 25 || page.Total != 30 {
		t.Fatal(string(raw))
	}
	raw, _ = h.request("POST", "/admin/api/apps/warm/app/prewarm/"+first.ID+"/retry", map[string]string{"request_id": strings.Repeat("b", 32)}, 200, nil)
	var next store.PrewarmJob
	json.Unmarshal(raw, &next)
	retry := warmJob(t, h, "warm/app", next.ID)
	if retry.Completed != 0 || gets.Load() != 30 {
		t.Fatal("retry revisited successful resources", retry, gets.Load())
	}
	h.request("GET", "/admin/api/apps/warm/app/prewarm/"+first.ID+"/items?limit=101", nil, 400, nil)
	h.request("GET", "/admin/api/apps/warm/app/prewarm/"+strings.Repeat("f", 32), nil, 404, nil)
}
func TestPrewarmBusyCancelAndReadLimit(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("xx"))
		w.(http.Flusher).Flush()
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, strings.Repeat("x", 98))
	}))
	defer origin.Close()
	defer close(release)
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	warmApp(t, h, "http-cache", origin.URL)
	first := startWarm(t, h, "warm/app", map[string]any{"request_id": strings.Repeat("a", 32), "paths": []string{"/file"}}, 200)
	<-started
	startWarm(t, h, "warm/app", map[string]any{"request_id": strings.Repeat("b", 32), "paths": []string{"/file"}}, 409)
	h.request("POST", "/admin/api/apps/warm/app/prewarm/"+first.ID+"/cancel", map[string]any{}, 200, nil)
	if done := warmJob(t, h, "warm/app", first.ID); done.State != "cancelled" {
		t.Fatal(done)
	}
	next := startWarm(t, h, "warm/app", map[string]any{"request_id": strings.Repeat("c", 32), "paths": []string{"/another"}, "limits": map[string]any{"max_files": 1, "max_depth": 1, "max_download_bytes": 1, "max_duration_seconds": 10}}, 200)
	if done := warmJob(t, h, "warm/app", next.ID); done.State != "limited" || done.Succeeded != 0 {
		t.Fatal(done)
	}
}
func TestPrewarmCodexPlatformCacheVerificationAndMissing(t *testing.T) {
	var downloads atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "codex-npm-linux-x64-1.0.0.tgz"
		body := []byte("verified binary")
		sum := sha256.Sum256(body)
		size := int64(len(body))
		if strings.HasSuffix(r.URL.Path, "release.json") || strings.HasPrefix(r.URL.Path, "/channels/") {
			json.NewEncoder(w).Encode(codex.Release{Tag: "rust-v1.0.0", Assets: []codex.Asset{{Name: key, Size: &size, Digest: "sha256:" + hex.EncodeToString(sum[:]), URL: "http://warm.example/releases/1.0.0/" + key}}})
			return
		}
		downloads.Add(1)
		w.Write(body)
	}))
	defer proxy.Close()
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	if err := h.server.Pool.SetProxy(distributor.ProxyUpdate{Mode: "url", URL: proxy.URL}, h.server.Pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	warmApp(t, h, "codex", "http://warm.example")
	first := startWarm(t, h, "warm/app", map[string]any{"request_id": strings.Repeat("a", 32), "target": "1.0.0", "platforms": []string{"linux-x64"}}, 200)
	if done := warmJob(t, h, "warm/app", first.ID); done.State != "completed" || done.Succeeded != 1 {
		t.Fatal(done)
	}
	next := startWarm(t, h, "warm/app", map[string]any{"request_id": strings.Repeat("b", 32), "target": "1.0.0", "platforms": []string{"linux-x64"}}, 200)
	if done := warmJob(t, h, "warm/app", next.ID); done.State != "completed" || done.Bytes != 0 || downloads.Load() != 1 {
		t.Fatal(done, downloads.Load())
	}
	missing := startWarm(t, h, "warm/app", map[string]any{"request_id": strings.Repeat("c", 32), "target": "1.0.0", "platforms": []string{"darwin-arm64"}}, 200)
	if done := warmJob(t, h, "warm/app", missing.ID); done.State != "completed_with_errors" || downloads.Load() != 1 {
		t.Fatal(done)
	}
	app, _ := h.server.DB.Application("warm/app")
	beforeRuntime := app.RuntimeRevision
	_, err := h.server.DB.PatchApplicationConfiguration(app.Key, store.ConfigurationPatch{Revision: app.Revision, Set: map[string]json.RawMessage{"prewarm": encodeJSON(map[string]any{"enabled": true, "channels": []string{"latest"}, "platforms": []string{"linux-x64"}})}})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := h.server.DB.Application(app.Key)
	if updated.RuntimeRevision != beforeRuntime {
		t.Fatal("prewarm policy changed runtime revision")
	}
	service, err := h.server.Prewarmer()
	if err != nil {
		t.Fatal(err)
	}
	service.Automatic(context.Background())
	service.Automatic(context.Background())
	var reason string
	if err := h.server.DB.DB.QueryRow(`SELECT reason FROM prewarm_jobs WHERE automatic=1 ORDER BY rowid DESC LIMIT 1`).Scan(&reason); err != nil || reason != "unchanged_target" || downloads.Load() != 1 {
		t.Fatal(reason, err, downloads.Load())
	}
}

func TestPrewarmShutdownPersistsInterruptedAndDoesNotResume(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		fmt.Fprint(w, "xx")
		w.(http.Flusher).Flush()
		once.Do(func() { close(started) })
		<-r.Context().Done()
	}))
	defer origin.Close()
	dir := t.TempDir()
	h := newDirectoryHarness(t, dir)
	h.login(h.password)
	warmApp(t, h, "http-cache", origin.URL)
	first := startWarm(t, h, "warm/app", map[string]any{"request_id": strings.Repeat("a", 32), "paths": []string{"/file"}}, 200)
	<-started
	h.close()
	restarted := newDirectoryHarness(t, dir)
	restarted.login(h.password)
	raw, _ := restarted.request("GET", "/admin/api/apps/warm/app/prewarm/"+first.ID, nil, 200, nil)
	var job store.PrewarmJob
	json.Unmarshal(raw, &job)
	if job.State != "interrupted" || job.Succeeded != 0 {
		t.Fatal(job)
	}
	var running int
	if err := restarted.server.DB.DB.QueryRow(`SELECT count(*) FROM prewarm_jobs WHERE state='running'`).Scan(&running); err != nil || running != 0 {
		t.Fatal(running, err)
	}
}

func TestPrewarmManualIgnoresMetadataChangesAndDeletionDrains(t *testing.T) {
	started, release, deleted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var firstOnce, deleteOnce sync.Once
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4")
		fmt.Fprint(w, "bo")
		w.(http.Flusher).Flush()
		if r.URL.Path == "/first" {
			firstOnce.Do(func() { close(started) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if r.URL.Path == "/block" {
			deleteOnce.Do(func() { close(deleted) })
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, "dy")
	}))
	defer origin.Close()
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	warmApp(t, h, "http-cache", origin.URL)
	first := startWarm(t, h, "warm/app", map[string]any{"request_id": strings.Repeat("a", 32), "paths": []string{"/first", "/second"}}, 200)
	<-started
	app, _ := h.server.DB.Application("warm/app")
	if _, err := h.server.DB.PatchApplicationConfiguration(app.Key, store.ConfigurationPatch{Revision: app.Revision, Set: map[string]json.RawMessage{"name.en": encodeJSON("Renamed")}}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if done := warmJob(t, h, "warm/app", first.ID); done.State != "completed" || done.Succeeded != 2 {
		t.Fatal("metadata interrupted manual task", done)
	}
	second := startWarm(t, h, "warm/app", map[string]any{"request_id": strings.Repeat("b", 32), "paths": []string{"/block"}}, 200)
	<-deleted
	app, _ = h.server.DB.Application("warm/app")
	h.request("DELETE", "/admin/api/apps/warm/app", deleteBody(app), 200, nil)
	h.request("GET", "/admin/api/apps/warm/app/prewarm/"+second.ID, nil, 404, nil)
	var jobs int
	if err := h.server.DB.DB.QueryRow(`SELECT count(*) FROM prewarm_jobs WHERE app_uid=?`, app.UID).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal(jobs, err)
	}
}

func TestPrewarmAutomaticPolicyChangeCancelsRemainingArtifacts(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var other atomic.Int64
	body := []byte("signed by metadata digest")
	sum := sha256.Sum256(body)
	keys := []string{"codex-npm-linux-x64-1.0.0.tgz", "codex-npm-win32-x64-1.0.0.tgz"}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "release.json") || strings.HasPrefix(r.URL.Path, "/channels/") {
			assets := []codex.Asset{}
			for _, key := range keys {
				assets = append(assets, codex.Asset{Name: key, Digest: "sha256:" + hex.EncodeToString(sum[:]), URL: "http://warm.example/releases/1.0.0/" + key})
			}
			json.NewEncoder(w).Encode(codex.Release{Tag: "rust-v1.0.0", Assets: assets})
			return
		}
		if strings.HasSuffix(r.URL.Path, keys[0]) {
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			w.Write(body[:2])
			w.(http.Flusher).Flush()
			once.Do(func() { close(started) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.Write(body[2:])
			return
		}
		other.Add(1)
		w.Write(body)
	}))
	defer proxy.Close()
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	if err := h.server.Pool.SetProxy(distributor.ProxyUpdate{Mode: "url", URL: proxy.URL}, h.server.Pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	warmApp(t, h, "codex", "http://warm.example")
	set := func(enabled bool) {
		a, _ := h.server.DB.Application("warm/app")
		_, err := h.server.DB.PatchApplicationConfiguration(a.Key, store.ConfigurationPatch{Revision: a.Revision, Set: map[string]json.RawMessage{"prewarm": encodeJSON(map[string]any{"enabled": enabled, "channels": []string{"latest"}, "platforms": []string{"linux-x64", "win32-x64"}})}})
		if err != nil {
			t.Fatal(err)
		}
	}
	set(true)
	service, err := h.server.Prewarmer()
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.Start(context.Background(), "warm/app", warmplan.Input{RequestID: strings.Repeat("a", 32), Target: "latest", Platforms: []string{"linux-x64", "win32-x64"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	set(false)
	close(release)
	done := warmJob(t, h, "warm/app", job.ID)
	if done.State != "cancelled" || done.Completed != 1 || other.Load() != 0 {
		t.Fatal(done, other.Load())
	}
	var successes int
	if err := h.server.DB.DB.QueryRow(`SELECT count(*) FROM prewarm_success`).Scan(&successes); err != nil || successes != 0 {
		t.Fatal(successes, err)
	}
}
