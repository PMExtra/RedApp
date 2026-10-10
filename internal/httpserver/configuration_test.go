package httpserver

import (
	"bytes"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
)

func TestConfigurationOverridesResetToTemplateWithoutTouchingState(t *testing.T) {
	h := newHarness(t)
	h.login("")
	path := "/admin/api/apps/openai/codex/configuration"
	data, headers := h.request("GET", path, nil, 200, nil)
	c := decodeJSONBody[configurationDTO](t, data)
	if c.TemplateRef == nil || *c.TemplateRef != "openai/codex" || c.Defaults == nil || c.Fields["name.en"].Source != "inherited" || headers.Get("ETag") != etag(c.Revision) || c.Revision != h.adminApp("openai/codex").Revision {
		t.Fatal("linked configuration", string(data))
	}
	for _, absent := range []string{"base_urls", "source_strategy", "http_policy.rules"} {
		if _, ok := c.Fields[absent]; ok {
			t.Fatal("field of another provider", absent)
		}
	}
	if _, ok := c.Effective["base_urls"]; ok || c.Effective["retention"] == nil || c.Effective["prewarm"] == nil {
		t.Fatal("effective spec is not projected to the provider", c.Effective)
	}
	h.expectError("PATCH", path, map[string]any{"set": map[string]any{"name.en": "x"}}, 400, codeIfMatchRequired, nil)
	h.expectError("PATCH", path, map[string]any{"set": map[string]any{"name.en": "x"}}, 409, codeRevisionConflict, ifMatchHeader(c.Revision+1))
	for status, bodies := range map[errorCode][]map[string]any{
		codeInvalidRequest: {
			{"set": map[string]any{"enabled": false}},
			{"set": map[string]any{"name.en": nil}},
			{"revision": c.Revision, "set": map[string]any{"name.en": "x"}},
		},
		codeValidationFailed: {
			{"set": map[string]any{"name.en": "x"}, "unset": []string{"name.en"}},
			{"unset": []string{"enabled"}},
			{"set": map[string]any{"base_urls": []string{"https://example.com"}}},
			{"unset": []string{"http_policy.rules"}},
			{"set": map[string]any{"cache_ttl_seconds": 0}},
			{"set": map[string]any{"name.en": "x"}, "new_categories": []string{"Tools"}},
		},
	} {
		for _, body := range bodies {
			h.expectError("PATCH", path, body, 400, status, ifMatchHeader(c.Revision))
		}
	}
	data, _ = h.request("PATCH", path, map[string]any{"set": map[string]any{"name.en": "My Codex", "description.zh-CN": "", "instructions.en": "## Custom"}}, 200, ifMatchHeader(c.Revision))
	c = decodeJSONBody[configurationDTO](t, data)
	if c.Fields["name.en"].Source != "custom" || !*c.Fields["name.en"].Differs || c.Fields["instructions.en"].Source != "custom" || c.Effective["name"].(map[string]any)["en"] != "My Codex" {
		t.Fatal("override not applied", string(data))
	}
	h.setEnabled("apps/openai/codex", false)
	c = h.configuration("apps/openai/codex")
	data, _ = h.request("PATCH", path, map[string]any{"unset": []string{"name.en", "description.zh-CN", "instructions.en"}}, 200, ifMatchHeader(c.Revision))
	reset := decodeJSONBody[configurationDTO](t, data)
	if reset.Revision != c.Revision+1 || reset.Fields["name.en"].Source != "inherited" || reset.Fields["instructions.en"].Source != "inherited" || len(reset.Overrides) != 0 {
		t.Fatal("reset wrote defaults instead of unsetting", string(data))
	}
	if app := h.adminApp("openai/codex"); app.Enabled || app.Revision != reset.Revision {
		t.Fatal("field reset changed the enabled state", app)
	}
	// An empty patch checks If-Match and returns the current state.
	data, _ = h.request("PATCH", path, map[string]any{}, 200, ifMatchHeader(reset.Revision))
	if decodeJSONBody[configurationDTO](t, data).Revision != reset.Revision {
		t.Fatal("empty patch changed the revision")
	}
	for _, public := range []string{"/api/bootstrap", "/api/catalog"} {
		raw, _ := h.request("GET", public, nil, 200, nil)
		for _, private := range []string{"template_ref", "overrides", "effective", "defaults", "proxy_effective"} {
			if bytes.Contains(raw, []byte(`"`+private+`"`)) {
				t.Fatal("configuration leaked publicly", public, private)
			}
		}
	}
	h.expectError("GET", "/admin/api/apps/openai/missing/configuration", nil, 404, codeApplicationNotFound, nil)
	h.expectError("PATCH", "/admin/api/apps/openai/missing/configuration", map[string]any{}, 404, codeApplicationNotFound, ifMatchHeader(1))
}

