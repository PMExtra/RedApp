package store

import (
	"fmt"
	"strings"

	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/presets"
)

// ProviderKey on ownedConfig is the namespace of every provider-specific leaf.
// Flat API paths remain stable, but are never resolved against another provider.
func providerPath(path string) bool {
	field, _, _ := strings.Cut(path, ".")
	switch field {
	case "json_release", "base_url", "base_urls", "source_strategy", "cache_ttl_seconds", "http_policy", "auto_index", "retention", "prewarm":
		return true
	}
	return false
}

func providerDefaults(provider string) Object {
	out := Object{"provider": provider, "proxy": object(networkproxy.Inherit()), "categories": []any{}, "tags": []any{}, "description": map[string]any{"en": "", "zh-CN": ""}, "instructions": map[string]any{"en": "", "zh-CN": ""}, "icon": ""}
	switch provider {
	case "http-cache":
		out["cache_ttl_seconds"] = 300
		out["source_strategy"] = "ordered"
		out["http_policy"] = object(cachepolicy.Empty())
	case "codex", "claude-code":
		key, _ := presets.ReleaseTemplateKey(provider)
		for _, app := range presets.Embedded().Apps {
			if app.Key() == key && app.Spec.Provider == provider {
				out["base_url"] = app.Spec.BaseURL
				out["cache_ttl_seconds"] = app.Spec.CacheTTLSeconds
			}
		}
	}
	if presets.VersionsProvider(provider) {
		out["retention"] = object(presets.DefaultRetention())
		out["prewarm"] = object(presets.DefaultPrewarm())
	}
	if presets.AutoIndexProvider(provider) {
		out["auto_index"] = false
	}
	return out
}

// templateBase filters before merging: even identically named options belong
// only to the selected provider. The provider choice itself is never inherited.
func templateBase(t *templateSnapshot, provider string) Object {
	out := providerDefaults(provider)
	for key, value := range t.Spec {
		if key == "provider" {
			continue
		}
		if providerPath(key) && (t.Spec["provider"] != provider || !AppPathApplies(provider, key)) {
			continue
		}
		out[key] = cloneValue(value)
	}
	return out
}

func validateProviderOverrides(provider string, over Object) error {
	for key := range over {
		if providerPath(key) && !AppPathApplies(provider, key) {
			return invalidf("%s does not apply to the %s provider", key, provider)
		}
	}
	return nil
}

// Preserve every representable old value, including values equal to defaults.
// Only absent leaves can inherit. Never copy identity/provider into overrides.
func independentOverrides(spec Object, provider string) Object {
	out := Object{}
	for _, path := range paths("App") {
		if !AppPathApplies(provider, path) {
			continue
		}
		if value, ok := leaf(spec, path); ok {
			setLeaf(out, path, cloneValue(value))
		}
	}
	return out
}

func (w *configSet) canReset(kind string, c ownedConfig, path string) (bool, error) {
	if c.Ref == nil {
		return false, nil
	}
	t, err := w.template(kind, *c.Ref)
	if err != nil {
		return false, err
	}
	if t == nil {
		return false, nil
	}
	if kind == "App" && providerPath(path) && (t.Spec["provider"] != c.ProviderKey || !AppPathApplies(c.ProviderKey, path)) {
		return false, nil
	}
	_, exists := leaf(t.Spec, path)
	return exists, nil
}

func (w *configSet) distributionKey(a *appEntry) (string, error) {
	// A mirrored application uses the reviewed contract of its provider.
	if a.config.Ref != nil && !isUpstreamRef(a.config.Ref) {
		t, err := w.template("App", *a.config.Ref)
		if err != nil {
			return "", err
		}
		if t != nil && fmt.Sprint(t.Spec["provider"]) == a.Provider {
			return *a.config.Ref, nil
		}
	}
	key, _ := presets.ReleaseTemplateKey(a.Provider)
	return key, nil
}
