package httpserver

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
)

func TestHTTPCacheSourcesValidateAndStartNewEpochs(t *testing.T) {
	h := newHarness(t)
	h.login("")
	h.createVendor("source-test")
	name := map[string]string{"en": "Multiple sources", "zh-CN": "多个源"}
	create := func(body map[string]any, status int) []byte {
		t.Helper()
		body["vendor"], body["name"], body["enabled"] = "source-test", name, true
		data, _ := h.request("POST", "/admin/api/apps", body, status, nil)
		return data
	}
	base := []string{"http://127.0.0.1:9/first", "http://127.0.0.1:9/second"}
	tooMany := make([]string, 17)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("http://127.0.0.1:9/source-%d", i)
	}
	invalidSources := [][]string{{}, tooMany, {base[0], base[0]}, {base[0], base[0] + "/"}, {"https://user:password@example.test/files"},
		{"ftp://example.test/files"}, {"example.test/files"}, {"https://example.test/files?query=1"}, {"https://example.test/files#fragment"}}
	for _, values := range invalidSources {
		expectCode(t, create(map[string]any{"id": "invalid", "provider": application.HttpCache, "base_urls": values, "source_strategy": "ordered"}, 400), codeValidationFailed)
	}
	sixteen := tooMany[:16]
	maximal := decodeJSONBody[appDTO](t, create(map[string]any{"id": "sixteen", "provider": application.HttpCache, "base_urls": sixteen}, 201))
	if !reflect.DeepEqual(maximal.BaseURLs, sixteen) {
		t.Fatal("16 sources", maximal.BaseURLs)
	}
	for _, strategy := range []string{"ordered", "round_robin", "random"} {
		created := decodeJSONBody[appDTO](t, create(map[string]any{"id": "strategy-" + strings.ReplaceAll(strategy, "_", "-"), "provider": application.HttpCache, "base_urls": base, "source_strategy": strategy}, 201))
		if created.SourceStrategy != strategy {
			t.Fatal("source strategy", created)
		}
	}
	expectCode(t, create(map[string]any{"id": "invalid-strategy", "provider": application.HttpCache, "base_urls": base, "source_strategy": "fastest"}, 400), codeValidationFailed)
	for _, provider := range []string{application.Codex, application.ClaudeCode} {
		expectCode(t, create(map[string]any{"id": "not-multi-" + provider, "provider": provider, "base_urls": base}, 400), codeValidationFailed)
		expectCode(t, create(map[string]any{"id": "not-strategy-" + provider, "provider": provider, "source_strategy": "ordered"}, 400), codeValidationFailed)
	}
	app := decodeJSONBody[appDTO](t, create(map[string]any{"id": "files", "provider": application.HttpCache, "base_urls": base}, 201))
	if app.SourceEpoch != 1 || !reflect.DeepEqual(app.BaseURLs, base) || app.SourceStrategy != "ordered" {
		t.Fatal("multi-source defaults or order lost", app)
	}
	path := "apps/" + app.Key
	patch := func(set map[string]any) {
		t.Helper()
		h.patchConfiguration(path, map[string]any{"set": set}, 200)
		app = h.adminApp(app.Key)
	}
	patch(map[string]any{"name.en": "New label", "cache_ttl_seconds": 0, "http_policy.stale_fallback": true})
	if app.SourceEpoch != 1 || !reflect.DeepEqual(app.BaseURLs, base) || *app.CacheTTLSeconds != 0 {
		t.Fatal("metadata, TTL or policy changed the source identity", app)
	}
	reordered := []string{base[1], base[0]}
	if patch(map[string]any{"base_urls": reordered}); app.SourceEpoch != 2 || !reflect.DeepEqual(app.BaseURLs, reordered) {
		t.Fatal("reordering did not isolate its cache namespace", app)
	}
	if patch(map[string]any{"source_strategy": "round_robin"}); app.SourceEpoch != 3 || app.SourceStrategy != "round_robin" {
		t.Fatal("source strategy did not change the epoch", app)
	}
	replaced := []string{base[1], "http://127.0.0.1:9/replacement"}
	if patch(map[string]any{"base_urls": replaced}); app.SourceEpoch != 4 || !reflect.DeepEqual(app.BaseURLs, replaced) {
		t.Fatal("replacement did not isolate its cache namespace", app)
	}
	if patch(map[string]any{"base_urls": replaced, "source_strategy": "round_robin"}); app.SourceEpoch != 4 {
		t.Fatal("unchanged sources started another epoch", app)
	}
	// The same rules apply when the sources are edited.
	for _, values := range invalidSources {
		expectCode(t, h.patchConfiguration(path, map[string]any{"set": map[string]any{"base_urls": values}}, 400), codeValidationFailed)
	}
	expectCode(t, h.patchConfiguration(path, map[string]any{"set": map[string]any{"source_strategy": "fastest"}}, 400), codeValidationFailed)
	if unchanged := h.adminApp(app.Key); unchanged.SourceEpoch != 4 || unchanged.Revision != app.Revision {
		t.Fatal("rejected source edit changed the application", unchanged)
	}
	sources, err := h.store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	epochs := 0
	for _, source := range sources {
		if source.AppUID == app.UID {
			epochs++
			if source.Active != (source.Epoch == 4) {
				t.Fatal("incorrect current source", source)
			}
		}
	}
	if epochs != 4 {
		t.Fatal("historical source snapshots not retained", epochs)
	}
	body, _ := h.request("GET", "/api/bootstrap", nil, 200, nil)
	for _, private := range []string{`"base_urls"`, `"source_strategy"`, `"stale_fallback"`} {
		if bytes.Contains(body, []byte(private)) {
			t.Fatalf("public bootstrap exposed source policy %s", private)
		}
	}
}
