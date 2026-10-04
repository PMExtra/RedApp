package cachepolicy

import (
	"errors"
	"testing"

	"github.com/PMExtra/RedApp/internal/pathmatch"
)

func TestPolicyFirstMatchAndImmutableSnapshot(t *testing.T) {
	cfg := Empty()
	cfg.Rules = []CacheRule{{ID: "specific", Match: pathmatch.Spec{Type: "glob", Pattern: "/releases/"}, TTLSeconds: 0}, {ID: "fallback", Match: pathmatch.Spec{Type: "glob", Pattern: "/"}, TTLSeconds: 60}}
	cfg.AutoCleanup = []CleanupRule{{Match: pathmatch.Spec{Type: "glob", Pattern: "/releases/"}, Basis: "last_access", AgeSeconds: 86400}, {Match: pathmatch.Spec{Type: "glob", Pattern: "/"}, Basis: "fetched_at", AgeSeconds: 60}}
	p, err := Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Rules[0].TTLSeconds = 900
	cfg.AutoCleanup[0].AgeSeconds = 60
	cfg.StaleFallback = false
	rule, index, ok := p.CacheRule("/releases/v1/file")
	if !ok || index != 0 || rule.ID != "specific" || rule.TTLSeconds != 0 || !p.StaleFallback() {
		t.Fatal("first match/zero TTL/snapshot changed", rule, index, ok)
	}
	cleanup, index, ok := p.CleanupRule("/releases/v1/file")
	if !ok || index != 0 || cleanup.AgeSeconds != 86400 || cleanup.Basis != "last_access" {
		t.Fatal("cleanup fell through its first path match", cleanup, index, ok)
	}
	rule, index, ok = p.CacheRule("/other/file")
	if !ok || index != 1 || rule.TTLSeconds != 60 {
		t.Fatal(rule, index, ok)
	}
	if _, index, ok = p.CacheRule("not-canonical"); ok || index != -1 {
		t.Fatal("invalid path selected rule")
	}
	empty, err := Compile(Empty())
	if err != nil {
		t.Fatal(err)
	}
	if _, index, ok = empty.CleanupRule("/file"); ok || index != -1 {
		t.Fatal("empty policy fabricated rule")
	}
	disabled := Empty()
	disabled.StaleFallback = false
	normalized, err := Normalize(disabled)
	if err != nil || normalized.StaleFallback {
		t.Fatal("explicit false became default true", normalized, err)
	}
}

func TestPolicySaveTimeBoundsAndIDs(t *testing.T) {
	valid := CacheRule{Match: pathmatch.Spec{Type: "glob", Pattern: "/"}, TTLSeconds: 60}
	invalids := []Config{
		{Rules: make([]CacheRule, MaxRules+1)},
		{AutoCleanup: make([]CleanupRule, MaxRules+1)},
		{Rules: []CacheRule{{Match: valid.Match, TTLSeconds: -1}}},
		{Rules: []CacheRule{{Match: valid.Match, TTLSeconds: MaxTTLSeconds + 1}}},
		{Rules: []CacheRule{{ID: "duplicate", Match: valid.Match}, {ID: "duplicate", Match: valid.Match}}},
		{Rules: []CacheRule{{ID: "line\nbreak", Match: valid.Match}}},
		{AutoCleanup: []CleanupRule{{Match: valid.Match, Basis: "unknown", AgeSeconds: 60}}},
		{AutoCleanup: []CleanupRule{{Match: valid.Match, Basis: "fetched_at", AgeSeconds: MinAgeSeconds - 1}}},
		{AutoCleanup: []CleanupRule{{Match: valid.Match, Basis: "last_access", AgeSeconds: MaxAgeSeconds + 1}}},
	}
	for _, cfg := range invalids {
		if err := cfg.Validate(); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatal("invalid policy accepted", cfg, err)
		}
	}
	for _, basis := range []string{"fetched_at", "last_access"} {
		for _, age := range []int64{MinAgeSeconds, MaxAgeSeconds} {
			cfg := Config{AutoCleanup: []CleanupRule{{Match: valid.Match, Basis: basis, AgeSeconds: age}}}
			if err := cfg.Validate(); err != nil {
				t.Fatal(err)
			}
		}
	}
}
