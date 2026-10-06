// Package builtin composes the compiled providers with persisted applications.
// The reviewed manifest remains shared with installer maintenance tooling.
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

//go:embed assets/anthropic.svg
var claudeIcon []byte

//go:embed assets/anthropic-light.svg
var anthropicIcon []byte

func New() (*application.Registry, error) {
	descriptors, err := reviewedDescriptors()
	if err != nil {
		return nil, err
	}
	var entries []application.Entry
	var shared *distributor.Client
	for _, descriptor := range descriptors {
		var client *distributor.Client
		if shared == nil {
			client, err = distributor.New(descriptor.Upstream)
			shared = client
		} else {
			client, err = shared.Sibling(descriptor.Upstream)
		}
		if err != nil {
			return nil, err
		}
		entry, err := releaseEntry(descriptor, client)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return application.NewRegistry(entries)
}

func reviewedDescriptors() ([]application.Descriptor, error) {
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
	for _, descriptor := range input.Applications {
		if descriptor.Protocol != "codex-releases-v1" && descriptor.Protocol != "claude-manifest-v1" {
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
	}
	return input.Applications, nil
}

func releaseEntry(descriptor application.Descriptor, client *distributor.Client) (application.Entry, error) {
	entry := application.Entry{Descriptor: descriptor, Upstream: client, TemplateID: descriptor.ID}
	switch descriptor.Protocol {
	case "codex-releases-v1":
		entry.Provider = application.Codex
		entry.Protocol = codex.NewProtocol(client)
	case "claude-manifest-v1":
		entry.Provider = application.ClaudeCode
		entry.Protocol = claude.NewProtocol(client)
	default:
		return application.Entry{}, fmt.Errorf("Unregistered protocol %s", descriptor.Protocol)
	}
	// Brand resources are reviewed static files; an icon field never fetches a URL.
	if descriptor.Icon != "" {
		asset, ok := BrandAsset("/" + descriptor.ID + "/" + descriptor.Icon)
		if !ok {
			return application.Entry{}, fmt.Errorf("Unregistered icon for %s", descriptor.ID)
		}
		entry.PublicAssets = map[string]application.Representation{descriptor.Icon: asset}
	}
	return entry, nil
}

// BrandAsset serves only reviewed, compiled image bytes, independently of a
// persisted application's availability. It never fetches the supplied path.
func BrandAsset(path string) (application.Representation, bool) {
	if path == "/openai/codex/icon.svg" || path == "/assets/builtin/openai.svg" {
		return application.Representation{ContentType: "image/svg+xml", Body: []byte(codex.OpenAISymbol())}, true
	}
	switch path {
	case "/anthropic/claude-code/icon.svg":
		return application.Representation{ContentType: "image/svg+xml", Body: claudeIcon}, true
	case "/assets/builtin/anthropic.svg":
		return application.Representation{ContentType: "image/svg+xml", Body: anthropicIcon}, true
	}
	return application.Representation{}, false
}
