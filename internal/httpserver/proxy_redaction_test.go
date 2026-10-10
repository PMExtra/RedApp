package httpserver

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/configexchange"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/store"
)

func TestConfigurationResponsesRedactProxyPasswordsExceptCredentialExport(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	vendorEndpoint, appEndpoint := "/admin/api/vendors/openai/configuration", "/admin/api/apps/openai/codex/configuration"
	patch := func(endpoint string, proxy map[string]any, status int) []byte {
		raw, _ := h.request("GET", endpoint, nil, 200, nil)
		data, _ := h.request("PATCH", endpoint, map[string]any{"revision": configurationValue(t, raw).Revision, "set": map[string]any{"proxy": proxy}}, status, nil)
		return data
	}
	vendorURL := "http://vendor-user:vendor-secret@127.0.0.1:3128"
	patch(vendorEndpoint, map[string]any{"mode": "url", "url": vendorURL}, 200)
	appData := patch(appEndpoint, map[string]any{"mode": "url", "url": "socks5://app-user:app-secret@127.0.0.1:1080"}, 200)
	vendorData, _ := h.request("GET", vendorEndpoint, nil, 200, nil)
	inheritedData, _ := h.request("GET", appEndpoint, nil, 200, nil)
	for _, data := range [][]byte{vendorData, appData, inheritedData} {
		if bytes.Contains(data, []byte("-secret")) || !bytes.Contains(data, []byte(":****@127.0.0.1")) {
			t.Fatal("configuration proxy password", string(data))
		}
	}
	app := configurationValue(t, appData)
	if app.ProxyEffective.URL != "socks5://app-user:****@127.0.0.1:1080" || !strings.Contains(fmt.Sprint(app.Effective["proxy"]), "app-user:****@") {
		t.Fatal("redacted app proxy", string(appData))
	}
	// The app's own proxy is not the vendor's; the vendor placeholder cannot move there.
	patch(appEndpoint, map[string]any{"mode": "url", "url": "http://vendor-user:****@127.0.0.1:3128"}, 400)
	patch(appEndpoint, map[string]any{"mode": "url", "url": "socks5://app-user:****@127.0.0.1:1080"}, 200)
	patch(vendorEndpoint, map[string]any{"mode": "url", "url": "http://vendor-user:****@127.0.0.1:3128"}, 200)
	stored, _ := h.server.store.ApplicationConfiguration("openai/codex")
	vendor, _ := h.server.store.VendorConfiguration("openai")
	if stored.ProxyEffective.URL != "socks5://app-user:app-secret@127.0.0.1:1080" || vendor.ProxyEffective.URL != vendorURL {
		t.Fatal("redacted passwords not kept", stored.ProxyEffective.SourceScope, vendor.ProxyEffective.SourceScope)
	}
	// Only the explicit credential export carries saved passwords.
	selection := []store.ExportSelection{{Kind: "App", Key: "openai/codex"}}
	raw, _ := h.request("POST", "/admin/api/configuration/export", store.ExportOptions{Selection: selection, Mode: "independent", IncludeProxyCredentials: true}, 200, nil)
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
		t.Fatal("credential export lost password")
	}
	// Import previews redact both sides of a proxy difference.
	patch(appEndpoint, map[string]any{"mode": "url", "url": "socks5://app-user:changed-secret@127.0.0.1:1080"}, 200)
	body := exchangeUpload(h, raw, nil, 200)
	if bytes.Contains(body, []byte("-secret")) || !bytes.Contains(body, []byte("app-user:****@")) {
		t.Fatal("import preview proxy password", string(body))
	}
	// Import choices for an omitted proxy have no saved password to keep.
	omitted, _ := h.request("POST", "/admin/api/configuration/export", store.ExportOptions{Selection: selection, Mode: "independent"}, 200, nil)
	choice := func(url string) []store.ImportChoice {
		return []store.ImportChoice{{Kind: "App", Key: "openai/codex", Action: "update", DetachTemplate: true, Proxy: &networkproxy.Config{Mode: "url", URL: url}}}
	}
	exchangeUpload(h, omitted, choice("socks5://app-user:****@127.0.0.1:1080"), 400)
	exchangeUpload(h, omitted, choice("socks5://app-user:typed-secret@127.0.0.1:1080"), 200)
}
