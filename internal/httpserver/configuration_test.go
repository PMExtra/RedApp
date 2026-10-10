package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/identity"
	"sync/atomic"
	"testing"

	"github.com/PMExtra/RedApp/internal/store"
)

func configurationValue(t *testing.T, raw []byte) store.Configuration {
	t.Helper()
	var c store.Configuration
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestConfigurationAPIAndLegacyWritersShareAuthority(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	endpoint := "/admin/api/apps/openai/codex/configuration"
	raw, _ := h.request("GET", endpoint, nil, 200, nil)
	c := configurationValue(t, raw)
	if c.TemplateRef == nil || *c.TemplateRef != "openai/codex" || c.Fields["name.en"].Source != "inherited" {
		t.Fatal(c)
	}
	raw, _ = h.request("PATCH", endpoint, map[string]any{"revision": c.Revision, "set": map[string]any{"name.en": "My Codex", "description.en": ""}}, 200, nil)
	c = configurationValue(t, raw)
	if c.Fields["name.en"].Source != "custom" || !*c.Fields["name.en"].Differs {
		t.Fatal(c)
	}
	h.request("PATCH", endpoint, map[string]any{"revision": c.Revision, "set": map[string]any{"name.en": "bad"}}, 400, map[string]string{"If-Match": "\"999\""})
	h.request("PATCH", endpoint, map[string]any{"revision": c.Revision - 1, "set": map[string]any{"name.en": "bad"}}, 409, nil)
	h.request("PATCH", endpoint, map[string]any{"revision": c.Revision, "set": map[string]any{"enabled": false}}, 400, nil)
	h.request("PATCH", endpoint, map[string]any{"revision": c.Revision, "set": map[string]any{"name.en": nil}}, 400, nil)
	h.request("PATCH", "/admin/api/apps/openai/codex", map[string]any{"revision": c.Revision, "description": map[string]any{"zh-CN": "私人说明"}}, 200, nil)
	raw, _ = h.request("GET", endpoint, nil, 200, nil)
	c = configurationValue(t, raw)
	if c.Fields["description.zh-CN"].Source != "custom" || c.Fields["name.zh-CN"].Source != "inherited" || c.Fields["base_url"].Source != "inherited" {
		t.Fatal("legacy patch invented overrides", c)
	}
	app, _ := h.server.DB.Application("openai/codex")
	ins, _ := h.server.DB.Instructions(app.UID)
	h.request("PUT", "/admin/api/apps/openai/codex/instructions", map[string]any{"revision": ins.Revision, "en": "custom instructions", "zh-CN": ins.ZhCN}, 200, nil)
	raw, _ = h.request("GET", endpoint, nil, 200, nil)
	c = configurationValue(t, raw)
	if c.Revision != app.Revision+1 || c.Fields["instructions.en"].Source != "custom" {
		t.Fatal("instructions owner not updated", c)
	}
	h.request("PATCH", endpoint, map[string]any{"revision": c.Revision, "unset": []string{"name.en", "name.zh-CN", "description.en", "description.zh-CN", "instructions.en"}}, 200, nil)
	raw, _ = h.request("GET", endpoint, nil, 200, nil)
	c = configurationValue(t, raw)
	if c.Fields["name.en"].Source != "inherited" || c.Fields["instructions.en"].Source != "inherited" || c.Fields["instructions.zh-CN"].Source != "custom" {
		t.Fatal("reset wrote defaults rather than unsetting", c)
	}
	before := append([]byte(nil), raw...)
	h.request("GET", endpoint, nil, 200, nil)
	raw, _ = h.request("GET", endpoint, nil, 200, nil)
	if !bytes.Equal(before, raw) {
		t.Fatal("GET changed override state")
	}
	public, _ := h.request("GET", "/api/bootstrap", nil, 200, nil)
	for _, private := range []string{"template_ref", "template_hash", "overrides", "effective", "defaults", "template_missing"} {
		if bytes.Contains(public, []byte(`"`+private+`"`)) {
			t.Fatal("raw configuration leaked publicly", private)
		}
	}
}
func TestConfigurationIndependentEntityAndNoOp(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	h.request("POST", "/admin/api/vendors", map[string]any{"id": "acme", "name": map[string]string{"en": "Acme", "zh-CN": "Acme"}}, 201, nil)
	endpoint := "/admin/api/vendors/acme/configuration"
	raw, _ := h.request("GET", endpoint, nil, 200, nil)
	c := configurationValue(t, raw)
	if c.TemplateRef != nil || c.Defaults != nil || c.Fields["icon"].Differs != nil || c.Fields["icon"].Source != "custom" {
		t.Fatal(c)
	}
	raw, _ = h.request("PATCH", endpoint, map[string]any{"revision": c.Revision}, 200, nil)
	unchanged := configurationValue(t, raw)
	if unchanged.Revision != c.Revision {
		t.Fatal("empty patch changed revision")
	}
	h.request("PATCH", endpoint, map[string]any{"revision": c.Revision, "unset": []string{"description.en"}}, 400, nil)
	h.request("PATCH", endpoint, map[string]any{"revision": c.Revision, "set": map[string]any{"localized_icons.en": ""}}, 200, nil)
}

func TestConfigurationRuntimeAndDatabaseStayAlignedOnFailure(t *testing.T) {
	var fail atomic.Bool
	h := newDirectoryHarness(t, t.TempDir(), func(s *Server) {
		s.testConfigurationPrepare = func(store.DirectorySnapshot) error {
			if fail.Load() {
				return errors.New("injected prepare failure")
			}
			return nil
		}
	})
	h.login(h.password)
	key := "openai/codex"
	before, _ := h.server.DB.Application(key)
	entry, _ := h.server.Registry.Lookup(key)
	endpoint := "/admin/api/apps/" + key + "/configuration"
	body := map[string]any{"revision": before.Revision, "set": map[string]any{"base_url": "https://replacement.example/codex"}}
	fail.Store(true)
	h.request("PATCH", endpoint, body, 503, nil)
	fail.Store(false)
	if _, err := h.server.DB.DB.Exec(`CREATE TRIGGER reject_runtime_config BEFORE UPDATE ON applications BEGIN SELECT RAISE(FAIL,'injected DB failure'); END`); err != nil {
		t.Fatal(err)
	}
	h.request("PATCH", endpoint, body, 503, nil)
	after, _ := h.server.DB.Application(key)
	current, _ := h.server.Registry.Lookup(key)
	if after.Revision != before.Revision || after.SourceEpoch != before.SourceEpoch || after.BaseURL != before.BaseURL || current.Upstream != entry.Upstream || current.Revision != entry.Revision {
		t.Fatal("half-saved configuration", after, current)
	}
	candidateID := identity.StorageID(before.UID, before.SourceEpoch+1)
	client, err := builtin.NewScopedSourceClient(before.Provider, "https://other.example/codex", "", before.UID, before.VendorUID, h.server.Pool)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := h.server.Downloads.PrepareUpstreams(map[string]*distributor.Client{candidateID: client})
	if err != nil {
		t.Fatal("aborted namespace leaked into Downloads", err)
	}
	plan.Abort()
	h.server.DB.DB.Exec(`DROP TRIGGER reject_runtime_config`)
	raw, _ := h.request("PATCH", endpoint, body, 200, nil)
	saved := configurationValue(t, raw)
	current, _ = h.server.Registry.Lookup(key)
	if current.Revision != saved.Revision || current.SourceEpoch != before.SourceEpoch+1 || current.Upstream.Base.String() != "https://replacement.example/codex" {
		t.Fatal("published runtime differs from DB", current, saved)
	}
	if err = h.server.DB.CheckSourceActive(entry.StorageID(), store.SourceFence{AppRevision: entry.Revision, VendorRevision: entry.VendorRevision}); !errors.Is(err, store.ErrSourceInactive) {
		t.Fatal("old source fence accepted", err)
	}
}
