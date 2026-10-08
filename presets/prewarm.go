package presets

import (
	"encoding/json"
	"fmt"
)

type Prewarm struct {
	Enabled   bool     `json:"enabled"`
	Channels  []string `json:"channels"`
	Platforms []string `json:"platforms"`
}

func DefaultPrewarm() *Prewarm { return &Prewarm{Channels: []string{}, Platforms: []string{}} }
func (p Prewarm) Validate(provider string) error {
	if !VersionsProvider(provider) {
		return fmt.Errorf("prewarm requires release provider")
	}
	channels := map[string]bool{"latest": true}
	platforms := map[string]bool{"darwin-arm64": true, "darwin-x64": true, "linux-arm64": true, "linux-x64": true, "win32-arm64": true, "win32-x64": true}
	if provider == "claude-code" {
		channels["stable"] = true
		platforms["linux-arm64-musl"] = true
		platforms["linux-x64-musl"] = true
	}
	for _, values := range []struct {
		items   []string
		allowed map[string]bool
	}{{p.Channels, channels}, {p.Platforms, platforms}} {
		seen := map[string]bool{}
		for _, id := range values.items {
			if !values.allowed[id] || seen[id] {
				return fmt.Errorf("invalid prewarm selection")
			}
			seen[id] = true
		}
	}
	if p.Enabled && (len(p.Channels) == 0 || len(p.Platforms) == 0) {
		return fmt.Errorf("enabled prewarm requires channels and platforms")
	}
	return nil
}
func (p *Prewarm) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if len(fields) != 3 {
		return fmt.Errorf("prewarm requires enabled, channels and platforms")
	}
	var value struct {
		Enabled   *bool     `json:"enabled"`
		Channels  *[]string `json:"channels"`
		Platforms *[]string `json:"platforms"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if value.Enabled == nil || value.Channels == nil || value.Platforms == nil {
		return fmt.Errorf("prewarm fields cannot be missing or null")
	}
	*p = Prewarm{*value.Enabled, *value.Channels, *value.Platforms}
	return nil
}
