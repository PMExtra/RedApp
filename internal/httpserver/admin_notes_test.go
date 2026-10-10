package httpserver

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
)

func TestAdminNotesPrivateCASIsolationAndRestart(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, withDir(dir))
	password := h.password
	paths := []string{"/admin/api/vendors/openai/admin-notes", "/admin/api/apps/openai/codex/admin-notes"}
	for _, path := range paths {
		h.expectError("GET", path, nil, 401, codeAuthRequired, nil)
		h.expectError("PUT", path, map[string]any{"text": "secret"}, 401, codeAuthRequired, ifMatchHeader(1))
	}
	h.login(password)
	secret := func(i int) string {
		return "PRIVATE-NOTES-SENTINEL-" + strings.Repeat("X", i+1) + "\n\t<script>private</script>"
	}
	for i, path := range paths {
		data, headers := h.request("GET", path, nil, 200, nil)
		if initial := decodeJSONBody[adminNotesDTO](t, data); initial != (adminNotesDTO{Revision: 1}) || headers.Get("ETag") != `"1"` || headers.Get("Cache-Control") != "no-store" {
			t.Fatal("notes before the first save", string(data), headers)
		}
		note := map[string]any{"text": secret(i)}
		h.expectError("PUT", path, note, 403, codeCSRFRejected, map[string]string{"X-CSRF-Token": "wrong", "If-Match": `"1"`})
		h.expectError("PUT", path, note, 400, codeIfMatchRequired, nil)
		h.expectError("PUT", path, map[string]any{"text": "x", "revision": 1}, 400, codeInvalidRequest, ifMatchHeader(1))
		h.expectError("PUT", path, map[string]any{"text": strings.Repeat("x", 12001)}, 400, codeValidationFailed, ifMatchHeader(1))
		h.expectError("PUT", path, map[string]any{"text": "bad\x00value"}, 400, codeValidationFailed, ifMatchHeader(1))
		data, headers = h.request("PUT", path, note, 200, ifMatchHeader(1))
		if saved := decodeJSONBody[adminNotesDTO](t, data); saved.Revision != 2 || saved.Text != secret(i) || headers.Get("ETag") != `"2"` {
			t.Fatal("saved notes", string(data))
		}
		h.expectError("PUT", path, note, 409, codeRevisionConflict, ifMatchHeader(1))
	}
	// Notes have their own revision; the entity revision is unchanged.
	if v := h.adminVendor("openai"); v.Revision != h.configuration("vendors/openai").Revision {
		t.Fatal("notes changed the vendor configuration revision")
	}
	// The same application ID under another vendor stays independent.
	h.createVendor("other")
	other := h.createApp("other", "codex", application.Info, nil)
	for _, path := range []string{"/admin/api/vendors/other/admin-notes", "/admin/api/apps/" + other.Key + "/admin-notes"} {
		if got := getJSON[adminNotesDTO](h, path); got.Text != "" || got.Revision != 1 {
			t.Fatal("notes leaked across identities", path, got)
		}
	}
	h.expectError("GET", "/admin/api/vendors/missing/admin-notes", nil, 404, codeVendorNotFound, nil)
	h.expectError("PUT", "/admin/api/apps/openai/missing/admin-notes", map[string]any{"text": ""}, 404, codeApplicationNotFound, ifMatchHeader(1))
	if err := h.store.DeleteApplication(other.Key, other.Revision); err != nil {
		t.Fatal(err)
	}
	h.expectError("PUT", "/admin/api/apps/"+other.Key+"/admin-notes", map[string]any{"text": "x"}, 409, codeEntityDeleted, ifMatchHeader(1))
	// Entity documents, configuration and public pages never carry notes.
	for _, path := range []string{"/admin/api/vendors/openai", "/admin/api/apps/openai/codex", "/admin/api/vendors/openai/configuration", "/admin/api/apps/openai/codex/configuration", "/admin/api/apps?vendor=openai", "/admin/api/vendors", "/api/bootstrap", "/api/catalog", "/api/home", "/api/search?q=PRIVATE-NOTES-SENTINEL", "/api/vendors/openai", "/api/apps/openai/codex/instructions/document?lang=en", "/openai", "/openai/codex"} {
		data, _ := h.request("GET", path, nil, 200, nil)
		if bytes.Contains(data, []byte("PRIVATE-NOTES-SENTINEL")) || bytes.Contains(data, []byte(`"admin_notes"`)) {
			t.Fatal("notes leaked", path)
		}
	}
	h.patchConfiguration("vendors/openai", map[string]any{"unset": []string{"icon"}}, 200)
	h.close()
	h = newHarness(t, withDir(dir))
	h.login(password)
	for i, path := range paths {
		if got := getJSON[adminNotesDTO](h, path); got.Revision != 2 || got.Text != secret(i) {
			t.Fatal("notes lost on restart or configuration reset", got)
		}
		h.request("PUT", path, map[string]any{"text": ""}, 200, ifMatchHeader(2))
	}
	h.close()
	h = newHarness(t, withDir(dir))
	h.login(password)
	for _, path := range paths {
		if got := getJSON[adminNotesDTO](h, path); got.Text != "" || got.Revision != 3 {
			t.Fatal("cleared notes did not persist", got)
		}
	}
}
