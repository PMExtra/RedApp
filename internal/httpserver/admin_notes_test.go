package httpserver

import (
	"bytes"
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/store"
	"strings"
	"testing"
)

func TestAdminNotesPrivateCASIsolationAndRestart(t *testing.T) {
	dir := t.TempDir()
	h := newDirectoryHarness(t, dir)
	password := h.password
	endpoints := []string{"/admin/api/vendors/openai/admin-notes", "/admin/api/apps/openai/codex/admin-notes"}
	for _, path := range endpoints {
		h.request("GET", path, nil, 401, nil)
		h.request("PUT", path, map[string]any{"text": "secret", "revision": 0}, 401, nil)
	}
	h.login(password)
	for i, path := range endpoints {
		raw, headers := h.request("GET", path, nil, 200, nil)
		var initial store.AdminNotes
		if err := json.Unmarshal(raw, &initial); err != nil || initial != (store.AdminNotes{}) || headers.Get("Cache-Control") != "no-store" {
			t.Fatal(string(raw), headers, err)
		}
		note := map[string]any{"text": "PRIVATE-NOTES-SENTINEL-" + strings.Repeat("X", i+1) + "\n\t<script>private</script>", "revision": 0}
		h.request("PUT", path, note, 403, map[string]string{"X-CSRF-Token": "wrong"})
		h.request("PUT", path, note, 200, nil)
		h.request("PUT", path, note, 409, nil)
		h.request("PUT", path, map[string]any{"text": strings.Repeat("x", 12001), "revision": 1}, 400, nil)
		h.request("PUT", path, map[string]any{"text": "bad\x00value", "revision": 1}, 400, nil)
	}
	// Same application ID under another vendor, and the vendor/app type boundary, stay independent.
	other, err := h.server.DB.CreateVendor(store.VendorInput{ID: "other", Name: store.LocalizedText{En: "Other", ZhCN: "其他"}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := h.server.DB.CreateApplication(other.ID, store.ApplicationInput{ID: "codex", Name: store.LocalizedText{En: "Other", ZhCN: "其他"}, Provider: "info"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/admin/api/vendors/other/admin-notes", "/admin/api/apps/" + a.Key + "/admin-notes"} {
		raw, _ := h.request("GET", path, nil, 200, nil)
		if !bytes.Contains(raw, []byte(`"text":""`)) {
			t.Fatal(string(raw))
		}
	}
	// The ordinary entity DTOs and templates never acquire notes fields or content.
	for _, path := range []string{"/admin/api/vendors/openai", "/admin/api/apps/openai/codex", "/admin/api/vendors/openai/configuration", "/admin/api/apps/openai/codex/configuration", "/api/bootstrap", "/api/catalog", "/api/home", "/api/search?q=PRIVATE-NOTES-SENTINEL", "/api/vendors/openai", "/api/apps/openai/codex/instructions/document?lang=en", "/openai", "/openai/codex"} {
		raw, _ := h.request("GET", path, nil, 200, nil)
		if bytes.Contains(raw, []byte("PRIVATE-NOTES-SENTINEL")) || bytes.Contains(raw, []byte(`"admin_notes"`)) {
			t.Fatal("notes leaked", path)
		}
	}
	for _, path := range []string{"/api/vendors/openai/admin-notes", "/api/apps/openai/codex/admin-notes"} {
		h.request("GET", path, nil, 404, nil)
	}
	v, _ := h.server.DB.Vendor("openai")
	app, _ := h.server.DB.Application("openai/codex")
	h.request("PATCH", "/admin/api/vendors/openai/configuration", map[string]any{"revision": v.Revision, "unset": []string{"icon"}}, 200, nil)
	h.request("PATCH", "/admin/api/apps/openai/codex/configuration", map[string]any{"revision": app.Revision, "unset": []string{"icon"}}, 200, nil)
	h.close()
	h = newDirectoryHarness(t, dir)
	h.login(password)
	for i, path := range endpoints {
		raw, _ := h.request("GET", path, nil, 200, nil)
		var got store.AdminNotes
		if err := json.Unmarshal(raw, &got); err != nil || got.Revision != 1 || got.Text != "PRIVATE-NOTES-SENTINEL-"+strings.Repeat("X", i+1)+"\n\t<script>private</script>" {
			t.Fatal("restart/reset/isolation", string(raw), err)
		}
		h.request("PUT", path, map[string]any{"text": "", "revision": 0}, 200, map[string]string{"If-Match": "\"1\""})
	}
	h.close()
	h = newDirectoryHarness(t, dir)
	h.login(password)
	for _, path := range endpoints {
		raw, _ := h.request("GET", path, nil, 200, nil)
		var got store.AdminNotes
		if err := json.Unmarshal(raw, &got); err != nil || got.Text != "" || got.Revision != 2 {
			t.Fatal("clear did not persist", string(raw), err)
		}
	}
	for _, path := range []string{"/admin/vendors/openai/admin-notes", "/admin/vendors/openai/apps/codex/admin-notes"} {
		h.request("GET", path, nil, 200, nil)
	}
}
