package httpserver

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/presets"
)

func TestVendorLocalizedIconsPublicProjectionResetAndRestart(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, withDir(dir))
	password := h.password
	h.login(password)
	en, zh := h.uploadIcon(svgIcon("red"), 201), h.uploadIcon(svgIcon("blue"), 201)
	base := presets.ImagePrefix + "builtin/openai.svg"
	h.request("PUT", "/admin/api/vendors/openai/admin-notes", map[string]any{"text": "keep private note"}, 200, ifMatchHeader(1))
	expectCode(t, h.patchConfiguration("vendors/openai", map[string]any{"set": map[string]any{"localized_icons.en": "https://untrusted.example/logo.svg"}}, 400), codeValidationFailed)
	expectCode(t, h.patchConfiguration("vendors/openai", map[string]any{"set": map[string]any{"localized_icons.en": "/assets/icons/" + strings.Repeat("f", 64) + ".svg"}}, 400), codeValidationFailed)
	expectCode(t, h.patchConfiguration("apps/openai/codex", map[string]any{"set": map[string]any{"localized_icons.en": en}}, 400), codeInvalidRequest)
	h.patchConfiguration("vendors/openai", map[string]any{"set": map[string]any{"icon": base, "localized_icons.en": en, "localized_icons.zh-CN": zh}}, 200)
	v := h.adminVendor("openai")
	if v.Icon != base || v.LocalizedIcons != (localizedText{En: en, ZhCN: zh}) {
		t.Fatal(v)
	}
	// Published surfaces expose the vendor logos, never private notes.
	for _, path := range []string{"/api/vendors/openai", "/api/search?q=OpenAI", "/api/apps/openai/codex", "/api/catalog"} {
		raw, _ := h.request("GET", path, nil, 200, nil)
		if !bytes.Contains(raw, []byte(en)) || !bytes.Contains(raw, []byte(zh)) || bytes.Contains(raw, []byte("keep private note")) {
			t.Fatal(path, string(raw))
		}
	}
	if raw, _ := h.request("GET", "/admin/api/apps/openai/codex", nil, 200, nil); bytes.Contains(raw, []byte("localized_icons")) {
		t.Fatal("application document has vendor logos")
	}
	h.setEnabled("vendors/openai", false)
	if v = h.adminVendor("openai"); v.LocalizedIcons.En != en || v.LocalizedIcons.ZhCN != zh {
		t.Fatal("toggle overwrote logos")
	}
	h.close()
	h = newHarness(t, withDir(dir))
	h.login(password)
	if v = h.adminVendor("openai"); v.Icon != base || v.LocalizedIcons.En != en || v.LocalizedIcons.ZhCN != zh || v.Enabled {
		t.Fatal("restart", v)
	}
	h.patchConfiguration("vendors/openai", map[string]any{"set": map[string]any{"localized_icons.en": ""}}, 200)
	if v = h.adminVendor("openai"); v.LocalizedIcons.En != "" || v.LocalizedIcons.ZhCN != zh || v.Icon != base {
		t.Fatal("clear changed the default or the other language", v)
	}
	h.patchConfiguration("vendors/openai", map[string]any{"unset": []string{"icon", "localized_icons.en", "localized_icons.zh-CN"}}, 200)
	if v = h.adminVendor("openai"); v.Icon != base || v.LocalizedIcons != (localizedText{}) || v.Enabled {
		t.Fatal("reset", v)
	}
	if notes := getJSON[adminNotesDTO](h, "/admin/api/vendors/openai/admin-notes"); notes.Text != "keep private note" || notes.Revision != 2 {
		t.Fatal("configuration reset changed notes", notes)
	}
	h.patchConfiguration("vendors/openai", map[string]any{"set": map[string]any{"icon": "", "localized_icons.en": "", "localized_icons.zh-CN": ""}}, 200)
	h.close()
	h = newHarness(t, withDir(dir))
	h.login(password)
	if v = h.adminVendor("openai"); v.Icon != "" || v.LocalizedIcons != (localizedText{}) {
		t.Fatal("clear reverted on restart", v)
	}
}
