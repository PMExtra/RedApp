package httpserver

import (
	"bytes"
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/store"
	"strings"
	"testing"
)

func TestTaxonomyCRUDPublicPrivacyAndRecommendations(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	h.request("GET", "/admin/api/taxonomy", nil, 401, nil)
	h.login(h.password)
	taxonomy := func(kind, id string) store.TaxonomyItem {
		raw, _ := h.request("POST", "/admin/api/taxonomy/"+kind, map[string]any{"id": id, "name": map[string]string{"en": id, "zh-CN": "中文" + id}}, 200, nil)
		var item store.TaxonomyItem
		json.Unmarshal(raw, &item)
		return item
	}
	cat := taxonomy("categories", "tools")
	tag := taxonomy("tags", "cli")
	taxonomy("categories", "private-only")
	h.request("POST", "/admin/api/taxonomy/tags", map[string]any{"id": "bad", "name": map[string]string{"en": "English"}}, 400, nil)
	h.request("PATCH", "/admin/api/taxonomy/tags/cli", map[string]any{"revision": tag.Revision, "set": map[string]any{"id": "new"}}, 400, nil)
	oldCSRF := h.csrf
	h.csrf = ""
	h.request("POST", "/admin/api/taxonomy/tags", map[string]any{"id": "bad", "name": tag.Name}, 403, nil)
	h.csrf = oldCSRF
	h.request("POST", "/admin/api/vendors", map[string]any{"id": "taxonomy", "name": tag.Name, "enabled": true}, 201, nil)
	create := func(id, category string, enabled bool) {
		h.request("POST", "/admin/api/vendors/taxonomy/apps", map[string]any{"id": id, "provider": "info", "name": map[string]string{"en": "Search " + id, "zh-CN": "中文"}, "enabled": enabled, "category": category, "tags": []string{"cli"}}, 201, nil)
	}
	create("own", "tools", true)
	create("peer", "tools", true)
	create("disabled", "private-only", false)
	before, _ := h.server.DB.Application("taxonomy/own")
	raw, _ := h.request("GET", "/api/bootstrap", nil, 200, nil)
	var bootstrapBefore map[string]any
	json.Unmarshal(raw, &bootstrapBefore)
	h.request("PATCH", "/admin/api/taxonomy/categories/tools", map[string]any{"revision": cat.Revision, "set": map[string]any{"name.en": "Tools renamed"}}, 200, nil)
	after, _ := h.server.DB.Application("taxonomy/own")
	if after.Revision != before.Revision || after.RuntimeRevision != before.RuntimeRevision || after.SourceEpoch != before.SourceEpoch {
		t.Fatal("dictionary rename touched app")
	}
	raw, _ = h.request("GET", "/api/bootstrap", nil, 200, nil)
	var bootstrapAfter map[string]any
	json.Unmarshal(raw, &bootstrapAfter)
	if bootstrapAfter["revision"] == bootstrapBefore["revision"] {
		t.Fatal("public revision unchanged")
	}
	for _, path := range []string{"/api/catalog?category=tools&q=Search&limit=1&page=2", "/api/apps/taxonomy/own/related", "/api/bootstrap"} {
		raw, _ = h.request("GET", path, nil, 200, nil)
		for _, secret := range []string{"overrides", "defaults", "proxy_effective", "source_epoch", "base_url", "template_ref", "builtin", "references"} {
			if bytes.Contains(raw, []byte(`"`+secret+`"`)) {
				t.Fatal("private projection", path, secret)
			}
		}
		if strings.Contains(string(raw), "private-only") || strings.Contains(string(raw), "taxonomy/disabled") {
			t.Fatal("disabled data leaked", path)
		}
	}
	raw, _ = h.request("GET", "/api/catalog?category=tools&q=Search&limit=1&page=2", nil, 200, nil)
	var result struct {
		Items      []map[string]any
		Total      int
		Categories []store.TaxonomyLabel
	}
	json.Unmarshal(raw, &result)
	if result.Total != 2 || len(result.Items) != 1 || result.Items[0]["id"] != "taxonomy/peer" || len(result.Categories) != 1 || result.Categories[0].Name.En != "Tools renamed" {
		t.Fatal(string(raw))
	}
	raw, _ = h.request("GET", "/api/apps/taxonomy/own/related", nil, 200, nil)
	if !bytes.Contains(raw, []byte(`"id":"taxonomy/peer"`)) || bytes.Contains(raw, []byte(`"id":"taxonomy/own"`)) {
		t.Fatal(string(raw))
	}
	raw, _ = h.request("DELETE", "/admin/api/taxonomy/tags/cli", map[string]any{"revision": tag.Revision}, 409, nil)
	if !bytes.Contains(raw, []byte(`"references":3`)) {
		t.Fatal(string(raw))
	}
	h.request("DELETE", "/admin/api/taxonomy/categories/tools", map[string]any{"revision": cat.Revision}, 409, nil)
	h.request("GET", "/api/catalog?category=bad/slug", nil, 400, nil)
	h.request("GET", "/admin/api/taxonomy?kind=unknown", nil, 400, nil)
}
