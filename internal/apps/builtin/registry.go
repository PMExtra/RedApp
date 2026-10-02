// Package builtin registers the reviewed protocols and their fixed production
// origins. The manifest is shared with installer maintenance tooling.
package builtin

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"

	"github.com/PMExtra/RedApp/installers"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/claude"
	"github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/distributor"
)

//go:embed manifest.json
var manifest []byte

func New() (*application.Registry, error) {
	var input struct {
		SchemaVersion int                      `json:"schema_version"`
		Applications  []application.Descriptor `json:"applications"`
	}
	d := json.NewDecoder(bytes.NewReader(manifest))
	d.DisallowUnknownFields()
	if err := d.Decode(&input); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF || input.SchemaVersion != 1 || len(input.Applications) == 0 {
		return nil, fmt.Errorf("Invalid application manifest")
	}
	factories := map[string]func(*distributor.Client) application.Protocol{
		"codex-releases-v1":  func(c *distributor.Client) application.Protocol { return codex.NewProtocol(c) },
		"claude-manifest-v1": func(c *distributor.Client) application.Protocol { return claude.NewProtocol(c) },
	}
	var entries []application.Entry
	var shared *distributor.Client
	for _, descriptor := range input.Applications {
		factory, ok := factories[descriptor.Protocol]
		if !ok {
			return nil, fmt.Errorf("Unregistered protocol %s", descriptor.Protocol)
		}
		for _, asset := range descriptor.Installers {
			if _, err := installers.Installer(descriptor.ID, asset.File, "https://validation.invalid/"+descriptor.ID); err != nil {
				return nil, fmt.Errorf("Missing generated installer for %s: %w", descriptor.ID, err)
			}
		}
		for _, asset := range descriptor.Assets {
			if _, err := installers.PublicAsset(descriptor.ID, asset.File); err != nil {
				return nil, fmt.Errorf("Missing public asset for %s: %w", descriptor.ID, err)
			}
		}
		var client *distributor.Client
		var err error
		if shared == nil {
			client, err = distributor.New(descriptor.Upstream)
			shared = client
		} else {
			client, err = shared.Sibling(descriptor.Upstream)
		}
		if err != nil {
			return nil, err
		}
		entry := application.Entry{Descriptor: descriptor, Protocol: factory(client), Upstream: client}
		// Brand resources are reviewed static files; an icon field never fetches a URL.
		if descriptor.Icon != "" {
			assets := map[string]application.Representation{"openai/codex/icon.svg": {ContentType: "image/svg+xml", Body: []byte(codex.OpenAISymbol())}}
			asset, ok := assets[descriptor.ID+"/"+descriptor.Icon]
			if !ok {
				return nil, fmt.Errorf("Unregistered icon for %s", descriptor.ID)
			}
			entry.PublicAssets = map[string]application.Representation{descriptor.Icon: asset}
		}
		entries = append(entries, entry)
	}
	return application.NewRegistry(entries)
}
