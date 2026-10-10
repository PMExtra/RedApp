package httpserver

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/presets"
)

func TestVendorLocalizedIconsPrivateWritesPublicProjectionResetAndRestart(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, withDir(dir))
	password := h.password
	h.login(password)
	upload := func(color string) string {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, err := form.CreateFormFile("file", "logo.svg")
		if err != nil {
			t.Fatal(err)
		}
		part.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 20"><rect width="100" height="20" fill="` + color + `"/></svg>`))
		form.Close()
		code, raw, _ := h.raw("POST", "/admin/api/assets/icons", &body, form.FormDataContentType(), nil)
		if code != 201 {
			t.Fatal(code, string(raw))
		}
		var result struct {
			Icon string `json:"icon"`
		}
		if err = json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		return result.Icon
	}
	en, zh := upload("red"), upload("blue")
	v, _ := h.server.store.Vendor("openai")
	base := presets.ImagePrefix + "builtin/openai.svg"
	h.request("PUT", "/admin/api/vendors/openai/admin-notes", map[string]any{"revision": 0, "text": "keep private note"}, 200, nil)
	h.request("PATCH", "/admin/api/vendors/openai", map[string]any{"revision": v.Revision, "localized_icons": map[string]string{"en": en}}, 403, map[string]string{"X-CSRF-Token": "bad"})
	h.request("PATCH", "/admin/api/vendors/openai", map[string]any{"revision": v.Revision, "localized_icons": map[string]string{"en": "https://untrusted.example/logo.svg"}}, 400, nil)
	h.request("PATCH", "/admin/api/vendors/openai", map[string]any{"revision": v.Revision, "localized_icons": map[string]string{"en": "/assets/icons/" + strings.Repeat("f", 64) + ".svg"}}, 400, nil)
	raw, _ := h.request("PATCH", "/admin/api/vendors/openai", map[string]any{"revision": v.Revision, "icon": base, "localized_icons": map[string]string{"en": en, "zh-CN": zh}}, 200, nil)
	v = directoryDecode[store.Vendor](t, raw, "vendor")
	if v.Icon != base || v.LocalizedIcons != (store.LocalizedText{En: en, ZhCN: zh}) {
		t.Fatal(v)
	}
	if err := h.server.store.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	// Published surfaces expose just vendor logo settings, never private notes.
	for _, path := range []string{"/api/vendors/openai", "/api/search?q=OpenAI", "/api/apps/openai/codex", "/api/catalog"} {
		raw, _ := h.request("GET", path, nil, 200, nil)
		if !bytes.Contains(raw, []byte(en)) || !bytes.Contains(raw, []byte(zh)) || bytes.Contains(raw, []byte("keep private note")) {
			t.Fatal(path, string(raw))
		}
	}
	app, _ := h.server.store.Application("openai/codex")
	h.request("PATCH", "/admin/api/apps/openai/codex", map[string]any{"revision": app.Revision, "localized_icons": map[string]string{"en": en}}, 400, nil)
	h.request("POST", "/admin/api/vendors/openai/apps", map[string]any{"id": "other", "provider": "info", "name": map[string]string{"en": "Other", "zh-CN": "其他"}, "localized_icons": map[string]string{"en": en}}, 400, nil)
	raw, _ = h.request("GET", "/admin/api/apps/openai/codex", nil, 200, nil)
	if bytes.Contains(raw, []byte("localized_icons")) {
		t.Fatal("app model expanded")
	}
	raw, _ = h.request("PATCH", "/admin/api/vendors/openai", map[string]any{"revision": v.Revision, "enabled": false}, 200, nil)
	v = directoryDecode[store.Vendor](t, raw, "vendor")
	if v.LocalizedIcons.En != en || v.LocalizedIcons.ZhCN != zh {
		t.Fatal("toggle overwrote logos")
	}
	h.close()
	h = newHarness(t, withDir(dir))
	h.login(password)
	v, _ = h.server.store.Vendor("openai")
	if v.Icon != base || v.LocalizedIcons.En != en || v.LocalizedIcons.ZhCN != zh || v.Enabled {
		t.Fatal("restart", v)
	}
	raw, _ = h.request("PATCH", "/admin/api/vendors/openai", map[string]any{"revision": v.Revision, "localized_icons": map[string]string{"en": "", "zh-CN": zh}}, 200, nil)
	v = directoryDecode[store.Vendor](t, raw, "vendor")
	if v.LocalizedIcons.En != "" || v.LocalizedIcons.ZhCN != zh || v.Icon != base {
		t.Fatal("clear changed default/other language")
	}
	h.request("PATCH", "/admin/api/vendors/openai/configuration", map[string]any{"revision": v.Revision, "unset": []string{"icon", "localized_icons.en", "localized_icons.zh-CN"}}, 200, nil)
	v, _ = h.server.store.Vendor("openai")
	if v.Icon != "/assets/presets/builtin/openai.svg" || v.LocalizedIcons != (store.LocalizedText{}) || v.Enabled {
		t.Fatal("reset", v)
	}
	note, err := h.server.store.AdminNotes("vendor", "openai")
	if err != nil || note.Text != "keep private note" || note.Revision != 1 {
		t.Fatal(note, err)
	}
	h.request("PATCH", "/admin/api/vendors/openai", map[string]any{"revision": v.Revision, "icon": "", "localized_icons": map[string]string{"en": "", "zh-CN": ""}}, 200, nil)
	h.close()
	h = newHarness(t, withDir(dir))
	v, _ = h.server.store.Vendor("openai")
	if v.Icon != "" || v.LocalizedIcons != (store.LocalizedText{}) {
		t.Fatal("clear reverted on restart", v)
	}
}
