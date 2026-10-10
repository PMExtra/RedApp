package httpserver

import (
	"bytes"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/configexchange"
	"github.com/PMExtra/RedApp/internal/store"
)

func TestCopyApplicationGuardsSourceIdentityAndNotes(t *testing.T) {
	h := newHarness(t)
	h.login("")
	h.createVendor("exchange")
	source := h.adminApp("openai/codex")
	path := "/admin/api/apps/openai/codex/copy"
	body := func(changes map[string]any) map[string]any {
		out := map[string]any{"source_uid": source.UID, "target_vendor": "exchange", "target_id": "copy", "mode": "linked", "include_notes": false}
		for key, value := range changes {
			out[key] = value
		}
		return out
	}
	h.expectError("POST", path, body(nil), 400, codeIfMatchRequired, nil)
	h.expectError("POST", path, body(nil), 409, codeRevisionConflict, ifMatchHeader(source.Revision+1))
	h.expectError("POST", path, body(map[string]any{"source_uid": strings.Repeat("0", 32)}), 409, codeRevisionConflict, ifMatchHeader(source.Revision))
	h.expectError("POST", path, body(map[string]any{"target_vendor": "missing"}), 404, codeVendorNotFound, ifMatchHeader(source.Revision))
	h.expectError("POST", path, body(map[string]any{"target_id": "codex", "target_vendor": "openai"}), 409, codeAlreadyExists, ifMatchHeader(source.Revision))
	h.expectError("POST", path, body(map[string]any{"include_notes": true}), 400, codeValidationFailed, ifMatchHeader(source.Revision))
	h.expectError("POST", path, body(map[string]any{"mode": "other"}), 400, codeValidationFailed, ifMatchHeader(source.Revision))
	h.expectError("POST", path, body(map[string]any{"source_revision": 1}), 400, codeInvalidRequest, ifMatchHeader(source.Revision))
	h.expectError("POST", "/admin/api/apps/openai/missing/copy", body(nil), 404, codeApplicationNotFound, ifMatchHeader(1))
	h.request("PUT", "/admin/api/apps/openai/codex/admin-notes", map[string]any{"text": "copied private notes"}, 200, ifMatchHeader(1))
	h.expectError("POST", path, body(map[string]any{"include_notes": true, "notes_revision": 1}), 409, codeRevisionConflict, ifMatchHeader(source.Revision))
	data, headers := h.request("POST", path, body(map[string]any{"include_notes": true, "notes_revision": 2}), 201, ifMatchHeader(source.Revision))
	copied := decodeJSONBody[appDTO](t, data)
	if copied.Enabled || copied.UID == source.UID || copied.Key != "exchange/copy" || copied.BuiltinTemplate || headers.Get("Location") != "/admin/api/apps/exchange/copy" || headers.Get("ETag") != etag(copied.Revision) {
		t.Fatal("copy", string(data), headers)
	}
	if notes := getJSON[adminNotesDTO](h, "/admin/api/apps/exchange/copy/admin-notes"); notes.Text != "copied private notes" {
		t.Fatal("notes not copied", notes)
	}
	if c := h.configuration("apps/exchange/copy"); c.TemplateRef == nil || *c.TemplateRef != "openai/codex" {
		t.Fatal("linked copy lost its template", c)
	}
	h.expectError("POST", path, body(nil), 409, codeAlreadyExists, ifMatchHeader(source.Revision))
	// Copies of built-in applications are deletable.
	h.deleteApp(copied.Key, copied.UID, copied.Revision, 200)
	h.expectError("GET", "/admin/api/apps/exchange/copy", nil, 404, codeApplicationNotFound, nil)
	soft := h.createApp("exchange", "soft", "info", nil)
	if err := h.store.DeleteApplication(soft.Key, soft.Revision); err != nil {
		t.Fatal(err)
	}
	deleted := h.adminApp(soft.Key)
	h.expectError("POST", "/admin/api/apps/exchange/soft/copy", map[string]any{"source_uid": soft.UID, "target_vendor": "exchange", "target_id": "again", "mode": "independent", "include_notes": false}, 409, codeEntityDeleted, ifMatchHeader(deleted.Revision))
}

