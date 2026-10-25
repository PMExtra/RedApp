package httpserver

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/site"
)

func TestSiteSettingsRequireRevisionAndPublishPlainText(t *testing.T) {
	h := newHarness(t)
	h.expectError("GET", "/admin/api/settings/site", nil, 401, codeAuthRequired, nil)
	h.login("")
	body, headers := h.request("GET", "/admin/api/settings/site", nil, 200, nil)
	initial := decodeJSONBody[siteSettingsStateDTO](t, body)
	defaults := site.Defaults()
	if initial.Revision != 1 || headers.Get("ETag") != `"1"` || initial.Title.En != defaults.Title.EN {
		t.Fatal("initial site settings", string(body), headers)
	}
	text := func(en, zh string) map[string]string { return map[string]string{"en": en, "zh-CN": zh} }
	input := map[string]any{"title": text("  <svg onload=alert(1)>  ", "工具"), "subtitle": text("", ""), "disclaimer": text("Custom notice", "自定义说明")}
	h.expectError("PUT", "/admin/api/settings/site", input, 400, codeIfMatchRequired, nil)
	h.expectError("PUT", "/admin/api/settings/site", input, 400, codeIfMatchRequired, map[string]string{"If-Match": "1"})
	h.expectError("PUT", "/admin/api/settings/site", input, 403, codeCSRFRejected, map[string]string{"If-Match": `"1"`, "X-CSRF-Token": ""})
	body, headers = h.request("PUT", "/admin/api/settings/site", input, 200, map[string]string{"If-Match": `"1"`})
	saved := decodeJSONBody[siteSettingsStateDTO](t, body)
	if saved.Revision != 2 || headers.Get("ETag") != `"2"` || saved.Title.En != "<svg onload=alert(1)>" || saved.Subtitle.En != "" {
		t.Fatal("saved site settings were not trimmed or revisioned", string(body))
	}
	h.expectError("PUT", "/admin/api/settings/site", input, 409, codeRevisionConflict, map[string]string{"If-Match": `"1"`})
	for _, invalid := range []map[string]any{
		{"title": text("", "工具"), "subtitle": text("", ""), "disclaimer": text("", "")},
		{"title": text(strings.Repeat("x", 81), "工具"), "subtitle": text("", ""), "disclaimer": text("", "")},
		{"title": text("Tools", "工具"), "subtitle": text("bell\a", ""), "disclaimer": text("", "")},
	} {
		h.expectError("PUT", "/admin/api/settings/site", invalid, 400, codeValidationFailed, map[string]string{"If-Match": `"2"`})
	}
	for _, invalid := range []map[string]any{
		{"title": text("Tools", "工具")},
		{"title": map[string]string{"en": "Tools"}, "subtitle": text("", ""), "disclaimer": text("", "")},
		{"title": text("Tools", "工具"), "subtitle": text("", ""), "disclaimer": text("", ""), "revision": 2},
		{"title": text("Tools", "工具"), "subtitle": map[string]any{"en": false, "zh-CN": ""}, "disclaimer": text("", "")},
	} {
		h.expectError("PUT", "/admin/api/settings/site", invalid, 400, codeInvalidRequest, map[string]string{"If-Match": `"2"`})
	}
	// Public pages receive the texts as data, never as markup.
	body, headers = h.request("GET", "/api/bootstrap", nil, 200, nil)
	var info struct {
		Site struct {
			Title localizedText `json:"title"`
		} `json:"site"`
	}
	if json.Unmarshal(body, &info) != nil || info.Site.Title.En != "<svg onload=alert(1)>" || headers.Get("Cache-Control") != "no-store" || headers.Get("Set-Cookie") != "" {
		t.Fatal("public site texts", string(body))
	}
}

func TestNavigationPublishedAndRejectsInvalidInputs(t *testing.T) {
	h := newHarness(t)
	h.login("")
	input := map[string]any{"title": site.Text{EN: "Mirror", ZHCN: "镜像"}, "subtitle": site.Text{}, "disclaimer": site.Text{}, "navigation": []site.NavigationItem{{Title: "Archive", LocalizedTitle: &site.Text{EN: "Archive", ZHCN: "旧版资源"}, URL: "/legacy/", NewTab: true}}}
	body, _ := h.request("PUT", "/admin/api/settings/site", input, 200, map[string]string{"If-Match": `"1"`})
	saved := decodeJSONBody[siteSettingsStateDTO](t, body)
	body, _ = h.request("GET", "/api/bootstrap", nil, 200, nil)
	public := decodeJSONBody[bootstrapDTO](t, body)
	if len(public.Site.Navigation) != 1 || !reflect.DeepEqual(public.Site.Navigation[0], saved.Navigation[0]) {
		t.Fatal("navigation missing from bootstrap")
	}
	h.expectError("PUT", "/admin/api/settings/site", input, 409, codeRevisionConflict, map[string]string{"If-Match": `"1"`})
	for _, invalid := range []any{nil, []any{map[string]any{"title": "a", "url": "/"}}, []any{map[string]any{"title": "a", "url": "/", "new_tab": false, "extra": true}}} {
		input["navigation"] = invalid
		h.expectError("PUT", "/admin/api/settings/site", input, 400, codeInvalidRequest, map[string]string{"If-Match": `"2"`})
	}
	for _, localized := range []any{nil, map[string]any{"en": "Docs"}, map[string]any{"en": "Docs", "zh-CN": nil}, map[string]any{"en": "Docs", "zh-CN": "文档", "extra": true}} {
		input["navigation"] = []any{map[string]any{"title": "Docs", "url": "/", "new_tab": false, "localized_title": localized}}
		h.expectError("PUT", "/admin/api/settings/site", input, 400, codeInvalidRequest, map[string]string{"If-Match": `"2"`})
	}
	input["navigation"] = []site.NavigationItem{{Title: "Unsafe", URL: "//evil.test"}}
	h.expectError("PUT", "/admin/api/settings/site", input, 400, codeValidationFailed, map[string]string{"If-Match": `"2"`})
}
