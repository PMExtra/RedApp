package store

import (
	"encoding/json"
	"testing"
)

func TestRetentionOverlayIsAtomicAndDoesNotChangeRuntime(t *testing.T) {
	s := openTest(t)
	if err := s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Application("openai/codex")
	before := a
	cfg, _ := s.ApplicationConfiguration(a.Key)
	if cfg.Fields["retention"].Source != "inherited" {
		t.Fatal(cfg)
	}
	for _, raw := range []string{`null`, `{"enabled":true}`, `{"keep_latest":3}`, `{"enabled":true,"keep_latest":0}`, `{"enabled":true,"keep_latest":1001}`, `{"enabled":true,"keep_latest":1.5}`, `{"enabled":null,"keep_latest":3}`, `{"enabled":true,"keep_latest":3,"other":1}`} {
		_, err := s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: a.Revision, Set: map[string]json.RawMessage{"retention": json.RawMessage(raw)}})
		if err == nil {
			t.Fatal("accepted", raw)
		}
	}
	patch(t, s, a.Key, map[string]any{"retention": map[string]any{"enabled": false, "keep_latest": 3}})
	a, _ = s.Application(a.Key)
	cfg, _ = s.ApplicationConfiguration(a.Key)
	if cfg.Fields["retention"].Source != "custom" || *cfg.Fields["retention"].Differs || a.RuntimeRevision != before.RuntimeRevision || a.SourceEpoch != before.SourceEpoch {
		t.Fatal(a, cfg)
	}
	if _, err := s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: a.Revision, Unset: []string{"retention"}}); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.ApplicationConfiguration(a.Key)
	if cfg.Fields["retention"].Source != "inherited" {
		t.Fatal(cfg)
	}
}
