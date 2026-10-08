package store

import (
	"encoding/json"
	"errors"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"testing"
)

func TestRuntimeClassifierAndProxyHierarchy(t *testing.T) {
	s := openTest(t)
	if err := s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Vendor("openai")
	a, _ := s.Application("openai/codex")
	initialApp, initialVendor := a.RuntimeRevision, v.RuntimeRevision
	patch(t, s, a.Key, map[string]any{"name.en": "Edited", "instructions.en": "Edited instructions", "proxy": networkproxy.Inherit()})
	a, _ = s.Application(a.Key)
	if a.RuntimeRevision != initialApp {
		t.Fatal("presentation/inherit changed runtime fence")
	}
	for _, global := range []networkproxy.Config{networkproxy.Direct(), {Mode: "url", URL: "http://global.example:3128"}} {
		for _, vendor := range []networkproxy.Config{networkproxy.Inherit(), networkproxy.Direct(), {Mode: "url", URL: "socks5://vendor.example:1080"}} {
			for _, app := range []networkproxy.Config{networkproxy.Inherit(), networkproxy.Direct(), {Mode: "url", URL: "http://app.example:3128"}} {
				childBefore, _ := s.Application(a.Key)
				snapshot, _ := s.DirectoryConfigurationSnapshot()
				if _, err := s.PatchGlobalProxy(snapshot.GlobalProxyRevision, global); err != nil {
					t.Fatal(err)
				}
				v, _ = s.Vendor(v.ID)
				if _, err := s.PatchVendorConfiguration(v.ID, ConfigurationPatch{Revision: v.Revision, Set: map[string]json.RawMessage{"proxy": encode(vendor)}}); err != nil {
					t.Fatal(err)
				}
				childAfter, _ := s.Application(a.Key)
				if childAfter.Revision != childBefore.Revision {
					t.Fatal("parent proxy changed child config revision")
				}
				patch(t, s, a.Key, map[string]any{"proxy": app})
				c, _ := s.ApplicationConfiguration(a.Key)
				want := networkproxy.Resolve(app, a.Key, vendor, v.ID, global)
				if c.ProxyEffective != want {
					t.Fatal("proxy precedence", global, vendor, app, c.ProxyEffective, want)
				}
			}
		}
	}
	a, _ = s.Application(a.Key)
	v, _ = s.Vendor(v.ID)
	if a.RuntimeRevision != initialApp || v.RuntimeRevision != initialVendor || a.SourceEpoch != 1 {
		t.Fatal("proxy hierarchy altered runtime/source revisions")
	}
	epoch, rev := a.SourceEpoch, a.RuntimeRevision
	patch(t, s, a.Key, map[string]any{"cache_ttl_seconds": 17})
	a, _ = s.Application(a.Key)
	if a.RuntimeRevision != rev+1 || a.SourceEpoch != epoch {
		t.Fatal("TTL classification")
	}
	patch(t, s, a.Key, map[string]any{"base_url": "https://mirror.example/codex"})
	a, _ = s.Application(a.Key)
	if a.RuntimeRevision != rev+2 || a.SourceEpoch != epoch+1 {
		t.Fatal("source classification")
	}
	for _, value := range []any{map[string]any{"mode": "inherit", "url": ""}, map[string]any{"mode": "url"}, map[string]any{"mode": "direct", "url": "http://private.example:3128"}, map[string]any{"mode": "url", "url": "http://u:secret@private.example"}} {
		if _, err := s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: a.Revision, Set: map[string]json.RawMessage{"proxy": encode(value)}}); !errors.Is(err, ErrInvalidDirectory) {
			t.Fatal("invalid proxy accepted", err)
		}
	}
	s.SetConfigurationPrepare(func(DirectorySnapshot) (ConfigurationPublication, error) { return nil, errors.New("prepare failed") })
	if err := s.SeedDirectory(nil, nil); err == nil {
		t.Fatal("runtime initialization bypass left open")
	}
	before, _ := s.DirectoryConfigurationSnapshot()
	if _, err := s.PatchGlobalProxy(before.GlobalProxyRevision, networkproxy.Direct()); err == nil {
		t.Fatal("failed prepare accepted")
	}
	after, _ := s.DirectoryConfigurationSnapshot()
	if after.GlobalProxy != before.GlobalProxy || after.GlobalProxyRevision != before.GlobalProxyRevision {
		t.Fatal("global failed prepare partially saved")
	}
}
