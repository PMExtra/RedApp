package httpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
)

func TestInstanceIdentityAndLoopDetection(t *testing.T) {
	var via atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		via.Store(r.Header.Get("Via"))
		io.WriteString(w, "file")
	}))
	defer upstream.Close()
	h := newHarness(t)
	uid, err := h.store.InstanceUID()
	if err != nil || len(uid) != 32 {
		t.Fatal(uid, err)
	}
	instance := getJSON[instanceDTO](h, "/api/instance")
	if instance.InstanceID != uid || instance.Version == "" {
		t.Fatal(instance)
	}
	h.createVendor("acme")
	app := h.createApp("acme", "mirror", application.HttpCache, map[string]any{"base_url": upstream.URL + "/files"})
	h.patchApp(app.Key, map[string]any{"auto_index": true})
	self := distributor.ViaPseudonym(uid, app.UID)
	// Another application of this instance, or the instance alone, is no loop.
	for _, header := range []string{"1.1 redapp-0123456789abcdef0123456789abcdef", "1.1 " + distributor.ViaPseudonym(uid, ""), "1.1 " + distributor.ViaPseudonym(uid, strings.Repeat("a", 32))} {
		if body, _ := h.request("GET", "/acme/mirror/a.bin", nil, 200, map[string]string{"Via": header}); string(body) != "file" {
			t.Fatal(string(body))
		}
	}
	if via.Load() != "1.1 "+self {
		t.Fatal("upstream request does not name this instance and application", via.Load(), self)
	}
	for _, header := range []string{"1.1 " + self, "1.1 proxy.example, HTTP/1.1 " + self + " (RedApp)"} {
		h.expectError("GET", "/acme/mirror/b.bin", nil, 508, codeLoopDetected, map[string]string{"Via": header})
		h.expectError("GET", "/acme/mirror/", nil, 508, codeLoopDetected, map[string]string{"Via": header})
		h.expectError("GET", "/api/apps/acme/mirror/downloads", nil, 508, codeLoopDetected, map[string]string{"Via": header})
	}
	if code, _, _ := h.raw("HEAD", "/acme/mirror/b.bin", nil, "", map[string]string{"Via": "1.1 " + self}); code != 508 {
		t.Fatal("HEAD loop", code)
	}
}

// Applications of one instance may use each other as sources.
func TestApplicationsOfOneInstanceMayChain(t *testing.T) {
	h := newHarness(t)
	h.createVendor("acme")
	files := h.createApp("acme", "files", application.Hosted, nil)
	h.login("")
	h.upload(files.Key, "tool.bin", "", "tool", 201)
	h.createApp("acme", "cache", application.HttpCache, map[string]any{"base_url": h.http.URL + "/acme/files"})
	if body, _ := h.request("GET", "/acme/cache/tool.bin", nil, 200, nil); string(body) != "tool" {
		t.Fatal(string(body))
	}
}

func TestMirrorDescriptionOfAPublishedApplication(t *testing.T) {
	h := newHarness(t)
	h.createVendor("acme")
	a := h.createApp("acme", "files", application.Hosted, nil)
	h.patchApp(a.Key, map[string]any{"auto_index": true, "instructions.en": "Download from {{app_url}}/", "description.zh-CN": "文件"})
	uid, _ := h.store.InstanceUID()
	first := getJSON[mirrorAppDTO](h, "/api/apps/acme/files/mirror")
	if first.InstanceID != uid || first.Key != "acme/files" || first.Provider != "hosted" || first.Vendor.ID != "acme" || !first.AutoIndex ||
		first.Instructions.En != "Download from {{app_url}}/" || first.Description.ZhCN != "文件" || len(first.Channels) != 0 || len(first.UpstreamChain) != 0 || first.Revision == "" {
		t.Fatalf("%+v", first)
	}
	h.patchApp(a.Key, map[string]any{"instructions.zh-CN": "下载"})
	if second := getJSON[mirrorAppDTO](h, "/api/apps/acme/files/mirror"); second.Revision == first.Revision || second.Instructions.ZhCN != "下载" {
		t.Fatal("revision did not follow the instructions", second)
	}
	key := h.releaseApp("fixture", "codex", "codex")
	if codex := getJSON[mirrorAppDTO](h, "/api/apps/"+key+"/mirror"); codex.Provider != "codex" || strings.Join(codex.Channels, ",") != "latest" {
		t.Fatal("release channels", codex)
	}
	h.setAppEnabled(a.Key, false)
	h.expectError("GET", "/api/apps/acme/files/mirror", nil, 404, codeApplicationNotFound, nil)
}
