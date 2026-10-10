package httpserver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
)

// policyHTTPApp signs in and creates the HTTP cache application
// policy-test/files sourced from source; it returns the admin API path.
func policyHTTPApp(t *testing.T, source string) (*harness, store.Application, string) {
	t.Helper()
	h := newHarness(t)
	h.login(h.password)
	vendor := h.createVendor("policy-test")
	app := h.createApp(vendor.ID, "files", application.HttpCache, map[string]any{"base_url": source})
	return h, app, "/admin/api/apps/" + app.Key
}

// setCachePolicy saves an HTTP cache policy through the store, independent of
// the configuration API.
func setCachePolicy(t *testing.T, h *harness, key string, config cachepolicy.Config) store.Application {
	t.Helper()
	current, err := h.store.Application(key)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := h.store.SaveHTTPPolicy(key, current.Revision, config)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

// updateApp edits an application through the store; change starts from the
// saved fields (BaseURLs is nil so that a BaseURL edit replaces the sources).
func updateApp(t *testing.T, h *harness, key string, change func(*store.ApplicationChanges)) store.Application {
	t.Helper()
	a, err := h.store.Application(key)
	if err != nil {
		t.Fatal(err)
	}
	c := store.ApplicationChanges{Name: a.Name, Description: a.Description, Icon: a.Icon, BaseURL: a.BaseURL, SourceStrategy: a.SourceStrategy, CacheTTLSeconds: a.CacheTTLSeconds, Enabled: a.Enabled}
	change(&c)
	updated, err := h.store.UpdateApplication(key, a.Revision, c)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

// cacheEntries reads every cache entry page of api (an application's admin
// API path) with query parameters appended to the first request.
func cacheEntries(t *testing.T, h *harness, api, query string) []cacheEntryDTO {
	t.Helper()
	var all []cacheEntryDTO
	path := api + "/cache/entries" + query
	for {
		body, _ := h.request("GET", path, nil, 200, nil)
		page := decodeJSONBody[cacheEntryPageDTO](t, body)
		all = append(all, page.Items...)
		if page.NextCursor == nil {
			return all
		}
		separator := "?"
		if query != "" {
			separator = query + "&"
		}
		path = api + "/cache/entries" + separator + "cursor=" + *page.NextCursor
	}
}

func TestHTTPCachePolicyChangesLiveTTLAndStaleFallback(t *testing.T) {
	var requests atomic.Int32
	var fail atomic.Bool
	payload := "a rule can retain this explicitly selected private response"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		fmt.Fprint(w, payload)
	}))
	defer upstream.Close()
	h, app, api := policyHTTPApp(t, upstream.URL)
	public := "/" + app.Key + "/tool.bin"
	h.request("GET", public, nil, 200, nil)
	h.request("GET", public, nil, 200, nil)
	if requests.Load() != 2 || len(cacheEntries(t, h, api, "")) != 0 {
		t.Fatal("unmatched private/no-store source was shared")
	}
	rule := cachepolicy.CacheRule{Match: pathmatch.Spec{Type: "glob", Pattern: "/"}, TTLSeconds: 0}
	setCachePolicy(t, h, app.Key, cachepolicy.Config{Rules: []cachepolicy.CacheRule{rule}, AutoCleanup: []cachepolicy.CleanupRule{}, StaleFallback: true})
	h.request("GET", public, nil, 200, nil)
	h.request("GET", public, nil, 200, nil)
	entries := cacheEntries(t, h, api, "")
	if requests.Load() != 4 || len(entries) != 1 || entries[0].Path != "/tool.bin" || entries[0].FreshUntil.After(entries[0].ValidatedAt) {
		t.Fatal("explicit TTL 0 failed to retain body or revalidate each request", entries)
	}
	fail.Store(true)
	body, _ := h.request("GET", public, nil, 200, nil)
	if string(body) != payload {
		t.Fatalf("enabled stale fallback lost retained body: %q", body)
	}
	setCachePolicy(t, h, app.Key, cachepolicy.Config{Rules: []cachepolicy.CacheRule{rule}, AutoCleanup: []cachepolicy.CleanupRule{}, StaleFallback: false})
	h.request("GET", public, nil, 502, nil)
}
