package httpserver

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/store"
)

func TestCategoriesTagsPublicPrivacyAndSearch(t *testing.T) {
	h := newHarness(t)
	h.request("GET", "/admin/api/categories", nil, 401, nil)
	h.login(h.password)
	h.request("POST", "/admin/api/vendors", map[string]any{"id": "taxonomy", "name": map[string]string{"en": "Taxonomy", "zh-CN": "分类"}, "enabled": true}, 201, nil)
	create := func(id string, enabled bool) {
		h.request("POST", "/admin/api/vendors/taxonomy/apps", map[string]any{"id": id, "provider": "info", "name": map[string]string{"en": "Search " + id, "zh-CN": "中文"}, "enabled": enabled}, 201, nil)
	}
	create("own", true)
	create("peer", true)
	create("disabled", false)
	save := func(key string, body map[string]any, status int) []byte {
		raw, _ := h.request("GET", "/admin/api/apps/"+key+"/configuration", nil, 200, nil)
		body["revision"] = configurationValue(t, raw).Revision
		raw, _ = h.request("PATCH", "/admin/api/apps/"+key+"/configuration", body, status, nil)
		return raw
	}
	// New categories are typed names saved with the App in one request.
	save("taxonomy/own", map[string]any{"set": map[string]any{"categories": []string{}, "tags": []string{"#Private Tag", "private tag"}}, "new_categories": []string{"Tools"}}, 200)
	// The entity PATCH accepts the categories set; the removed single-value field is rejected.
	peer, _ := h.server.store.Application("taxonomy/peer")
	h.request("PATCH", "/admin/api/apps/taxonomy/peer", map[string]any{"revision": peer.Revision, "category": "tools"}, 400, nil)
	h.request("PATCH", "/admin/api/apps/taxonomy/peer", map[string]any{"revision": peer.Revision, "categories": []string{"tools", "tools"}}, 200, nil)
	if peer, _ = h.server.store.Application("taxonomy/peer"); strings.Join(peer.Categories, ",") != "tools" {
		t.Fatal("entity PATCH categories", peer.Categories)
	}
	save("taxonomy/disabled", map[string]any{"set": map[string]any{"categories": []string{"tools"}}, "new_categories": []string{"Private only"}}, 200)
	save("taxonomy/peer", map[string]any{"set": map[string]any{"categories": []string{"unknown"}}}, 400)
	save("taxonomy/peer", map[string]any{"set": map[string]any{"name.en": "x"}, "new_categories": []string{"Orphan"}}, 400)
	raw, _ := h.request("GET", "/admin/api/categories?q=o", nil, 200, nil)
	var listing store.Page[store.CategoryListItem]
	json.Unmarshal(raw, &listing)
	if listing.Total != 2 || listing.Items[0].ID != "private-only" || listing.Items[0].Applications != 1 || listing.Items[1].ID != "tools" || listing.Items[1].Applications != 3 {
		t.Fatal("category listing", string(raw))
	}
	own, _ := h.server.store.Application("taxonomy/own")
	if strings.Join(own.Tags, ",") != "Private Tag" {
		t.Fatal("tags not normalized", own.Tags)
	}
	raw, _ = h.request("GET", "/api/bootstrap", nil, 200, nil)
	var bootstrapBefore map[string]any
	json.Unmarshal(raw, &bootstrapBefore)
	tools := listing.Items[1]
	h.request("PATCH", "/admin/api/categories/tools", map[string]any{"revision": tools.Revision, "set": map[string]any{"id": "new"}}, 400, nil)
	oldCSRF := h.csrf
	h.csrf = ""
	h.request("PATCH", "/admin/api/categories/tools", map[string]any{"revision": tools.Revision, "set": map[string]any{"name.en": "Blocked"}}, 403, nil)
	h.csrf = oldCSRF
	h.request("PATCH", "/admin/api/categories/tools", map[string]any{"revision": tools.Revision, "set": map[string]any{"name.en": "Tools renamed"}}, 200, nil)
	after, _ := h.server.store.Application("taxonomy/own")
	if after.Revision != own.Revision || after.RuntimeRevision != own.RuntimeRevision || after.SourceEpoch != own.SourceEpoch {
		t.Fatal("category rename touched app")
	}
	raw, _ = h.request("GET", "/api/bootstrap", nil, 200, nil)
	var bootstrapAfter map[string]any
	json.Unmarshal(raw, &bootstrapAfter)
	if bootstrapAfter["revision"] == bootstrapBefore["revision"] {
		t.Fatal("public revision unchanged")
	}
	// Removed endpoints: tag dictionary, category create/delete and recommendations.
	h.request("POST", "/admin/api/categories", map[string]any{"id": "x"}, 405, nil)
	h.request("DELETE", "/admin/api/categories/tools", map[string]any{"revision": 1}, 405, nil)
	h.request("GET", "/admin/api/taxonomy", nil, 404, nil)
	h.request("GET", "/admin/categories", nil, 200, nil)
	h.request("GET", "/admin/taxonomy", nil, 404, nil)
	h.request("GET", "/api/apps/taxonomy/own/related", nil, 404, nil)
	for _, path := range []string{"/api/catalog?category=tools&q=Search&limit=1&page=2", "/api/catalog", "/api/bootstrap", "/api/search?q=private", "/api/home"} {
		raw, _ = h.request("GET", path, nil, 200, nil)
		for _, secret := range []string{"overrides", "defaults", "proxy_effective", "source_epoch", "base_url", "template_ref", "builtin", "tags", "related"} {
			if bytes.Contains(raw, []byte(`"`+secret+`"`)) {
				t.Fatal("private projection", path, secret)
			}
		}
		if bytes.Contains(raw, []byte("Private Tag")) {
			t.Fatal("tag text published", path)
		}
		if strings.Contains(string(raw), "private-only") || strings.Contains(string(raw), "taxonomy/disabled") {
			t.Fatal("disabled data leaked", path)
		}
	}
	raw, _ = h.request("GET", "/api/catalog?category=tools&q=Search&limit=1&page=2", nil, 200, nil)
	var result struct {
		Items      []map[string]any
		Total      int
		Categories []store.CategoryCount
	}
	json.Unmarshal(raw, &result)
	if result.Total != 2 || len(result.Items) != 1 || result.Items[0]["key"] != "taxonomy/peer" || len(result.Categories) != 1 || result.Categories[0].Name.En != "Tools renamed" || result.Categories[0].Count != 2 {
		t.Fatal(string(raw))
	}
	// Counts are site-wide and do not follow the search text; tag search still finds the App.
	raw, _ = h.request("GET", "/api/catalog?q=%23private", nil, 200, nil)
	json.Unmarshal(raw, &result)
	if result.Total != 1 || result.Items[0]["key"] != "taxonomy/own" || len(result.Categories) != 1 || result.Categories[0].Count != 2 {
		t.Fatal("tag search or site-wide counts", string(raw))
	}
	raw, _ = h.request("GET", "/api/search?q=private", nil, 200, nil)
	if !bytes.Contains(raw, []byte(`"key":"taxonomy/own"`)) {
		t.Fatal("public suggestions ignore tags", string(raw))
	}
	h.request("GET", "/api/catalog?category=bad/slug", nil, 400, nil)
	h.request("GET", "/admin/api/categories?kind=categories", nil, 400, nil)
}