func TestExchangeRoundtripTrustReceiptAndSession(t *testing.T) {
	a := newHarness(t)
	a.login("")
	a.request("POST", "/admin/api/vendors", map[string]any{"id": "exchange", "name": map[string]string{"en": "Exchange", "zh-CN": "交换"}, "icon": "/assets/presets/builtin/openai.svg", "enabled": true}, 201, nil)
	source := a.adminApp("openai/codex")
	a.request("POST", "/admin/api/apps/openai/codex/copy", map[string]any{"source_uid": source.UID, "target_vendor": "exchange", "target_id": "source", "mode": "linked", "include_notes": false}, 201, ifMatchHeader(source.Revision))
	a.patchConfiguration("apps/exchange/source", map[string]any{"set": map[string]any{"instructions.en": "<script>fetch('https://must-not-fetch.invalid')</script>\n## Source\n", "proxy": map[string]any{"mode": "url", "url": "http://credential:sentinel@127.0.0.1:3128"}}}, 200)
	a.request("PUT", "/admin/api/apps/exchange/source/admin-notes", map[string]any{"text": "private-notes-sentinel"}, 200, ifMatchHeader(1))
	raw := a.exportPackage("linked", false, "app:exchange/source")
	p, err := configexchange.Parse(raw)
	if err != nil || len(p.Assets) == 0 {
		t.Fatal("controlled assets absent", err)
	}
	for _, doc := range p.Documents {
		body, _ := configexchange.YAML(doc)
		if bytes.Contains(body, []byte("credential:sentinel")) || bytes.Contains(body, []byte("private-notes-sentinel")) {
			t.Fatal("default export contains sensitive data")
		}
	}
	for _, invalid := range []map[string]any{
		{"selection": []any{}, "mode": "linked", "include_notes": false, "include_proxy_credentials": false},
		{"selection": []map[string]any{{"kind": "App", "key": "openai/codex"}}, "mode": "linked", "include_notes": false, "include_proxy_credentials": false},
		{"selection": []map[string]any{{"kind": "app", "key": "openai/codex", "include_apps": true}}, "mode": "linked", "include_notes": false, "include_proxy_credentials": false},
		{"selection": []map[string]any{{"kind": "app", "key": "openai/codex"}}, "mode": "copy", "include_notes": false, "include_proxy_credentials": false},
	} {
		a.expectError("POST", "/admin/api/configuration/export", invalid, 400, codeValidationFailed, nil)
	}
	a.expectError("POST", "/admin/api/configuration/export", map[string]any{"selection": []map[string]any{{"kind": "app", "key": "openai/codex"}}, "mode": "linked"}, 400, codeInvalidRequest, nil)
	a.expectError("POST", "/admin/api/configuration/export", map[string]any{"selection": []map[string]any{{"kind": "app", "key": "openai/missing"}}, "mode": "linked", "include_notes": false, "include_proxy_credentials": false}, 404, codeApplicationNotFound, nil)
	a.expectError("POST", "/admin/api/configuration/export", map[string]any{"selection": []map[string]any{{"kind": "vendor", "key": "missing"}}, "mode": "linked", "include_notes": false, "include_proxy_credentials": false}, 404, codeVendorNotFound, nil)

	b := newHarness(t)
	b.expectError("POST", "/admin/api/configuration/export", map[string]any{}, 401, codeAuthRequired, nil)
	b.login("")
	preview := decodeJSONBody[importPreviewDTO](t, exchangeUpload(b, raw, nil, 200))
	if preview.Ready || !preview.NeedsInstructionsTrust || len(preview.Digest) != 64 || !preview.ExpiresAt.After(time.Now()) {
		t.Fatal("preview without choices", preview)
	}
	kinds := map[string]bool{}
	for _, item := range preview.Items {
		kinds[item.Kind] = true
		if item.Key == "exchange/source" && (item.Action != "create" || strings.Join(item.Requirements, ",") != "resolve_omitted_proxy" || strings.Join(item.OmittedFields, ",") != "proxy") {
			t.Fatal("omitted proxy requirement", item)
		}
	}
	if !kinds["app"] || !kinds["vendor"] {
		t.Fatal("import kinds", kinds)
	}
	expectCode(t, b.executeImport(preview.ID, true, 409), codeImportNotReady)
	choices := []map[string]any{{"kind": "app", "key": "exchange/source", "proxy": map[string]any{"mode": "inherit"}}}
	preview = decodeJSONBody[importPreviewDTO](t, exchangeUpload(b, raw, choices, 200))
	if !preview.Ready {
		t.Fatal("choices did not resolve the preview", preview)
	}
	expectCode(t, b.executeImport(preview.ID, false, 400), codeInstructionsTrustRequired)
	b.expectError("POST", "/admin/api/configuration/import/"+preview.ID+"/execute", map[string]any{"confirm": true, "trust_instructions": true}, 400, codeInvalidRequest, nil)
	result := b.executeImport(preview.ID, true, 200)
	receipt := decodeJSONBody[importResultDTO](t, result)
	if !receipt.Applied || len(receipt.Items) != 2 || receipt.Items[0].Revision < 1 {
		t.Fatal("receipt", string(result))
	}
	if again := b.executeImport(preview.ID, false, 200); !bytes.Equal(result, again) {
		t.Fatal("idempotent receipt changed")
	}
	dest := b.adminApp("exchange/source")
	if dest.Enabled || dest.SourceEpoch != 1 || b.adminVendor("exchange").Enabled {
		t.Fatal("imported objects must start disabled", dest)
	}
	if c := b.configuration("apps/exchange/source"); c.TemplateRef == nil || *c.TemplateRef != "openai/codex" || c.Effective["proxy"].(map[string]any)["mode"] != "inherit" {
		t.Fatal("linked import", c)
	}
	b.request("GET", b.adminVendor("exchange").Icon, nil, 200, nil)
	// A preview belongs to its session; a receipt does not.
	pending := decodeJSONBody[importPreviewDTO](t, exchangeUpload(b, raw, []map[string]any{{"kind": "app", "key": "exchange/source", "action": "skip"}, {"kind": "vendor", "key": "exchange", "action": "skip"}}, 200))
	b.login("")
	expectCode(t, b.executeImport(pending.ID, true, 404), codePreviewNotFound)
	if again := b.executeImport(preview.ID, false, 200); !bytes.Equal(result, again) {
		t.Fatal("a new session changed the receipt")
	}
	execute := "/admin/api/configuration/import/" + preview.ID + "/execute"
	b.expectError("POST", execute, map[string]any{"trust_instructions": true}, 403, codeCSRFRejected, map[string]string{"X-CSRF-Token": "invalid"})
	anonymous := httptest.NewRequest("POST", execute, strings.NewReader(`{"trust_instructions":true}`))
	anonymous.Header.Set("Content-Type", "application/json")
	if response := b.serve(anonymous); response.Code != 401 {
		t.Fatal("receipt bypassed authentication", response.Code)
	}
	if _, err = b.store.DB.Exec(`UPDATE configuration_import_receipts SET created_s=? WHERE id=?`, time.Now().Add(-25*time.Hour).Unix(), preview.ID); err != nil {
		t.Fatal(err)
	}
	expectCode(t, b.executeImport(preview.ID, true, 404), codePreviewNotFound)
	expectCode(t, b.executeImport(strings.Repeat("a", 32), true, 404), codePreviewNotFound)
	b.expectError("POST", "/admin/api/configuration/import/bad/execute", map[string]any{"trust_instructions": true}, 400, codeInvalidPath, nil)
}

