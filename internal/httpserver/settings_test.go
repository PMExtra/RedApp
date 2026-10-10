package httpserver

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/store"
)

func settingsIfMatch(revision int64) map[string]string {
	return map[string]string{"If-Match": fmt.Sprintf(`"%d"`, revision)}
}

func settingsTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestGlobalProxyRedactsPasswordAndKeepsItOnlyForSameProxy(t *testing.T) {
	h := newHarness(t)
	h.login("")
	body, headers := h.request("GET", "/admin/api/settings/proxy", nil, 200, nil)
	initial := decodeJSONBody[globalProxyStateDTO](t, body)
	if initial.Mode != "direct" || initial.DNS != "local" || initial.Revision != 1 || headers.Get("ETag") != `"1"` || strings.Contains(string(body), `"url"`) {
		t.Fatal("initial proxy", string(body))
	}
	put := func(input map[string]any, status int) []byte {
		t.Helper()
		data, _ := h.request("PUT", "/admin/api/settings/proxy", input, status, settingsIfMatch(h.server.pool.Proxy().Revision))
		return data
	}
	saved := "http://proxy%40user:global-secret@proxy.example:3128"
	for _, data := range [][]byte{put(map[string]any{"mode": "url", "url": saved}, 200), func() []byte { data, _ := h.request("GET", "/admin/api/settings/proxy", nil, 200, nil); return data }()} {
		view := decodeJSONBody[globalProxyStateDTO](t, data)
		if bytes.Contains(data, []byte("global-secret")) || view.URL != "http://proxy%40user:****@proxy.example:3128" || view.Mode != "url" || view.DNS != "proxy" || view.Revision != 2 {
			t.Fatal("global proxy view", string(data))
		}
	}
	revision := h.server.pool.Proxy().Revision
	// The placeholder may not carry the saved password to another user, scheme or host.
	for _, moved := range []string{"http://other:****@proxy.example:3128", "socks5://proxy%40user:****@proxy.example:3128", "http://proxy%40user:****@attacker.example:3128", "http://proxy%40user:****@proxy.example:3129"} {
		data, _ := h.request("PUT", "/admin/api/settings/proxy", map[string]any{"mode": "url", "url": moved}, 400, settingsIfMatch(revision))
		if errorCodeOf(t, data) != string(codeProxyRedactedMismatch) || bytes.Contains(data, []byte("global-secret")) {
			t.Fatal("redacted mismatch", string(data))
		}
	}
	for _, invalid := range []map[string]any{{"mode": "inherit"}, {"mode": "url"}, {"mode": "direct", "url": saved}, {"mode": "url", "url": "http://fixture:private-proxy-secret@proxy.example"}, {"mode": "url", "url": "ftp://proxy.example:21"}} {
		data, _ := h.request("PUT", "/admin/api/settings/proxy", invalid, 400, settingsIfMatch(revision))
		if errorCodeOf(t, data) != string(codeValidationFailed) || bytes.Contains(data, []byte("secret")) {
			t.Fatal("invalid proxy", invalid, string(data))
		}
	}
	// The removed compatibility field, an implicit mode and a body revision are rejected.
	for _, invalid := range []map[string]any{{"server": saved}, {"url": saved}, {"mode": "direct", "revision": revision}} {
		h.expectError("PUT", "/admin/api/settings/proxy", invalid, 400, codeInvalidRequest, settingsIfMatch(revision))
	}
	h.expectError("PUT", "/admin/api/settings/proxy", map[string]any{"mode": "direct"}, 400, codeIfMatchRequired, nil)
	h.expectError("PUT", "/admin/api/settings/proxy", map[string]any{"mode": "direct"}, 409, codeRevisionConflict, settingsIfMatch(revision-1))
	if got := h.server.pool.Proxy(); got.URL != saved || got.Revision != revision {
		t.Fatal("rejected update changed proxy", got.Redacted())
	}
	put(map[string]any{"mode": "url", "url": "http://proxy%40user:%2A%2A%2A%2A@proxy.example:3128"}, 200)
	if got := h.server.pool.Proxy(); got.URL != saved || got.Revision != revision+1 {
		t.Fatal("redacted password not kept", got.Redacted())
	}
	put(map[string]any{"mode": "url", "url": "http://proxy%40user:next-secret@proxy.example:3128"}, 200)
	if h.server.pool.Proxy().URL != "http://proxy%40user:next-secret@proxy.example:3128" {
		t.Fatal("new password not saved")
	}
	if data := put(map[string]any{"mode": "direct"}, 200); strings.Contains(string(data), `"url"`) {
		t.Fatal("direct proxy kept a URL", string(data))
	}
	// Without a saved password, the placeholder is ambiguous.
	data := put(map[string]any{"mode": "url", "url": "http://proxy%40user:****@proxy.example:3128"}, 400)
	if errorCodeOf(t, data) != string(codeProxyRedactedMismatch) {
		t.Fatal("placeholder without saved password", string(data))
	}
	for _, path := range []string{"/api/bootstrap", "/api/home"} {
		if data, _ := h.request("GET", path, nil, 200, nil); bytes.Contains(data, []byte("secret")) {
			t.Fatal("proxy secret leaked", path)
		}
	}
}

