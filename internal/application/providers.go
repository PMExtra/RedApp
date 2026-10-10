package application

import (
	"errors"
	"github.com/PMExtra/RedApp/presets"

	"github.com/PMExtra/RedApp/internal/distributor"
)

const (
	Info       = "info"
	Hosted     = "hosted"
	HttpCache  = "http-cache"
	Codex      = "codex"
	ClaudeCode = "claude-code"
)

// InfoCapabilities is the shared content layer composed by every provider.
type InfoCapabilities struct {
	Details      bool `json:"details"`
	Instructions bool `json:"instructions"`
}
type Capabilities struct {
	HostedFiles bool `json:"hosted_files"`
	InfoCapabilities
	Files       bool `json:"files"`
	Versions    bool `json:"versions"`
	Installers  bool `json:"installers"`
	TimeCleanup bool `json:"time_cleanup"`
}

// Definition is compiled product metadata, not a runtime plugin or executable
// configuration schema. Provider-specific release code composes the same HTTP
// transport used by HTTP Cache.
type Definition struct {
	Key                    string       `json:"key"`
	Name                   Localized    `json:"name"`
	Description            Localized    `json:"description"`
	DefaultBaseURL         string       `json:"default_base_url"`
	DefaultCacheTTLSeconds int          `json:"default_cache_ttl_seconds"`
	Capabilities           Capabilities `json:"capabilities"`
}

func Definitions() []Definition {
	return []Definition{
		{Key: Info, Name: Localized{"en": "App Info", "zh-CN": "应用介绍"}, Description: Localized{"en": "Application details and instructions, without file hosting or caching.", "zh-CN": "展示应用资料与使用说明，不托管或缓存文件。"}, Capabilities: contentCapabilities(false, false, false, false)},
		{Key: Hosted, Name: Localized{"en": "Hosted Files", "zh-CN": "文件托管"}, Description: Localized{"en": "Upload files or import a URL for permanent storage and downloads.", "zh-CN": "上传文件或从网址导入，持久保存并提供下载。"}, Capabilities: hostedCapabilities()},
		{Key: HttpCache, Name: Localized{"en": "HTTP Cache", "zh-CN": "HTTP 缓存"}, Description: Localized{"en": "Fetch and cache HTTP upstream files on demand.", "zh-CN": "按需获取并缓存 HTTP 上游文件。"}, DefaultCacheTTLSeconds: 300, Capabilities: contentCapabilities(true, false, false, true)},
		{Key: Codex, Name: Localized{"en": "Codex", "zh-CN": "Codex"}, Description: Localized{"en": "Distribute Codex releases and installation resources.", "zh-CN": "分发 Codex 版本及安装资源。"}, DefaultBaseURL: presetDefault(Codex).BaseURL, DefaultCacheTTLSeconds: presetDefault(Codex).CacheTTLSeconds, Capabilities: contentCapabilities(true, true, true, false)},
		{Key: ClaudeCode, Name: Localized{"en": "Claude Code", "zh-CN": "Claude Code"}, Description: Localized{"en": "Distribute Claude Code releases and installation resources.", "zh-CN": "分发 Claude Code 版本及安装资源。"}, DefaultBaseURL: presetDefault(ClaudeCode).BaseURL, DefaultCacheTTLSeconds: presetDefault(ClaudeCode).CacheTTLSeconds, Capabilities: contentCapabilities(true, true, true, false)},
	}
}

func hostedCapabilities() Capabilities {
	c := contentCapabilities(true, false, false, false)
	c.HostedFiles = true
	return c
}
func contentCapabilities(files, versions, installers, cleanup bool) Capabilities {
	return Capabilities{InfoCapabilities: InfoCapabilities{Details: true, Instructions: true}, Files: files, Versions: versions, Installers: installers, TimeCleanup: cleanup}
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
	if provider == Info || provider == Hosted {
		if config.BaseURL != "" || len(config.BaseURLs) != 0 || config.SourceStrategy != "" || config.CacheTTLSeconds != 0 {
			return ProviderConfig{}, errors.New("Content applications do not have upstream or cache settings")
		}
		return ProviderConfig{}, nil
	}
	if provider == HttpCache {
		if config.BaseURLs == nil && config.BaseURL != "" {
			config.BaseURLs = []string{config.BaseURL}
		}
		if len(config.BaseURLs) < 1 || len(config.BaseURLs) > MaxSources {
			return ProviderConfig{}, errors.New("HTTP Cache requires 1..16 base URLs")
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
		return ProviderConfig{}, errors.New("Multiple sources are supported only by HTTP Cache")
	}
	if config.BaseURL == "" {
		config.BaseURL = definition.DefaultBaseURL
	}
	if config.BaseURL == "" {
		return ProviderConfig{}, errors.New("HTTP Cache requires a base URL")
	}
	if config.CacheTTLSeconds < 0 || config.CacheTTLSeconds > 86400 || (definition.Capabilities.Versions && config.CacheTTLSeconds == 0) {
		return ProviderConfig{}, errors.New("Cache TTL is outside the provider limits")
	}
	mode := distributor.ConfiguredRelease
	if provider == HttpCache {
		mode = distributor.GeneralHTTP
	}
	base, err := distributor.NormalizeBase(config.BaseURL, mode)
	if err != nil {
		return ProviderConfig{}, err
	}
	config.BaseURL = base
	return config, nil
}

func presetDefault(provider string) presets.AppSpec {
	key, _ := presets.ReleaseTemplateKey(provider)
	for _, a := range presets.Embedded().Apps {
		if a.Key() == key && a.Spec.Provider == provider {
			return a.Spec
		}
	}
	// Existing effective records supply their explicit source/TTL. A removed
	// display preset must not remove the compiled provider or invent defaults.
	return presets.AppSpec{}
}
