package httpserver

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/configexchange"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/store"
)

func exchangeUpload(h *harness, raw []byte, choices []store.ImportChoice, status int) []byte {
	h.t.Helper()
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
	part, _ := writer.CreateFormFile("file", "identity-ignored.yaml")
	part.Write(raw)
	data, _ := json.Marshal(choices)
	writer.WriteField("choices", string(data))
	writer.Close()
	code, body, _ := h.raw("POST", "/admin/api/configuration/import/preview", bytes.NewReader(b.Bytes()), writer.FormDataContentType(), nil)
	if code != status {
		h.t.Fatalf("preview status %d expected %d: %s", code, status, body)
	}
	return body
}
func TestExchangeHTTPZIPImagesRoundtripTrustReplayAndSession(t *testing.T) {
	a := newHarness(t)
	a.login(a.password)
	a.request("POST", "/admin/api/vendors", map[string]any{"id": "exchange", "name": map[string]string{"en": "Exchange", "zh-CN": "交换"}, "icon": "/assets/presets/builtin/openai.svg"}, 201, nil)
	source, _ := a.server.store.Application("openai/codex")
	raw, _ := a.request("POST", "/admin/api/apps/openai/codex/copy", store.CopyApplicationInput{SourceUID: source.UID, SourceRevision: source.Revision, TargetVendor: "exchange", TargetID: "source", Mode: "linked"}, 201, nil)
	var copied struct{ App store.Application }
	json.Unmarshal(raw, &copied)
	if copied.App.Enabled || copied.App.UID == source.UID {
		t.Fatal(copied)
	}
	endpoint := "/admin/api/apps/exchange/source/configuration"
	raw, _ = a.request("GET", endpoint, nil, 200, nil)
	cfg := configurationValue(t, raw)
	a.request("PATCH", endpoint, map[string]any{"revision": cfg.Revision, "set": map[string]any{"instructions.en": "<script>fetch('https://must-not-fetch.invalid')</script>\n## Source\n", "proxy": map[string]any{"mode": "url", "url": "http://credential:sentinel@127.0.0.1:3128"}}}, 200, nil)
	a.server.store.SaveAdminNotes("app", "exchange/source", 0, "private-notes-sentinel")
	raw, headers := a.request("POST", "/admin/api/configuration/export", store.ExportOptions{Selection: []store.ExportSelection{{Kind: "App", Key: "exchange/source"}}, Mode: "linked"}, 200, nil)
	if headers.Get("Content-Type") != "application/zip" || !strings.Contains(headers.Get("Content-Disposition"), "attachment") {
		t.Fatal(headers)
	}
	p, e := configexchange.Parse(raw)
	if e != nil || len(p.Assets) == 0 {
		t.Fatal("controlled assets absent", e)
	}
	for _, doc := range p.Documents {
		body, _ := configexchange.YAML(doc)
		if bytes.Contains(body, []byte("credential:sentinel")) || bytes.Contains(body, []byte("private-notes-sentinel")) {
			t.Fatal("default export sensitive bytes")
		}
	}
	b := newHarness(t)
	b.login(b.password)
	body := exchangeUpload(b, raw, nil, 200)
	var preview struct {
		ID      string
		Preview store.ImportPlan
	}
	json.Unmarshal(body, &preview)
	if preview.Preview.Ready || !preview.Preview.NeedsTrust {
		t.Fatal(string(body))
	}
	b.request("POST", "/admin/api/configuration/import/"+preview.ID+"/execute", map[string]any{"confirm": true, "trust_instructions": true}, 400, nil)
	choices := []store.ImportChoice{{Kind: "App", Key: "exchange/source", Proxy: &networkproxy.Config{Mode: "inherit"}}}
	body = exchangeUpload(b, raw, choices, 200)
	json.Unmarshal(body, &preview)
	if !preview.Preview.Ready {
		t.Fatal(string(body))
	}
	b.request("POST", "/admin/api/configuration/import/"+preview.ID+"/execute", map[string]any{"confirm": true}, 400, nil)
	result, _ := b.request("POST", "/admin/api/configuration/import/"+preview.ID+"/execute", map[string]any{"confirm": true, "trust_instructions": true}, 200, nil)
	again, _ := b.request("POST", "/admin/api/configuration/import/"+preview.ID+"/execute", map[string]any{"confirm": true}, 200, nil)
	if !bytes.Equal(result, again) {
		t.Fatal("idempotent receipt changed")
	}
	dest, e := b.server.store.Application("exchange/source")
	if e != nil || dest.Enabled || dest.SourceEpoch != 1 || dest.UID == copied.App.UID {
		t.Fatal(dest, e)
	}
	cfg, _ = b.server.store.ApplicationConfiguration(dest.Key)
	if cfg.TemplateRef == nil || *cfg.TemplateRef != "openai/codex" {
		t.Fatal(cfg)
	}
	vendor, _ := b.server.store.Vendor("exchange")
	b.request("GET", vendor.Icon, nil, 200, nil)
	body = exchangeUpload(b, raw, choices, 200)
	var pending struct{ ID string }
	json.Unmarshal(body, &pending)
	b.login(b.password)
	b.request("POST", "/admin/api/configuration/import/"+pending.ID+"/execute", map[string]any{"confirm": true}, 409, nil)
	again, _ = b.request("POST", "/admin/api/configuration/import/"+preview.ID+"/execute", map[string]any{"confirm": true}, 200, nil)
	if !bytes.Equal(result, again) {
		t.Fatal("new administrator session changed terminal result")
	}
	path := "/admin/api/configuration/import/" + preview.ID + "/execute"
	b.request("POST", path, map[string]any{"confirm": true}, 403, map[string]string{"X-CSRF-Token": "invalid"})
	request := httptest.NewRequest("POST", path, strings.NewReader(`{"confirm":true}`))
	response := httptest.NewRecorder()
	b.server.ServeHTTP(response, request)
	if response.Code != 401 {
		t.Fatal("terminal result bypassed authentication", response.Code)
	}
	b.server.store.DB.Exec(`UPDATE configuration_import_receipts SET created_s=? WHERE id=?`, time.Now().Add(-25*time.Hour).Unix(), preview.ID)
	b.request("POST", path, map[string]any{"confirm": true}, 409, nil)
	b.request("POST", "/admin/api/configuration/export", store.ExportOptions{Mode: "linked"}, 400, nil)
	anonymous := newHarness(t)
	anonymous.request("POST", "/admin/api/configuration/export", store.ExportOptions{Mode: "linked"}, 401, nil)
}
func TestExchangeHTTPPreviewCASAndBadPackage(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	p, _ := h.server.store.ExportConfiguration(store.ExportOptions{Selection: []store.ExportSelection{{Kind: "App", Key: "openai/codex"}}, Mode: "linked"})
	raw, _ := configexchange.ZIP(p)
	choices := []store.ImportChoice{{Kind: "App", Key: "openai/codex", Action: "update"}}
	body := exchangeUpload(h, raw, choices, 200)
	var preview struct{ ID string }
	json.Unmarshal(body, &preview)
	notes, _ := h.server.store.AdminNotes("app", "openai/codex")
	h.server.store.SaveAdminNotes("app", "openai/codex", notes.Revision, "concurrent private notes")
	h.request("POST", "/admin/api/configuration/import/"+preview.ID+"/execute", map[string]any{"confirm": true, "trust_instructions": true}, 409, nil)
	exchangeUpload(h, []byte("schema_version: 1\nkind: App\ndistribution: {}\n"), nil, 400)
}