func TestPublicURLOverrideValidationAndEffectiveURL(t *testing.T) {
	h := newHarness(t)
	h.login("")
	read := func() publicURLStateDTO {
		t.Helper()
		body, headers := h.request("GET", "/admin/api/settings/public-url", nil, 200, nil)
		state := decodeJSONBody[publicURLStateDTO](t, body)
		if headers.Get("ETag") != fmt.Sprintf(`"%d"`, state.Revision) {
			t.Fatal("ETag", headers)
		}
		return state
	}
	initial := read()
	if initial.Revision != 1 || initial.Source != "request" || initial.OverrideURL != nil || initial.EnvironmentURL != nil || initial.EffectiveURL != h.http.URL {
		t.Fatal("initial public URL", initial)
	}
	for _, invalid := range []string{"", "downloads.example", "ftp://downloads.example", "https://user:pass@downloads.example", "https://downloads.example/sub", "https://downloads.example/?q=1", "https://downloads.example/#f", "https://downloads.example:0", "https://" + strings.Repeat("a", 2048) + ".example"} {
		h.expectError("PUT", "/admin/api/settings/public-url", map[string]any{"override_url": invalid}, 400, codeValidationFailed, settingsIfMatch(1))
	}
	for _, invalid := range []map[string]any{{}, {"override_url": 7}, {"override_url": nil, "revision": 1}} {
		h.expectError("PUT", "/admin/api/settings/public-url", invalid, 400, codeInvalidRequest, settingsIfMatch(1))
	}
	h.expectError("PUT", "/admin/api/settings/public-url", map[string]any{"override_url": nil}, 400, codeIfMatchRequired, nil)
	body, _ := h.request("PUT", "/admin/api/settings/public-url", map[string]any{"override_url": "HTTPS://Downloads.Example.Internal/"}, 200, settingsIfMatch(1))
	saved := decodeJSONBody[publicURLStateDTO](t, body)
	if saved.Revision != 2 || saved.Source != "override" || saved.OverrideURL == nil || *saved.OverrideURL != "https://downloads.example.internal" || saved.EffectiveURL != *saved.OverrideURL {
		t.Fatal("override not normalized", string(body))
	}
	data, _ := h.request("GET", "/api/bootstrap", nil, 200, nil)
	if !bytes.Contains(data, []byte(`"public_url":"https://downloads.example.internal"`)) {
		t.Fatal("bootstrap ignored the override", string(data))
	}
	h.expectError("PUT", "/admin/api/settings/public-url", map[string]any{"override_url": nil}, 409, codeRevisionConflict, settingsIfMatch(1))
	body, _ = h.request("PUT", "/admin/api/settings/public-url", map[string]any{"override_url": nil}, 200, settingsIfMatch(2))
	if cleared := decodeJSONBody[publicURLStateDTO](t, body); cleared.OverrideURL != nil || cleared.Source != "request" || cleared.Revision != 3 {
		t.Fatal("override not cleared", string(body))
	}
	// The effective URL follows the verified request origin.
	r := httptest.NewRequest("GET", "/admin/api/settings/public-url", nil)
	r.Host = "remote.example:9443"
	for _, cookie := range h.client.Jar.Cookies(settingsTestURL(t, h.http.URL+"/admin")) {
		r.AddCookie(cookie)
	}
	if w := h.serve(r); w.Code != 200 || !strings.Contains(w.Body.String(), `"effective_url":"http://remote.example:9443"`) {
		t.Fatal("effective URL ignored the request origin", w.Code, w.Body.String())
	}
}

