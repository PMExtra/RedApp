package httpserver

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/store"
)

func TestContentProvidersInstructionsAndBackendCapabilityGates(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	h.createVendor("content")
	info := h.createApp("content", "about", "info", nil)
	hosted := h.createApp("content", "files", "hosted", nil)
	for _, a := range []store.Application{info, hosted} {
		var count int
		if err := h.server.store.DB.QueryRow(`SELECT COUNT(*) FROM application_sources WHERE app_uid=?`, a.UID).Scan(&count); err != nil || count != 0 {
			t.Fatal("content source created", count, err)
		}
		e, _ := h.server.registry.Lookup(a.Key)
		if e.Upstream != nil || len(e.Upstreams) != 0 || e.Protocol != nil {
			t.Fatal("content app has upstream", e)
		}
		for _, endpoint := range []string{"status", "sources", "versions", "resources", "cache", "settings"} {
			h.request("GET", "/admin/api/apps/"+a.Key+"/"+endpoint, nil, 404, nil)
		}
		for _, endpoint := range []string{"cleanup/preview", "cache/refresh/preview", "channel/refresh"} {
			h.request("POST", "/admin/api/apps/"+a.Key+"/"+endpoint, map[string]any{}, 404, nil)
		}
	}
	h.request("GET", "/content/about/file.zip", nil, 404, nil)
	h.request("GET", "/admin/api/apps/content/about/files", nil, 404, nil)
	h.request("POST", "/admin/api/apps", map[string]any{"vendor": "content", "id": "bad", "provider": "info", "name": store.LocalizedText{En: "Bad", ZhCN: "错误"}, "base_url": "https://example.com", "enabled": true}, 400, nil)
	// Usage instructions are configuration paths of the application.
	endpoint := "/admin/api/apps/" + info.Key + "/configuration"
	before, _ := h.publicCatalog()
	value := map[string]any{"set": map[string]any{"instructions.en": "<script>never execute</script>\nUse this app.", "instructions.zh-CN": "使用说明\n保留换行"}}
	h.request("PATCH", endpoint, value, 403, map[string]string{"X-CSRF-Token": "wrong", "If-Match": etag(info.Revision)})
	_, headers := h.request("PATCH", endpoint, value, 200, ifMatchHeader(info.Revision))
	if headers.Get("ETag") != etag(info.Revision+1) {
		t.Fatal(headers)
	}
	h.request("PATCH", endpoint, value, 409, ifMatchHeader(info.Revision))
	after, _ := h.publicCatalog()
	if before == after {
		t.Fatal("instructions did not invalidate public bootstrap")
	}
	data, _ := h.request("GET", "/api/apps/"+info.Key, nil, 200, nil)
	if app := decodeJSONBody[publicAppDTO](t, data); !app.InstructionsAvailable.En || !app.InstructionsAvailable.ZhCN {
		t.Fatal("public instructions flags", string(data))
	}
	if doc, _ := h.request("GET", "/api/apps/"+info.Key+"/instructions/document?lang=zh-CN", nil, 200, nil); !bytes.Contains(doc, []byte("保留换行")) {
		t.Fatal("instructions document lost the text", string(doc))
	}
	h.request("PATCH", endpoint, map[string]any{"set": map[string]any{"instructions.en": strings.Repeat("界", 12001)}}, 400, ifMatchHeader(info.Revision+1))
	if updated, _ := h.server.store.Application(info.Key); updated.Revision != info.Revision+1 || updated.SourceEpoch != info.SourceEpoch {
		t.Fatal("instructions changed application identity")
	}
}
func TestNumberedAPIInputValidation(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	for _, query := range []string{"page=0", "page=-1", "page=01", "page=1&page=2", "limit=101", "page=1000000001"} {
		h.request("GET", "/admin/api/vendors?"+query, nil, 400, nil)
	}
}