func TestExchangeHTTPExpiredPreviewAndSessionRevokedDuringPrepare(t *testing.T) {
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
	h.login(h.password)
	p, _ := h.server.store.ExportConfiguration(store.ExportOptions{Selection: []store.ExportSelection{{Kind: "App", Key: "openai/codex"}}, Mode: "linked"})
	raw, _ := configexchange.ZIP(p)
	body := exchangeUpload(h, raw, []store.ImportChoice{{Kind: "App", Key: "openai/codex", Action: "update"}}, 200)
	var preview struct{ ID string }
	json.Unmarshal(body, &preview)
	h.server.exchangeMu.Lock()
	record := h.server.exchangePreviews[preview.ID]
	record.until = time.Now().Add(-time.Minute)
	h.server.exchangePreviews[preview.ID] = record
	h.server.exchangeMu.Unlock()
	h.request("POST", "/admin/api/configuration/import/"+preview.ID+"/execute", map[string]any{"confirm": true, "trust_instructions": true}, 409, nil)
	body = exchangeUpload(h, raw, []store.ImportChoice{{Kind: "App", Key: "openai/codex", Action: "update"}}, 200)
	json.Unmarshal(body, &preview)
	before, _ := h.server.store.Application("openai/codex")
	revokeDuringPrepare.Store(true)
	h.request("POST", "/admin/api/configuration/import/"+preview.ID+"/execute", map[string]any{"confirm": true, "trust_instructions": true}, 409, nil)
	after, _ := h.server.store.Application("openai/codex")
	if before.Revision != after.Revision {
		t.Fatal("revoked session committed import")
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
