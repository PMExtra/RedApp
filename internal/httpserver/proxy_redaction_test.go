package httpserver

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/configexchange"
)

func TestConfigurationResponsesRedactProxyPasswordsExceptCredentialExport(t *testing.T) {
	h := newHarness(t)
	h.login("")
	patch := func(path string, proxy map[string]any, status int) []byte {
		return h.patchConfiguration(path, map[string]any{"set": map[string]any{"proxy": proxy}}, status)
	}
	vendorURL := "http://vendor-user:vendor-secret@127.0.0.1:3128"
	patch("vendors/openai", map[string]any{"mode": "url", "url": vendorURL}, 200)
	appData := patch("apps/openai/codex", map[string]any{"mode": "url", "url": "socks5://app-user:app-secret@127.0.0.1:1080"}, 200)
	vendorData, _ := h.request("GET", "/admin/api/vendors/openai/configuration", nil, 200, nil)
	inheritedData, _ := h.request("GET", "/admin/api/apps/openai/codex/configuration", nil, 200, nil)
	for _, data := range [][]byte{vendorData, appData, inheritedData} {
		if bytes.Contains(data, []byte("-secret")) || !bytes.Contains(data, []byte(":****@127.0.0.1")) {
			t.Fatal("configuration proxy password", string(data))
		}
	}
	app := decodeJSONBody[configurationDTO](t, appData)
	if app.ProxyEffective.URL != "socks5://app-user:****@127.0.0.1:1080" || app.ProxyEffective.SourceScope != "app" || !strings.Contains(fmt.Sprint(app.Effective["proxy"]), "app-user:****@") {
		t.Fatal("redacted application proxy", string(appData))
	}
	// The vendor's placeholder cannot move to the application's own proxy.
	expectCode(t, patch("apps/openai/codex", map[string]any{"mode": "url", "url": "http://vendor-user:****@127.0.0.1:3128"}, 400), codeProxyRedactedMismatch)
	patch("apps/openai/codex", map[string]any{"mode": "url", "url": "socks5://app-user:****@127.0.0.1:1080"}, 200)
	patch("vendors/openai", map[string]any{"mode": "url", "url": "http://vendor-user:****@127.0.0.1:3128"}, 200)
	stored, _ := h.store.ApplicationConfiguration("openai/codex")
	vendor, _ := h.store.VendorConfiguration("openai")
	if stored.ProxyEffective.URL != "socks5://app-user:app-secret@127.0.0.1:1080" || vendor.ProxyEffective.URL != vendorURL {
		t.Fatal("redacted passwords not kept", stored.ProxyEffective.SourceScope, vendor.ProxyEffective.SourceScope)
	}
	// Only the explicit credential export carries saved passwords.
	raw := h.exportPackage("independent", true, "app:openai/codex")
	p, err := configexchange.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	exported := false
	for _, doc := range p.Documents {
		body, _ := configexchange.YAML(doc)
		exported = exported || bytes.Contains(body, []byte("app-user:app-secret@"))
	}
	if !exported {
		t.Fatal("credential export lost the password")
	}
	// Import previews redact both sides of a proxy difference.
	patch("apps/openai/codex", map[string]any{"mode": "url", "url": "socks5://app-user:changed-secret@127.0.0.1:1080"}, 200)
	body := exchangeUpload(h, raw, nil, 200)
	if bytes.Contains(body, []byte("-secret")) || !bytes.Contains(body, []byte("app-user:****@")) {
		t.Fatal("import preview proxy password", string(body))
	}
	// A choice for an omitted proxy has no saved password to keep.
	omitted := h.exportPackage("independent", false, "app:openai/codex")
	choice := func(url string) []map[string]any {
		return []map[string]any{{"kind": "app", "key": "openai/codex", "action": "update", "detach_template": true, "proxy": map[string]any{"mode": "url", "url": url}}}
	}
	expectCode(t, exchangeUpload(h, omitted, choice("socks5://app-user:****@127.0.0.1:1080"), 400), codeValidationFailed)
	exchangeUpload(h, omitted, choice("socks5://app-user:typed-secret@127.0.0.1:1080"), 200)
}
