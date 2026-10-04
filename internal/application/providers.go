package application

import (
	"errors"

	"github.com/PMExtra/RedApp/internal/distributor"
)

const (
	GeneralHTTP = "general-http"
	Codex       = "codex"
	ClaudeCode  = "claude-code"
)

type Capabilities struct {
	Versions    bool `json:"versions"`
	Installers  bool `json:"installers"`
	TimeCleanup bool `json:"time_cleanup"`
}

// Definition is compiled product metadata, not a runtime plugin or executable
// configuration schema. Provider-specific release code composes the same HTTP
// transport used by GeneralHttp.
type Definition struct {
	Key                    string       `json:"key"`
	Name                   Localized    `json:"name"`
	DefaultBaseURL         string       `json:"default_base_url"`
	DefaultCacheTTLSeconds int          `json:"default_cache_ttl_seconds"`
	Capabilities           Capabilities `json:"capabilities"`
}

func Definitions() []Definition {
	return []Definition{
		{Key: GeneralHTTP, Name: Localized{"en": "General HTTP", "zh-CN": "通用 HTTP"}, DefaultCacheTTLSeconds: 300, Capabilities: Capabilities{TimeCleanup: true}},
		{Key: Codex, Name: Localized{"en": "Codex", "zh-CN": "Codex"}, DefaultBaseURL: "https://releases.openai.com/codex", DefaultCacheTTLSeconds: 60, Capabilities: Capabilities{Versions: true, Installers: true}},
		{Key: ClaudeCode, Name: Localized{"en": "Claude Code", "zh-CN": "Claude Code"}, DefaultBaseURL: "https://downloads.claude.ai/claude-code-releases", DefaultCacheTTLSeconds: 60, Capabilities: Capabilities{Versions: true, Installers: true}},
	}
}

func ProviderDefinition(key string) (Definition, bool) {
	for _, d := range Definitions() {
		if d.Key == key {
			return d, true
		}
	}
	return Definition{}, false
}

type ProviderConfig struct {
	BaseURL         string   `json:"base_url"`
	BaseURLs        []string `json:"base_urls,omitempty"`
	SourceStrategy  string   `json:"source_strategy,omitempty"`
	CacheTTLSeconds int      `json:"cache_ttl_seconds"`
}

const MaxSources = 16

// NormalizeConfig applies only the fixed provider defaults and basic URL/input
// boundaries. New authentication, CA and signing features are deliberately not
// accepted as configuration in this version.
func NormalizeConfig(provider string, config ProviderConfig) (ProviderConfig, error) {
	definition, ok := ProviderDefinition(provider)
	if !ok {
		return ProviderConfig{}, errors.New("Unknown provider")
	}
	if provider == GeneralHTTP {
		if config.BaseURLs == nil && config.BaseURL != "" {
			config.BaseURLs = []string{config.BaseURL}
		}
		if len(config.BaseURLs) < 1 || len(config.BaseURLs) > MaxSources {
			return ProviderConfig{}, errors.New("GeneralHttp requires 1..16 base URLs")
		}
		if config.SourceStrategy == "" {
			config.SourceStrategy = "ordered"
		}
		if config.SourceStrategy != "ordered" && config.SourceStrategy != "round_robin" && config.SourceStrategy != "random" {
			return ProviderConfig{}, errors.New("Invalid source strategy")
		}
		bases := make([]string, len(config.BaseURLs))
		seen := map[string]bool{}
		for i, value := range config.BaseURLs {
			if len(value) > 4096 {
				return ProviderConfig{}, errors.New("Base URL exceeds 4096 bytes")
			}
			base, err := distributor.NormalizeBase(value, distributor.GeneralHTTP)
			if err != nil {
				return ProviderConfig{}, err
			}
			if seen[base] {
				return ProviderConfig{}, errors.New("Duplicate source URL")
			}
			seen[base], bases[i] = true, base
		}
		config.BaseURLs, config.BaseURL = bases, bases[0]
	} else if len(config.BaseURLs) != 0 || config.SourceStrategy != "" {
		return ProviderConfig{}, errors.New("Multiple sources are supported only by GeneralHttp")
	}
	if config.BaseURL == "" {
		config.BaseURL = definition.DefaultBaseURL
	}
	if config.BaseURL == "" {
		return ProviderConfig{}, errors.New("GeneralHttp requires a base URL")
	}
	if config.CacheTTLSeconds < 0 || config.CacheTTLSeconds > 86400 || (definition.Capabilities.Versions && config.CacheTTLSeconds == 0) {
		return ProviderConfig{}, errors.New("Cache TTL is outside the provider limits")
	}
	mode := distributor.ConfiguredRelease
	if provider == GeneralHTTP {
		mode = distributor.GeneralHTTP
	}
	base, err := distributor.NormalizeBase(config.BaseURL, mode)
	if err != nil {
		return ProviderConfig{}, err
	}
	config.BaseURL = base
	return config, nil
}