func TestImportPreviewRejectsInvalidUploadsAndStalePlans(t *testing.T) {
	h := newHarness(t)
	h.login("")
	raw := h.exportPackage("linked", false, "app:openai/codex")
	update := []map[string]any{{"kind": "app", "key": "openai/codex", "action": "update"}}
	preview := decodeJSONBody[importPreviewDTO](t, exchangeUpload(h, raw, update, 200))
	if !preview.Ready {
		t.Fatal("update preview", preview)
	}
	h.request("PUT", "/admin/api/apps/openai/codex/admin-notes", map[string]any{"text": "concurrent private notes"}, 200, ifMatchHeader(1))
	expectCode(t, h.executeImport(preview.ID, true, 409), codePreviewStale)
	expectCode(t, exchangeUpload(h, []byte("schema_version: 1\nkind: App\ndistribution: {}\n"), nil, 400), codePackageInvalid)
	expectCode(t, exchangeUpload(h, raw, []map[string]any{{"kind": "App", "key": "openai/codex"}}, 400), codeInvalidRequest)
	expectCode(t, exchangeUpload(h, raw, []map[string]any{{"kind": "app", "key": "openai/codex", "unknown": true}}, 400), codeInvalidRequest)
	expectCode(t, exchangeUpload(h, raw, []map[string]any{{"kind": "app", "key": "openai/codex", "action": "create"}}, 409), codeAlreadyExists)
	expectCode(t, exchangeUpload(h, raw, []map[string]any{{"kind": "app", "key": "openai/codex", "action": "rename"}}, 400), codeValidationFailed)
	code, data, _ := h.raw("POST", "/admin/api/configuration/import/preview", bytes.NewReader(raw), "application/zip", nil)
	if code != 415 {
		t.Fatal("non-multipart preview", code)
	}
	expectCode(t, data, codeUnsupportedMediaType)
	body, contentType := multipartForm(t, [3]string{"choices", "", "[]"})
	code, data, _ = h.raw("POST", "/admin/api/configuration/import/preview", body, contentType, nil)
	if code != 400 {
		t.Fatal("preview without file", code)
	}
	expectCode(t, data, codeInvalidRequest)
	// At most eight previews are kept at once.
	for range 7 {
		exchangeUpload(h, raw, update, 200)
	}
	expectCode(t, exchangeUpload(h, raw, update, 429), codePreviewLimitExceeded)
}