func TestConfigurationOfIndependentAndDeletedEntities(t *testing.T) {
	h := newHarness(t)
	h.login("")
	h.request("POST", "/admin/api/vendors", map[string]any{"id": "acme", "name": map[string]string{"en": "Acme", "zh-CN": "Acme"}, "enabled": true}, 201, nil)
	c := h.configuration("vendors/acme")
	if c.TemplateRef != nil || c.Defaults != nil || c.TemplateHash != nil || c.Fields["icon"].Differs != nil || c.Fields["icon"].Source != "custom" || c.Effective["proxy"].(map[string]any)["mode"] != "inherit" {
		t.Fatal("independent vendor configuration", c)
	}
	expectCode(t, h.patchConfiguration("vendors/acme", map[string]any{"unset": []string{"description.en"}}, 400), codeValidationFailed)
	expectCode(t, h.patchConfiguration("vendors/acme", map[string]any{"set": map[string]any{"instructions.en": "x"}}, 400), codeInvalidRequest)
	expectCode(t, h.patchConfiguration("vendors/acme", map[string]any{"new_categories": []string{"x"}}, 400), codeInvalidRequest)
	data := h.patchConfiguration("vendors/acme", map[string]any{"set": map[string]any{"localized_icons.en": "", "name.zh-CN": "极点"}}, 200)
	if saved := decodeJSONBody[configurationDTO](t, data); saved.Revision != c.Revision+1 || h.adminVendor("acme").Name.ZhCN != "极点" {
		t.Fatal("independent vendor spec not saved", string(data))
	}
	app := h.createApp("acme", "files", application.HttpCache, map[string]any{"base_url": "http://intranet.example/files"})
	files := h.configuration("apps/" + app.Key)
	for _, field := range []string{"base_urls", "source_strategy", "http_policy.rules", "cache_ttl_seconds"} {
		if _, ok := files.Fields[field]; !ok {
			t.Fatal("http-cache field missing", field)
		}
	}
	for _, field := range []string{"base_url", "retention", "prewarm"} {
		if _, ok := files.Fields[field]; ok {
			t.Fatal("release field on http-cache", field)
		}
	}
	expectCode(t, h.patchConfiguration("apps/"+app.Key, map[string]any{"set": map[string]any{"base_url": "https://example.com"}}, 400), codeValidationFailed)
	// A new source starts a new epoch and storage namespace, keeping the metrics identity.
	data = h.patchConfiguration("apps/"+app.Key, map[string]any{"set": map[string]any{"base_urls": []string{"https://new.example/files", "https://mirror.example/files"}, "source_strategy": "round_robin"}}, 200)
	effective := decodeJSONBody[configurationDTO](t, data).Effective
	rebound, _ := h.store.Application(app.Key)
	if rebound.SourceEpoch != 2 || rebound.StorageID() == app.StorageID() || rebound.MetricsID() != app.MetricsID() || len(effective["base_urls"].([]any)) != 2 || effective["source_strategy"] != "round_robin" {
		t.Fatal("source change did not start a new epoch", rebound, effective)
	}
	if err := h.store.DeleteApplication(app.Key, rebound.Revision); err != nil {
		t.Fatal(err)
	}
	expectCode(t, h.patchConfiguration("apps/"+app.Key, map[string]any{"set": map[string]any{"name.en": "x"}}, 409), codeEntityDeleted)
}

