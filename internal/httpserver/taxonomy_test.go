package httpserver

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCategoriesTagsPublicPrivacyAndSearch(t *testing.T) {
	h := newHarness(t)
	h.expectError("GET", "/admin/api/categories", nil, 401, codeAuthRequired, nil)
	h.login("")
	h.request("POST", "/admin/api/vendors", map[string]any{"id": "taxonomy", "name": map[string]string{"en": "Taxonomy", "zh-CN": "分类"}, "enabled": true}, 201, nil)
	for _, app := range []struct {
		id      string
		enabled bool
	}{{"own", true}, {"peer", true}, {"disabled", false}} {
		h.request("POST", "/admin/api/apps", map[string]any{"vendor": "taxonomy", "id": app.id, "provider": "info", "name": map[string]string{"en": "Search " + app.id, "zh-CN": "中文"}, "enabled": app.enabled}, 201, nil)
	}
	// New categories are typed names saved with the application in one request.
	h.patchConfiguration("apps/taxonomy/own", map[string]any{"set": map[string]any{"categories": []string{}, "tags": []string{"#Private Tag", "private tag"}}, "new_categories": []string{"Tools"}}, 200)
	h.patchConfiguration("apps/taxonomy/peer", map[string]any{"set": map[string]any{"categories": []string{"tools", "tools"}}}, 200)
	h.patchConfiguration("apps/taxonomy/disabled", map[string]any{"set": map[string]any{"categories": []string{"tools"}}, "new_categories": []string{"Private only"}}, 200)
	expectCode(t, h.patchConfiguration("apps/taxonomy/peer", map[string]any{"set": map[string]any{"categories": []string{"unknown"}}}, 400), codeValidationFailed)
	if peer := h.adminApp("taxonomy/peer"); strings.Join(peer.Categories, ",") != "tools" {
		t.Fatal("categories not normalized", peer.Categories)
	}
	if own := h.adminApp("taxonomy/own"); strings.Join(own.Tags, ",") != "Private Tag" || strings.Join(own.Categories, ",") != "tools" {
		t.Fatal("tags not normalized", own.Tags, own.Categories)
	}
	listing := getJSON[pageDTO[categoryDTO]](h, "/admin/api/categories?q=o")
	if listing.Total != 2 || listing.Limit != 25 || listing.Items[0].ID != "private-only" || listing.Items[0].Applications != 1 || listing.Items[1].ID != "tools" || listing.Items[1].Applications != 3 {
		t.Fatal("category listing", listing)
	}
	data, headers := h.request("GET", "/admin/api/categories/tools", nil, 200, nil)
	tools := decodeJSONBody[categoryDTO](t, data)
	if tools.Builtin || tools.Defaults != nil || tools.TemplateRef != nil || tools.Name.En != "Tools" || headers.Get("ETag") != etag(tools.Revision) || tools.Fields["name.en"].Source != "custom" || bytes.Contains(data, []byte(`"kind"`)) {
		t.Fatal("category document", string(data))
	}
	h.expectError("GET", "/admin/api/categories/missing", nil, 404, codeCategoryNotFound, nil)
	h.expectError("GET", "/admin/api/categories/Bad_ID", nil, 400, codeInvalidPath, nil)
	bootstrapBefore := getJSON[bootstrapDTO](h, "/api/bootstrap")
	path := "/admin/api/categories/tools"
	h.expectError("PATCH", path, map[string]any{"set": map[string]any{"id": "new"}}, 400, codeInvalidRequest, ifMatchHeader(tools.Revision))
	h.expectError("PATCH", path, map[string]any{"set": map[string]any{"name.en": "Blocked"}}, 400, codeIfMatchRequired, nil)
	h.expectError("PATCH", path, map[string]any{"set": map[string]any{"name.en": "Blocked"}}, 409, codeRevisionConflict, ifMatchHeader(tools.Revision+1))
	h.expectError("PATCH", path, map[string]any{"unset": []string{"name.en"}}, 400, codeValidationFailed, ifMatchHeader(tools.Revision))
	h.expectError("PATCH", path, map[string]any{"set": map[string]any{"name.en": "PRIVATE ONLY"}}, 400, codeValidationFailed, ifMatchHeader(tools.Revision))
	h.expectError("PATCH", path, map[string]any{"set": map[string]any{"name.en": ""}}, 400, codeValidationFailed, ifMatchHeader(tools.Revision))
	h.expectError("PATCH", "/admin/api/categories/missing", map[string]any{"set": map[string]any{"name.en": "x"}}, 404, codeCategoryNotFound, ifMatchHeader(1))
	own := h.adminApp("taxonomy/own")
	data, _ = h.request("PATCH", path, map[string]any{"set": map[string]any{"name.en": "Tools renamed"}}, 200, ifMatchHeader(tools.Revision))
	if renamed := decodeJSONBody[categoryDTO](t, data); renamed.Revision != tools.Revision+1 || renamed.Name.En != "Tools renamed" || renamed.Applications != 3 {
		t.Fatal("rename", string(data))
	}
	if after := h.adminApp("taxonomy/own"); after.Revision != own.Revision || after.SourceEpoch != own.SourceEpoch {
		t.Fatal("category rename touched the application")
	}
	if getJSON[bootstrapDTO](h, "/api/bootstrap").Revision == bootstrapBefore.Revision {
		t.Fatal("public revision unchanged after a rename")
	}
	// Category creation and deletion only happen through applications.
	for _, method := range []string{"POST", "DELETE"} {
		if code, _, _ := h.raw(method, "/admin/api/categories/tools", nil, "", nil); code < 400 {
			t.Fatal("category", method, code)
		}
	}
	for _, public := range []string{"/api/catalog?category=tools&q=Search&limit=1&page=2", "/api/catalog", "/api/bootstrap", "/api/search?q=private", "/api/home"} {
		raw, _ := h.request("GET", public, nil, 200, nil)
		for _, secret := range []string{"overrides", "defaults", "proxy_effective", "source_epoch", "base_url", "template_ref", "builtin", "tags"} {
			if bytes.Contains(raw, []byte(`"`+secret+`"`)) {
				t.Fatal("private projection", public, secret)
			}
		}
		if bytes.Contains(raw, []byte("Private Tag")) || bytes.Contains(raw, []byte("private-only")) || bytes.Contains(raw, []byte("taxonomy/disabled")) {
			t.Fatal("private or disabled data published", public)
		}
	}
	var result struct {
		Items      []map[string]any   `json:"items"`
		Total      int                `json:"total"`
		Categories []categoryCountDTO `json:"categories"`
	}
	raw, _ := h.request("GET", "/api/catalog?category=tools&q=Search&limit=1&page=2", nil, 200, nil)
	if json.Unmarshal(raw, &result) != nil || result.Total != 2 || len(result.Items) != 1 || result.Items[0]["key"] != "taxonomy/peer" || len(result.Categories) != 1 || result.Categories[0].Name.En != "Tools renamed" || result.Categories[0].Count != 2 {
		t.Fatal(string(raw))
	}
	// Counts are site-wide; tag search still finds the application.
	raw, _ = h.request("GET", "/api/catalog?q=%23private", nil, 200, nil)
	if json.Unmarshal(raw, &result) != nil || result.Total != 1 || result.Items[0]["key"] != "taxonomy/own" || result.Categories[0].Count != 2 {
		t.Fatal("tag search or site-wide counts", string(raw))
	}
	// Names that older data made ambiguous must be chosen explicitly.
	if _, err := h.sql().Exec(`UPDATE categories SET name_zh_cn='Tools renamed' WHERE id='private-only'`); err != nil {
		t.Fatal(err)
	}
	expectCode(t, h.patchConfiguration("apps/taxonomy/peer", map[string]any{"set": map[string]any{"categories": []string{}}, "new_categories": []string{"tools RENAMED"}}, 409), codeCategoryAmbiguous)
}
