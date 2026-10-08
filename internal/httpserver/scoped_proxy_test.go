package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/store"
)

func TestScopedProxiesRouteCatalogArtifactsHTTPAndHostedImports(t *testing.T) {
	dir := t.TempDir()
	h := newDirectoryHarness(t, dir)
	h.login(h.password)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "direct") }))
	defer origin.Close()
	var mu sync.Mutex
	hits := map[string][]string{}
	body := []byte("artifact")
	digest := sha256.Sum256(body)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	proxy := func(id string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			hits[id] = append(hits[id], r.URL.Path)
			mu.Unlock()
			if strings.HasSuffix(r.URL.Path, "/release.json") || strings.HasSuffix(r.URL.Path, "/channels/latest") {
				version := "1.2.3"
				parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
				if len(parts) >= 3 && parts[len(parts)-3] == "releases" {
					version = parts[len(parts)-2]
				}
				json.NewEncoder(w).Encode(codex.Release{Tag: "rust-v" + version, Assets: []codex.Asset{{Name: "asset.tgz", Digest: "sha256:" + hex.EncodeToString(digest[:]), URL: origin.URL + "/releases/" + version + "/asset.tgz"}}})
				return
			}
			if strings.HasSuffix(r.URL.Path, "/asset.tgz") {
				w.Header().Set("Content-Length", fmt.Sprint(len(body)))
				w.Write(body[:2])
				w.(http.Flusher).Flush()
				close(started)
				<-release
				w.Write(body[2:])
				return
			}
			io.WriteString(w, id)
		}))
	}
	first, second := proxy("first"), proxy("second")
	defer first.Close()
	defer second.Close()
	h.request("POST", "/admin/api/vendors", map[string]any{"id": "routing", "name": map[string]string{"en": "Routing", "zh-CN": "路由"}, "enabled": true}, 201, nil)
	for _, entry := range []struct{ id, provider string }{{"release", "codex"}, {"cache", "http-cache"}, {"hosted", "hosted"}} {
		input := map[string]any{"id": entry.id, "provider": entry.provider, "name": map[string]string{"en": entry.id, "zh-CN": entry.id}, "enabled": true}
		if entry.provider != "hosted" {
			input["base_url"] = origin.URL
			input["cache_ttl_seconds"] = 60
			if entry.provider == "http-cache" {
				input["cache_ttl_seconds"] = 0
			}
		}
		h.request("POST", "/admin/api/vendors/routing/apps", input, 201, nil)
	}
	if err := h.server.Pool.SetProxy(distributor.ProxyUpdate{Mode: "url", URL: first.URL}, h.server.Pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	// App direct overrides the vendor proxy, while its sibling inherits it.
	vendor, _ := h.server.DB.Vendor("routing")
	if _, err := h.server.DB.PatchVendorConfiguration(vendor.ID, store.ConfigurationPatch{Revision: vendor.Revision, Set: map[string]json.RawMessage{"proxy": encodeJSON(networkproxy.Config{Mode: "url", URL: strings.Replace(second.URL, "://", "://fixture:private-proxy-secret@", 1)})}}); err != nil {
		t.Fatal(err)
	}
	app, _ := h.server.DB.Application("routing/cache")
	if _, err := h.server.DB.PatchApplicationConfiguration(app.Key, store.ConfigurationPatch{Revision: app.Revision, Set: map[string]json.RawMessage{"proxy": encodeJSON(networkproxy.Direct())}}); err != nil {
		t.Fatal(err)
	}
	data, _ := h.request("GET", "/routing/cache/direct-file", nil, 200, nil)
	if string(data) != "direct" {
		t.Fatal("app direct did not override vendor", string(data))
	}
	if _, err := h.server.Catalog.Release(context.Background(), "routing/release", "latest"); err != nil {
		t.Fatal(err)
	}
	h.request("POST", "/admin/api/apps/routing/hosted/files/import?transfer_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", map[string]any{"path": "import", "url": origin.URL + "/import"}, 201, nil)
	data, _ = h.request("GET", "/routing/hosted/import", nil, 200, nil)
	if string(data) != "second" {
		t.Fatal("Hosted import missed app/vendor scope")
	}
	// Remove the vendor override: the inherited release client now uses global A.
	vendor, _ = h.server.DB.Vendor(vendor.ID)
	if _, err := h.server.DB.PatchVendorConfiguration(vendor.ID, store.ConfigurationPatch{Revision: vendor.Revision, Set: map[string]json.RawMessage{"proxy": encodeJSON(networkproxy.Inherit())}}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		response, err := h.http.Client().Get(h.http.URL + "/routing/release/releases/2.0.0/asset.tgz")
		if err == nil {
			defer response.Body.Close()
			got, e := io.ReadAll(response.Body)
			err = e
			if !bytes.Equal(got, body) {
				err = fmt.Errorf("body interrupted")
			}
		}
		done <- err
	}()
	<-started
	before, _ := h.server.DB.Application("routing/release")
	if _, err := h.server.DB.PatchApplicationConfiguration(before.Key, store.ConfigurationPatch{Revision: before.Revision, Set: map[string]json.RawMessage{"name.en": encodeJSON("Edited during download")}}); err != nil {
		t.Fatal(err)
	}
	if err := h.server.Pool.SetProxy(distributor.ProxyUpdate{Mode: "url", URL: strings.Replace(second.URL, "://", "://fixture:private-proxy-secret@", 1)}, h.server.Pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	published := false
	for _, view := range h.server.Downloads.Snapshot() {
		if view.Resource.Application == before.StorageID() && view.Resource.Version == "2.0.0" && view.Current && !view.Retired && view.State == "complete" {
			published = true
		}
	}
	if !published {
		t.Fatal("metadata/proxy retired or prevented publication of admitted writer")
	}
	after, _ := h.server.DB.Application(before.Key)
	if after.RuntimeRevision != before.RuntimeRevision || after.SourceEpoch != before.SourceEpoch {
		t.Fatal("metadata/proxy invalidated active writer")
	}
	if _, err := h.server.Catalog.Release(context.Background(), before.Key, "3.0.0"); err != nil {
		t.Fatal(err)
	}
	app, _ = h.server.DB.Application("routing/cache")
	if _, err := h.server.DB.PatchApplicationConfiguration(app.Key, store.ConfigurationPatch{Revision: app.Revision, Set: map[string]json.RawMessage{"proxy": encodeJSON(networkproxy.Inherit())}}); err != nil {
		t.Fatal(err)
	}
	data, _ = h.request("GET", "/routing/cache/next-file", nil, 200, nil)
	if string(data) != "second" {
		t.Fatal("HTTP cache missed new inherited proxy", string(data))
	}
	// Global failed CAS and DB write failures preserve both persistence and routing.
	savedProxy := h.server.Pool.Proxy()
	invalid, _ := h.request("PUT", "/admin/api/settings/proxy", map[string]any{"mode": "url", "url": "http://fixture:private-proxy-secret@proxy.example"}, 400, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, savedProxy.Revision)})
	if bytes.Contains(invalid, []byte("private-proxy-secret")) {
		t.Fatal("proxy credential error leak")
	}
	if err := h.server.Pool.SetProxy(distributor.ProxyUpdate{Mode: "url", URL: first.URL}, savedProxy.Revision-1); err == nil {
		t.Fatal("stale global CAS accepted")
	}
	if _, err := h.server.DB.DB.Exec(`CREATE TRIGGER reject_global_proxy BEFORE UPDATE ON settings WHEN NEW.key='upstream_proxy' BEGIN SELECT RAISE(FAIL,'injected write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := h.server.Pool.SetProxy(distributor.ProxyUpdate{Mode: "url", URL: first.URL}, savedProxy.Revision); err == nil {
		t.Fatal("failed global write accepted")
	}
	if h.server.Pool.Proxy() != savedProxy {
		t.Fatal("failed global write changed transport view")
	}
	h.server.DB.DB.Exec(`DROP TRIGGER reject_global_proxy`)
	// Administrative proxy credentials do not enter bootstrap, directory or events.
	for _, path := range []string{"/api/bootstrap", "/api/vendors/routing", "/api/apps/routing/release"} {
		raw, _ := h.request("GET", path, nil, 200, nil)
		if bytes.Contains(raw, []byte("private-proxy-secret")) {
			t.Fatal("public credential leak", path)
		}
	}
	events, err := h.server.DB.EventsFor(after.MetricsID())
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(events)
	if bytes.Contains(encoded, []byte("private-proxy-secret")) {
		t.Fatal("proxy credential event leak")
	}
	// A deleted app and a forged vendor scope cannot fall back to global routing.
	scoped, err := h.server.Pool.NewScopedClient(origin.URL, distributor.GeneralHTTP, after.UID, "ffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	if response, err := scoped.Get(context.Background(), origin.URL+"/wrong-scope", nil); err == nil {
		response.Body.Close()
		t.Fatal("wrong vendor scope contacted network")
	}
	app, _ = h.server.DB.Application("routing/cache")
	deletedClient, err := h.server.Pool.NewScopedClient(origin.URL, distributor.GeneralHTTP, app.UID, app.VendorUID)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.server.DB.DeleteApplication(app.Key, app.Revision); err != nil {
		t.Fatal(err)
	}
	if response, err := deletedClient.Get(context.Background(), origin.URL+"/deleted", nil); err == nil {
		response.Body.Close()
		t.Fatal("deleted app contacted network")
	}
	// Restart reloads the same independent/inherited proxy settings and fences.
	h.close()
	h = newDirectoryHarness(t, dir)
	if h.server.Pool.Proxy() != savedProxy {
		t.Fatal("restart changed global proxy")
	}
	restarted, _ := h.server.DB.ApplicationConfiguration("routing/release")
	if restarted.ProxyEffective.Mode != "url" || restarted.ProxyEffective.URL != savedProxy.URL || restarted.ProxyEffective.SourceScope != "global" {
		t.Fatal("restart lost inheritance")
	}
	if _, err := h.server.Catalog.Release(context.Background(), "routing/release", "4.0.0"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(strings.Join(hits["first"], ","), "/asset.tgz") || !strings.Contains(strings.Join(hits["second"], ","), "/channels/latest") || !strings.Contains(strings.Join(hits["second"], ","), "/releases/3.0.0/release.json") || !strings.Contains(strings.Join(hits["second"], ","), "/import") || !strings.Contains(strings.Join(hits["second"], ","), "/next-file") {
		t.Fatal("routing coverage", hits)
	}
}
