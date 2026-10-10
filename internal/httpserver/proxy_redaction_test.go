package httpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/configexchange"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/store"
)

func TestGlobalProxyResponsesRedactPasswordAndKeepItOnlyForSameProxy(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	put := func(body map[string]any, status int) []byte {
		data, _ := h.request("PUT", "/admin/api/settings/proxy", body, status, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, h.server.Pool.Proxy().Revision)})
		return data
	}
	saved := "http://proxy%40user:global-secret@proxy.example:3128"
	for _, data := range [][]byte{put(map[string]any{"mode": "url", "url": saved}, 200), func() []byte { data, _ := h.request("GET", "/admin/api/settings/proxy", nil, 200, nil); return data }()} {
		var view map[string]any
		json.Unmarshal(data, &view)
		if bytes.Contains(data, []byte("global-secret")) || view["url"] != "http://proxy%40user:****@proxy.example:3128" || view["mode"] != "url" || view["server"] != nil {
			t.Fatal("global proxy view", string(data))
		}
	}
	revision := h.server.Pool.Proxy().Revision
	// The placeholder may not carry the saved password to another user, scheme or host.
	for _, moved := range []string{"http://other:****@proxy.example:3128", "socks5://proxy%40user:****@proxy.example:3128", "http://proxy%40user:****@attacker.example:3128", "http://proxy%40user:****@proxy.example:3129"} {
		if data := put(map[string]any{"mode": "url", "url": moved}, 400); bytes.Contains(data, []byte("global-secret")) {
			t.Fatal("error leaked password")
		}
	}
	// The removed compatibility field and an implicit mode are rejected.
	put(map[string]any{"server": saved}, 400)
	put(map[string]any{"url": saved}, 400)
	if got := h.server.Pool.Proxy(); got.URL != saved || got.Revision != revision {
		t.Fatal("rejected update changed proxy", got.Redacted())
	}
	put(map[string]any{"mode": "url", "url": "http://proxy%40user:%2A%2A%2A%2A@proxy.example:3128"}, 200)
	if got := h.server.Pool.Proxy(); got.URL != saved || got.Revision != revision+1 {
		t.Fatal("redacted password not kept", got.Redacted())
	}
	put(map[string]any{"mode": "url", "url": "http://proxy%40user:next-secret@proxy.example:3128"}, 200)
	if h.server.Pool.Proxy().URL != "http://proxy%40user:next-secret@proxy.example:3128" {
		t.Fatal("new password not saved")
	}
	put(map[string]any{"mode": "direct"}, 200)
	// Without a saved password, the placeholder is ambiguous.
	put(map[string]any{"mode": "url", "url": "http://proxy%40user:****@proxy.example:3128"}, 400)
}

func TestConfigurationResponsesRedactProxyPasswordsExceptCredentialExport(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
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
	stored, _ := h.server.DB.ApplicationConfiguration("openai/codex")
	vendor, _ := h.server.DB.VendorConfiguration("openai")
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