func TestConfigurationRuntimeAndDatabaseStayAlignedOnFailure(t *testing.T) {
	var fail atomic.Bool
	h := newHarness(t, withOptions(WithConfigurationCheck(func(store.DirectorySnapshot) error {
		if fail.Load() {
			return errors.New("injected prepare failure")
		}
		return nil
	})))
	h.login("")
	key := "openai/codex"
	before, _ := h.store.Application(key)
	entry, _ := h.server.registry.Lookup(key)
	path := "/admin/api/apps/" + key + "/configuration"
	body := map[string]any{"set": map[string]any{"base_url": "https://replacement.example/codex"}}
	fail.Store(true)
	h.expectError("PATCH", path, body, 503, codeStorageUnavailable, ifMatchHeader(before.Revision))
	fail.Store(false)
	if _, err := h.sql().Exec(`CREATE TRIGGER reject_runtime_config BEFORE UPDATE ON applications BEGIN SELECT RAISE(FAIL,'injected DB failure'); END`); err != nil {
		t.Fatal(err)
	}
	h.expectError("PATCH", path, body, 503, codeStorageUnavailable, ifMatchHeader(before.Revision))
	after, _ := h.store.Application(key)
	current, _ := h.server.registry.Lookup(key)
	if after.Revision != before.Revision || after.SourceEpoch != before.SourceEpoch || after.BaseURL != before.BaseURL || current.Upstream != entry.Upstream || current.Revision != entry.Revision {
		t.Fatal("half-saved configuration", after, current)
	}
	candidateID := identity.StorageID(before.UID, before.SourceEpoch+1)
	client, err := builtin.NewScopedSourceClient(before.Provider, "https://other.example/codex", "", before.UID, before.VendorUID, h.server.pool)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := h.server.downloads.PrepareUpstreams(map[string]*distributor.Client{candidateID: client})
	if err != nil {
		t.Fatal("aborted namespace leaked into downloads", err)
	}
	plan.Abort()
	if _, err = h.sql().Exec(`DROP TRIGGER reject_runtime_config`); err != nil {
		t.Fatal(err)
	}
	data, _ := h.request("PATCH", path, body, 200, ifMatchHeader(before.Revision))
	saved := decodeJSONBody[configurationDTO](t, data)
	current, _ = h.server.registry.Lookup(key)
	if current.Revision != saved.Revision || current.SourceEpoch != before.SourceEpoch+1 || current.Upstream.Base.String() != "https://replacement.example/codex" {
		t.Fatal("published runtime differs from the database", current, saved)
	}
	if err = h.store.CheckSourceActive(entry.StorageID(), store.SourceFence{AppRuntimeRevision: entry.RuntimeRevision, VendorRuntimeRevision: entry.VendorRuntimeRevision}); !errors.Is(err, store.ErrSourceInactive) {
		t.Fatal("old source fence accepted", err)
	}
	// Release settings that used to have their own endpoint are configuration paths.
	data = h.patchConfiguration("apps/"+key, map[string]any{"set": map[string]any{"cache_ttl_seconds": 120}}, 200)
	if entry, _ = h.server.registry.Lookup(key); entry.Descriptor.DefaultChannelTTLSeconds != 120 || decodeJSONBody[configurationDTO](t, data).Effective["cache_ttl_seconds"] != float64(120) {
		t.Fatal("channel TTL not published", string(data))
	}
	h.request("GET", "/admin/api/apps/"+key+"/settings", nil, 404, nil)
	h.request("GET", "/admin/api/apps/"+key+"/instructions", nil, 404, nil)
	h.request("GET", "/admin/api/assets/builtin-icon", nil, 404, nil)
}