func TestImportRefusesExpiredPreviewAndSessionRevokedDuringCommit(t *testing.T) {
	var revokeDuringPrepare atomic.Bool
	var h *harness
	h = newHarness(t, withOptions(WithConfigurationCheck(func(store.DirectorySnapshot) error {
		if revokeDuringPrepare.Load() {
			r := httptest.NewRequest("POST", "/admin", nil)
			r.Header.Set("Cookie", cookieHeader(h))
			h.server.auth.Logout(r)
		}
		return nil
	})))
	h.login("")
	raw := h.exportPackage("linked", false, "app:openai/codex")
	update := []map[string]any{{"kind": "app", "key": "openai/codex", "action": "update"}}
	preview := decodeJSONBody[importPreviewDTO](t, exchangeUpload(h, raw, update, 200))
	h.server.exchangeMu.Lock()
	record := h.server.exchangePreviews[preview.ID]
	record.until = time.Now().Add(-time.Minute)
	h.server.exchangePreviews[preview.ID] = record
	h.server.exchangeMu.Unlock()
	expectCode(t, h.executeImport(preview.ID, true, 404), codePreviewNotFound)
	preview = decodeJSONBody[importPreviewDTO](t, exchangeUpload(h, raw, update, 200))
	before := h.adminApp("openai/codex")
	revokeDuringPrepare.Store(true)
	expectCode(t, h.executeImport(preview.ID, true, 404), codePreviewNotFound)
	if after, _ := h.store.Application("openai/codex"); after.Revision != before.Revision {
		t.Fatal("revoked session committed the import")
	}
}

func cookieHeader(h *harness) string {
	u, _ := url.Parse(h.http.URL + "/admin")
	parts := []string{}
	for _, c := range h.client.Jar.Cookies(u) {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}
