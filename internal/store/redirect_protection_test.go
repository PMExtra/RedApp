package store

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/releaseconfig"
)

func TestRedirectProtectionHierarchy(t *testing.T) {
	s := openTest(t)
	if err := s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	mode, revision, err := s.GlobalRedirectProtection()
	if err != nil || mode != networkproxy.RedirectStrict || revision != 1 {
		t.Fatal("global default", mode, revision, err)
	}
	if _, err = s.PatchGlobalRedirectProtection(1, networkproxy.RedirectInherit); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatal("inherit accepted globally", err)
	}
	if revision, err = s.PatchGlobalRedirectProtection(1, networkproxy.RedirectBasic); err != nil || revision != 2 {
		t.Fatal(revision, err)
	}
	if _, err = s.PatchGlobalRedirectProtection(1, networkproxy.RedirectOff); !errors.Is(err, ErrConflict) {
		t.Fatal("stale revision accepted", err)
	}
	app, _ := s.Application("openai/codex")
	effective := func(want networkproxy.RedirectEffective) {
		t.Helper()
		c, err := s.ApplicationConfiguration(app.Key)
		if err != nil || c.RedirectEffective != want {
			t.Fatalf("configuration effective %+v, want %+v (%v)", c.RedirectEffective, want, err)
		}
		snapshot, err := s.DirectoryConfigurationSnapshot()
		if err != nil || snapshot.RedirectPolicies[app.UID].RedirectEffective != want {
			t.Fatalf("runtime policy %+v, want %+v (%v)", snapshot.RedirectPolicies[app.UID], want, err)
		}
	}
	// Configurations written before the setting inherit it from the global one.
	c, _ := s.ApplicationConfiguration(app.Key)
	if c.Effective["redirect_protection"] != networkproxy.RedirectInherit || c.Fields["redirect_protection"].Source != "inherited" || *c.Fields["redirect_protection"].Differs {
		t.Fatal("default redirect protection", c.Effective["redirect_protection"], c.Fields["redirect_protection"])
	}
	effective(networkproxy.RedirectEffective{Mode: "basic", SourceScope: "global"})
	v, _ := s.Vendor("openai")
	if _, err = s.PatchVendorConfiguration(v.ID, ConfigurationPatch{Revision: v.Revision, Set: map[string]json.RawMessage{"redirect_protection": encode("off")}}); err != nil {
		t.Fatal(err)
	}
	effective(networkproxy.RedirectEffective{Mode: "off", SourceScope: "vendor", SourceID: "openai"})
	patch(t, s, app.Key, map[string]any{"redirect_protection": "strict"})
	effective(networkproxy.RedirectEffective{Mode: "strict", SourceScope: "app", SourceID: app.Key})
	patch(t, s, app.Key, nil, "redirect_protection")
	effective(networkproxy.RedirectEffective{Mode: "off", SourceScope: "vendor", SourceID: "openai"})
	c, _ = s.ApplicationConfiguration(app.Key)
	for _, set := range []map[string]json.RawMessage{
		{"redirect_protection": encode("loose")},
		{"redirect_allowlist": encode([]releaseconfig.Source{{Origin: "https://cdn.example", Prefix: "/"}})},
	} {
		if _, err = s.PatchApplicationConfiguration(app.Key, ConfigurationPatch{Revision: c.Revision, Set: set}); err == nil {
			t.Fatal("accepted", set)
		}
	}
}

func TestHTTPCacheRedirectAllowlist(t *testing.T) {
	s := openTest(t)
	if _, err := s.CreateVendor(VendorInput{ID: "acme", Name: LocalizedText{"Acme", "Acme"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	app, err := s.CreateApplication("acme", ApplicationInput{ID: "files", Name: LocalizedText{"Files", "Files"}, Provider: "http-cache", BaseURLs: []string{"https://files.example/root"}, CacheTTLSeconds: 300, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.ApplicationConfiguration(app.Key)
	if list, ok := c.Effective["redirect_allowlist"].([]any); !ok || len(list) != 0 {
		t.Fatal("default allowlist", c.Effective["redirect_allowlist"])
	}
	allowlist := []releaseconfig.Source{{Origin: "https://cdn.example", Prefix: "/files/"}}
	patch(t, s, app.Key, map[string]any{"redirect_allowlist": allowlist})
	snapshot, _ := s.DirectoryConfigurationSnapshot()
	policy := snapshot.RedirectPolicies[app.UID]
	if policy.Mode != networkproxy.RedirectStrict || len(policy.Allowlist) != 1 || policy.Allowlist[0] != allowlist[0] {
		t.Fatalf("runtime policy %+v", policy)
	}
	c, _ = s.ApplicationConfiguration(app.Key)
	for _, invalid := range [][]releaseconfig.Source{
		{{Origin: "https://cdn.example/path", Prefix: "/"}},
		{{Origin: "https://cdn.example", Prefix: "files"}},
		{{Origin: "https://cdn.example", Prefix: "/"}, {Origin: "https://cdn.example", Prefix: "/"}},
	} {
		if _, err = s.PatchApplicationConfiguration(app.Key, ConfigurationPatch{Revision: c.Revision, Set: map[string]json.RawMessage{"redirect_allowlist": encode(invalid)}}); err == nil {
			t.Fatal("accepted invalid allowlist", invalid)
		}
	}
}
