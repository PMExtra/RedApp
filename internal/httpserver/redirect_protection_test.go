package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
)

func TestGlobalRedirectProtectionSettings(t *testing.T) {
	h := newHarness(t)
	h.login("")
	body, headers := h.request("GET", "/admin/api/settings/redirect-protection", nil, 200, nil)
	if string(body) != `{"mode":"strict","revision":1}`+"\n" || headers.Get("ETag") != `"1"` {
		t.Fatal(string(body), headers)
	}
	h.expectError("PUT", "/admin/api/settings/redirect-protection", map[string]string{"mode": "inherit"}, 400, codeValidationFailed, ifMatchHeader(1))
	if body, _ = h.request("PUT", "/admin/api/settings/redirect-protection", map[string]string{"mode": "basic"}, 200, ifMatchHeader(1)); string(body) != `{"mode":"basic","revision":2}`+"\n" {
		t.Fatal(string(body))
	}
	h.expectError("PUT", "/admin/api/settings/redirect-protection", map[string]string{"mode": "off"}, 409, codeRevisionConflict, ifMatchHeader(1))
	h.createVendor("acme")
	app := h.createApp("acme", "redirects", application.HttpCache, map[string]any{"base_url": "https://files.example/root"})
	c := h.configuration("apps/" + app.Key)
	if c.RedirectEffective.Mode != "basic" || c.RedirectEffective.SourceScope != "global" {
		t.Fatal("effective redirect protection", c.RedirectEffective)
	}
}

// A refused redirect names both URLs and the reason in the application's
// events; relaxing the protection lets the same file through.
func TestRedirectProtectionOfHTTPCache(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("redirected payload"))
	}))
	t.Cleanup(target.Close)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/cdn/file.bin?Signature=secret", http.StatusFound)
	}))
	t.Cleanup(source.Close)
	h := newHarness(t)
	h.login("")
	h.createVendor("acme")
	app := h.createApp("acme", "mirror", application.HttpCache, map[string]any{"base_url": source.URL + "/files", "cache_ttl_seconds": 0})
	if code, _, _ := h.raw("GET", "/"+app.Key+"/file.bin", nil, "", nil); code == 200 {
		t.Fatal("strict protection followed a redirect to another origin")
	}
	body, _ := h.request("GET", "/admin/api/events?limit=50", nil, 200, nil)
	var events struct {
		Items []struct {
			Code, Message string
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &events); err != nil {
		t.Fatal(err)
	}
	want := "Upstream redirect from " + source.URL + "/files/file.bin to " + target.URL + "/cdn/file.bin is not allowed: a redirect to another origin requires HTTPS"
	found := false
	for _, event := range events.Items {
		if event.Code == "upstream_redirect" && event.Message == want {
			found = true
		}
		if strings.Contains(event.Message, "secret") {
			t.Fatal("event exposed the query", event.Message)
		}
	}
	if !found {
		t.Fatalf("no refused redirect event: %s", body)
	}
	h.patchConfiguration("apps/"+app.Key, map[string]any{"set": map[string]any{"redirect_protection": "off"}}, 200)
	if body, _ = h.request("GET", "/"+app.Key+"/file.bin", nil, 200, nil); string(body) != "redirected payload" {
		t.Fatal(string(body))
	}
}
