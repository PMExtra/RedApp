package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/warmplan"
)

const warmPath = "/admin/api/apps/warm/app/prewarm"

func warmJob(t *testing.T, h *harness, id string) prewarmJobDTO {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := h.request("GET", warmPath+"/jobs/"+id, nil, 200, nil)
		job := decodeJSONBody[prewarmJobDTO](t, raw)
		if job.State != "running" {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("prewarm task did not finish")
	return prewarmJobDTO{}
}

// startWarm starts a job of warm/app and requires status (201 new, 200 repeated).
func startWarm(t *testing.T, h *harness, input any, status int) prewarmJobDTO {
	t.Helper()
	raw, headers := h.request("POST", warmPath+"/jobs", input, status, nil)
	job := decodeJSONBody[prewarmJobDTO](t, raw)
	if status == 201 && headers.Get("Location") != warmPath+"/jobs/"+job.ID {
		t.Fatal("created job without its Location", headers)
	}
	return job
}

func warmApp(h *harness, provider, base string) {
	h.createVendor("warm")
	h.createApp("warm", "app", provider, map[string]any{"base_url": base, "cache_ttl_seconds": 300})
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
	h := newHarness(t)
	h.login(h.password)
	warmApp(h, "http-cache", origin.URL+"/root")
	options, _ := h.request("GET", warmPath+"/options", nil, 200, nil)
	if got := decodeJSONBody[prewarmOptionsDTO](t, options); got.Kind != "http_cache" || len(got.Channels) != 0 || len(got.Platforms) != 0 || got.DefaultLimits != warmplan.DefaultLimits() {
		t.Fatal("HTTP cache options", string(options))
	}
	status, data, _ := h.raw("POST", warmPath+"/jobs", strings.NewReader("{\"request_id\":\""+strings.Repeat("f", 32)+"\",\"manifest\":\"/bad\xff\"}"), "application/json", nil)
	if status != 400 || errorCodeOf(t, data) != string(codeInvalidRequest) {
		t.Fatal("invalid UTF-8 manifest accepted", status, string(data))
	}
	for _, invalid := range []map[string]any{
		{"request_id": "not-an-id", "paths": []string{"/a"}},
		{"request_id": strings.Repeat("d", 32), "target": "latest", "platforms": []string{"linux-x64"}},
		{"request_id": strings.Repeat("d", 32), "paths": []string{"relative"}},
		{"request_id": strings.Repeat("d", 32), "paths": []string{"/a"}, "limits": map[string]any{"max_files": 0, "max_depth": 1, "max_download_bytes": 1, "max_duration_seconds": 1}},
	} {
		h.expectError("POST", warmPath+"/jobs", invalid, 400, codeValidationFailed, nil)
	}
	input := map[string]any{"request_id": strings.Repeat("a", 32), "indexes": []string{"/"}}
	first := startWarm(t, h, input, 201)
	if first.Target != nil || first.ResolvedVersion != nil || first.Limits != warmplan.DefaultLimits() {
		t.Fatal("HTTP cache job fields", first)
	}
	done := warmJob(t, h, first.ID)
	if done.State != "completed" || done.Reason != nil || done.Completed != 30 || done.Succeeded != 30 || gets.Load() != 30 {
		t.Fatal(done, gets.Load())
	}
	if again := startWarm(t, h, input, 200); again.ID != first.ID {
		t.Fatal("idempotency created another task")
	}
	input["paths"] = []string{"/different"}
	h.expectError("POST", warmPath+"/jobs", input, 409, codePrewarmRequestConflict, nil)
	raw, _ := h.request("GET", warmPath+"/jobs/"+first.ID+"/items", nil, 200, nil)
	page := decodeJSONBody[pageDTO[prewarmItemDTO]](t, raw)
	if len(page.Items) != 25 || page.Total != 30 || page.TotalPages != 2 || page.Items[0].Status != "downloaded" || page.Items[0].Reason != nil {
		t.Fatal(string(raw))
	}
	raw, _ = h.request("GET", warmPath+"/jobs/"+first.ID+"/items?page=3&limit=25", nil, 200, nil)
	if page = decodeJSONBody[pageDTO[prewarmItemDTO]](t, raw); page.Page != 3 || len(page.Items) != 0 {
		t.Fatal("page beyond the last", string(raw))
	}
	retryInput := map[string]string{"request_id": strings.Repeat("b", 32)}
	raw, headers := h.request("POST", warmPath+"/jobs/"+first.ID+"/retry", retryInput, 201, nil)
	next := decodeJSONBody[prewarmJobDTO](t, raw)
	if headers.Get("Location") != warmPath+"/jobs/"+next.ID {
		t.Fatal(headers)
	}
	if retry := warmJob(t, h, next.ID); retry.Completed != 0 || gets.Load() != 30 {
		t.Fatal("retry revisited successful resources", retry, gets.Load())
	}
	if raw, _ = h.request("POST", warmPath+"/jobs/"+first.ID+"/retry", retryInput, 200, nil); decodeJSONBody[prewarmJobDTO](t, raw).ID != next.ID {
		t.Fatal("repeated retry request_id started another job")
	}
	h.expectError("POST", warmPath+"/jobs/"+first.ID+"/retry", map[string]string{"request_id": "bad"}, 400, codeValidationFailed, nil)
	h.expectError("GET", warmPath+"/jobs/"+first.ID+"/items?limit=101", nil, 400, codeInvalidQuery, nil)
	h.expectError("GET", warmPath+"/jobs/"+strings.Repeat("f", 32), nil, 404, codeJobNotFound, nil)
	h.expectError("POST", warmPath+"/jobs/"+strings.Repeat("f", 32)+"/retry", retryInput, 404, codeJobNotFound, nil)
	h.expectError("GET", warmPath+"/jobs/not-a-job", nil, 400, codeInvalidPath, nil)
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
	h := newHarness(t)
	h.login(h.password)
	warmApp(h, "http-cache", origin.URL)
	first := startWarm(t, h, map[string]any{"request_id": strings.Repeat("a", 32), "paths": []string{"/file"}}, 201)
	<-started
	h.expectError("POST", warmPath+"/jobs", map[string]any{"request_id": strings.Repeat("b", 32), "paths": []string{"/file"}}, 409, codePrewarmBusy, nil)
	h.expectError("POST", warmPath+"/jobs/"+first.ID+"/retry", map[string]string{"request_id": strings.Repeat("b", 32)}, 409, codeOperationInProgress, nil)
	raw, _ := h.request("POST", warmPath+"/jobs/"+first.ID+"/cancel", nil, 202, nil)
	if job := decodeJSONBody[prewarmJobDTO](t, raw); job.ID != first.ID {
		t.Fatal(string(raw))
	}
	done := warmJob(t, h, first.ID)
	if done.State != "cancelled" || done.Reason == nil || *done.Reason != "cancelled_or_configuration_changed" {
		t.Fatal(done)
	}
	raw, _ = h.request("POST", warmPath+"/jobs/"+first.ID+"/cancel", nil, 202, nil)
	if job := decodeJSONBody[prewarmJobDTO](t, raw); job.State != "cancelled" {
		t.Fatal("cancelling a finished job changed it", string(raw))
	}
	h.expectError("POST", warmPath+"/jobs/"+strings.Repeat("e", 32)+"/cancel", nil, 404, codeJobNotFound, nil)
	limits := map[string]any{"max_files": 1, "max_depth": 1, "max_download_bytes": 1, "max_duration_seconds": 10}
	next := startWarm(t, h, map[string]any{"request_id": strings.Repeat("c", 32), "paths": []string{"/another"}, "limits": limits}, 201)
	if done := warmJob(t, h, next.ID); done.State != "limited" || done.Succeeded != 0 || done.Limits.MaxDownloadBytes != 1 {
		t.Fatal(done)
	}
}

func TestPrewarmCapabilitiesAndDeletedApplications(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	h.createVendor("warm")
	for _, provider := range []string{"hosted", "info"} {
		a := h.createApp("warm", provider, provider, nil)
		h.expectError("GET", "/admin/api/apps/"+a.Key+"/prewarm/options", nil, 404, codeCapabilityUnsupported, nil)
		h.expectError("POST", "/admin/api/apps/"+a.Key+"/prewarm/jobs", map[string]any{"request_id": strings.Repeat("a", 32)}, 404, codeCapabilityUnsupported, nil)
	}
	h.expectError("GET", "/admin/api/apps/warm/missing/prewarm/options", nil, 404, codeApplicationNotFound, nil)
	h.expectError("GET", "/admin/api/apps/warm/Bad_Id/prewarm/options", nil, 400, codeInvalidPath, nil)
	a := h.createApp("warm", "cache", "http-cache", map[string]any{"base_url": "http://warm.example"})
	h.markDeleted(a.Key)
	h.request("GET", "/admin/api/apps/"+a.Key+"/prewarm/options", nil, 200, nil)
	h.expectError("POST", "/admin/api/apps/"+a.Key+"/prewarm/jobs", map[string]any{"request_id": strings.Repeat("a", 32), "paths": []string{"/a"}}, 409, codeEntityDeleted, nil)
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
	h := newHarness(t)
	h.login(h.password)
	if err := h.server.pool.SetProxy(distributor.ProxyUpdate{Mode: "url", URL: proxy.URL}, h.server.pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	warmApp(h, "codex", "http://warm.example")
	raw, _ := h.request("GET", warmPath+"/options", nil, 200, nil)
	options := decodeJSONBody[prewarmOptionsDTO](t, raw)
	if options.Kind != "release" || fmt.Sprint(options.Channels) != "[latest]" || len(options.Platforms) != 6 || options.Platforms[0].Name == "" {
		t.Fatal("release options", string(raw))
	}
	h.expectError("POST", warmPath+"/jobs", map[string]any{"request_id": strings.Repeat("d", 32), "target": "1.0.0", "platforms": []string{"plan9"}}, 400, codeValidationFailed, nil)
	h.expectError("POST", warmPath+"/jobs", map[string]any{"request_id": strings.Repeat("d", 32), "paths": []string{"/a"}}, 400, codeValidationFailed, nil)
	first := startWarm(t, h, map[string]any{"request_id": strings.Repeat("a", 32), "target": "1.0.0", "platforms": []string{"linux-x64"}}, 201)
	if first.Target == nil || *first.Target != "1.0.0" || fmt.Sprint(first.Platforms) != "[linux-x64]" {
		t.Fatal("release job fields", first)
	}
	if done := warmJob(t, h, first.ID); done.State != "completed" || done.Succeeded != 1 || done.ResolvedVersion == nil || *done.ResolvedVersion != "1.0.0" {
		t.Fatal(done)
	}
	next := startWarm(t, h, map[string]any{"request_id": strings.Repeat("b", 32), "target": "1.0.0", "platforms": []string{"linux-x64"}}, 201)
	if done := warmJob(t, h, next.ID); done.State != "completed" || done.Bytes != 0 || downloads.Load() != 1 {
		t.Fatal(done, downloads.Load())
	}
	missing := startWarm(t, h, map[string]any{"request_id": strings.Repeat("c", 32), "target": "1.0.0", "platforms": []string{"darwin-arm64"}}, 201)
	if done := warmJob(t, h, missing.ID); done.State != "completed_with_errors" || downloads.Load() != 1 {
		t.Fatal(done)
	}
	app, _ := h.server.store.Application("warm/app")
	h.patchApp(app.Key, map[string]any{"prewarm": map[string]any{"enabled": true, "channels": []string{"latest"}, "platforms": []string{"linux-x64"}}})
	if updated, _ := h.server.store.Application(app.Key); updated.RuntimeRevision != app.RuntimeRevision {
		t.Fatal("prewarm policy changed runtime revision")
	}
	service := h.server.prewarmer
	service.Automatic(context.Background())
	service.Automatic(context.Background())
	var reason string
	if err := h.server.store.DB.QueryRow(`SELECT reason FROM prewarm_jobs WHERE automatic=1 ORDER BY rowid DESC LIMIT 1`).Scan(&reason); err != nil || reason != "unchanged_target" || downloads.Load() != 1 {
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
	h := newHarness(t, withDir(dir))
	h.login(h.password)
	warmApp(h, "http-cache", origin.URL)
	first := startWarm(t, h, map[string]any{"request_id": strings.Repeat("a", 32), "paths": []string{"/file"}}, 201)
	<-started
	h.close()
	restarted := newHarness(t, withDir(dir))
	restarted.login(h.password)
	raw, _ := restarted.request("GET", warmPath+"/jobs/"+first.ID, nil, 200, nil)
	job := decodeJSONBody[prewarmJobDTO](t, raw)
	if job.State != "interrupted" || job.Succeeded != 0 || job.Reason == nil || *job.Reason != "interrupted_by_shutdown" && *job.Reason != "interrupted_by_restart" {
		t.Fatal(job)
	}
	var running int
	if err := restarted.server.store.DB.QueryRow(`SELECT count(*) FROM prewarm_jobs WHERE state='running'`).Scan(&running); err != nil || running != 0 {
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
	h := newHarness(t)
	h.login(h.password)
	warmApp(h, "http-cache", origin.URL)
	first := startWarm(t, h, map[string]any{"request_id": strings.Repeat("a", 32), "paths": []string{"/first", "/second"}}, 201)
	<-started
	h.patchApp("warm/app", map[string]any{"name.en": "Renamed"})
	close(release)
	if done := warmJob(t, h, first.ID); done.State != "completed" || done.Succeeded != 2 {
		t.Fatal("metadata interrupted manual task", done)
	}
	second := startWarm(t, h, map[string]any{"request_id": strings.Repeat("b", 32), "paths": []string{"/block"}}, 201)
	<-deleted
	app, _ := h.server.store.Application("warm/app")
	if err := h.server.deleteApplication(context.Background(), app.Key, app.Revision); err != nil {
		t.Fatal(err)
	}
	// The deleted application's jobs are gone with it.
	data, _ := h.request("GET", warmPath+"/jobs/"+second.ID, nil, 404, nil)
	if code := errorCodeOf(t, data); code != string(codeApplicationNotFound) && code != string(codeJobNotFound) {
		t.Fatal(code)
	}
	var jobs int
	if err := h.server.store.DB.QueryRow(`SELECT count(*) FROM prewarm_jobs WHERE app_uid=?`, app.UID).Scan(&jobs); err != nil || jobs != 0 {
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
	h := newHarness(t)
	h.login(h.password)
	if err := h.server.pool.SetProxy(distributor.ProxyUpdate{Mode: "url", URL: proxy.URL}, h.server.pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	warmApp(h, "codex", "http://warm.example")
	set := func(enabled bool) {
		h.patchApp("warm/app", map[string]any{"prewarm": map[string]any{"enabled": enabled, "channels": []string{"latest"}, "platforms": []string{"linux-x64", "win32-x64"}}})
	}
	set(true)
	service := h.server.prewarmer
	job, created, err := service.Start(context.Background(), "warm/app", warmplan.Input{RequestID: strings.Repeat("a", 32), Target: "latest", Platforms: []string{"linux-x64", "win32-x64"}}, true)
	if err != nil || !created {
		t.Fatal(created, err)
	}
	<-started
	set(false)
	close(release)
	done := warmJob(t, h, job.ID)
	if done.State != "cancelled" || done.Completed != 1 || !done.Automatic || other.Load() != 0 {
		t.Fatal(done, other.Load())
	}
	var successes int
	if err := h.server.store.DB.QueryRow(`SELECT count(*) FROM prewarm_success`).Scan(&successes); err != nil || successes != 0 {
		t.Fatal(successes, err)
	}
}