func TestHomepagePinsKeepOrderAndShowPinState(t *testing.T) {
	h := newHarness(t)
	h.login("")
	h.createVendor("acme")
	for _, id := range []string{"one", "two", "three"} {
		h.createApp("acme", id, application.Info, map[string]any{"name": store.LocalizedText{En: "App " + id, ZhCN: "应用" + id}})
	}
	body, headers := h.request("GET", "/admin/api/settings/homepage", nil, 200, nil)
	initial := decodeJSONBody[homepageSettingsDTO](t, body)
	if initial.Revision != 1 || headers.Get("ETag") != `"1"` || len(initial.PinnedAppKeys) != 0 || len(initial.PinnedApps) != 0 {
		t.Fatal("initial homepage", string(body))
	}
	pins := []string{"acme/three", "acme/one", "openai/codex"}
	body, headers = h.request("PUT", "/admin/api/settings/homepage", map[string]any{"pinned_app_keys": pins}, 200, settingsIfMatch(1))
	saved := decodeJSONBody[homepageSettingsDTO](t, body)
	if saved.Revision != 2 || headers.Get("ETag") != `"2"` || strings.Join(saved.PinnedAppKeys, ",") != strings.Join(pins, ",") || len(saved.PinnedApps) != 3 {
		t.Fatal("pins lost order", string(body))
	}
	if first := saved.PinnedApps[0]; first.Key != "acme/three" || first.Name == nil || first.Name.En != "App three" || first.Name.ZhCN != "应用three" || first.Icon == nil || first.State != "published" {
		t.Fatal("pin display data", first)
	}
	data, _ := h.request("GET", "/api/home", nil, 200, nil)
	if !bytes.Contains(data, []byte("acme/three")) {
		t.Fatal("pin missing from the public homepage", string(data))
	}
	// Disabled and deleted applications stay pinned and are labelled.
	updateApp(t, h, "acme/one", func(c *store.ApplicationChanges) { c.Enabled = false })
	three, _ := h.store.Application("acme/three")
	if err := h.store.DeleteApplication("acme/three", three.Revision); err != nil {
		t.Fatal(err)
	}
	body, _ = h.request("GET", "/admin/api/settings/homepage", nil, 200, nil)
	states := []string{}
	for _, pin := range decodeJSONBody[homepageSettingsDTO](t, body).PinnedApps {
		states = append(states, pin.Key+"="+pin.State)
	}
	if strings.Join(states, ",") != "acme/three=deleted,acme/one=disabled,openai/codex=published" {
		t.Fatal("pin states", states)
	}
	data, _ = h.request("GET", "/api/home", nil, 200, nil)
	if bytes.Contains(data, []byte("acme/one")) || bytes.Contains(data, []byte("acme/three")) {
		t.Fatal("unpublished pin leaked to the public homepage", string(data))
	}
	revision := decodeJSONBody[homepageSettingsDTO](t, body).Revision
	for _, invalid := range [][]string{{"acme/two", "acme/two"}, {"acme/missing"}, {"acme/three"}, {"Acme/two"}, {"acme"}} {
		h.expectError("PUT", "/admin/api/settings/homepage", map[string]any{"pinned_app_keys": invalid}, 400, codeValidationFailed, settingsIfMatch(revision))
	}
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("acme/app-%d", i)
	}
	h.expectError("PUT", "/admin/api/settings/homepage", map[string]any{"pinned_app_keys": tooMany}, 400, codeValidationFailed, settingsIfMatch(revision))
	for _, invalid := range []map[string]any{{}, {"keys": []string{}}, {"pinned_app_keys": []string{}, "revision": revision}} {
		h.expectError("PUT", "/admin/api/settings/homepage", invalid, 400, codeInvalidRequest, settingsIfMatch(revision))
	}
	h.expectError("PUT", "/admin/api/settings/homepage", map[string]any{"pinned_app_keys": []string{}}, 400, codeIfMatchRequired, nil)
	h.expectError("PUT", "/admin/api/settings/homepage", map[string]any{"pinned_app_keys": []string{}}, 409, codeRevisionConflict, settingsIfMatch(revision-1))
	body, _ = h.request("PUT", "/admin/api/settings/homepage", map[string]any{"pinned_app_keys": []string{"acme/two"}}, 200, settingsIfMatch(revision))
	if after := decodeJSONBody[homepageSettingsDTO](t, body); strings.Join(after.PinnedAppKeys, ",") != "acme/two" || after.Revision != revision+1 {
		t.Fatal("replacement", string(body))
	}
}
