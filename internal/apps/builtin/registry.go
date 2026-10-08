// Package builtin composes the compiled providers with persisted applications.
// The reviewed manifest remains shared with installer maintenance tooling.
package builtin

import (
	"fmt"
	"github.com/PMExtra/RedApp/presets"
	"strings"

	"github.com/PMExtra/RedApp/installers"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/claude"
	"github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/distributor"
)

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
	input := presets.Embedded().Descriptors()
	if err := ValidateDescriptors(input); err != nil {
		return nil, err
	}
	return input, nil
}

// ValidateDescriptors checks persisted contracts against compiled protocols and resources.
// The snapshot never supplies executable code or signing keys.
func ValidateDescriptors(input []presets.Descriptor) error {
	for _, descriptor := range input {
		if descriptor.Protocol != "codex-releases-v1" && descriptor.Protocol != "claude-manifest-v1" {
			return fmt.Errorf("Unregistered protocol %s", descriptor.Protocol)
		}
		validator := "codex"
		expectedID := "openai/codex"
		if descriptor.Protocol == "claude-manifest-v1" {
			validator = "claude-code"
			expectedID = "anthropic/claude-code"
		}
		if descriptor.ID != expectedID || descriptor.TrustRevision != 1 || descriptor.InstallerValidator != validator {
			return fmt.Errorf("Unsupported compiled trust contract for %s", descriptor.ID)
		}
		required := map[string]bool{"install.sh": false, "install.ps1": false}
		assets := map[string]bool{"licenses/LICENSE": false, "licenses/NOTICE": false}
		if validator == "claude-code" {
			assets = map[string]bool{"LICENSE.md": false, "claude-code.asc": false}
		}
		for _, asset := range descriptor.Installers {
			if _, ok := required[asset.File]; !ok {
				return fmt.Errorf("Unreviewed installer %s", asset.File)
			}
			required[asset.File] = true
			if _, err := installers.Installer(descriptor.ID, asset.File, "https://validation.invalid/"+descriptor.ID); err != nil {
				return fmt.Errorf("Missing generated installer for %s: %w", descriptor.ID, err)
			}
		}
		for _, asset := range descriptor.Assets {
			if _, ok := assets[asset.File]; !ok {
				return fmt.Errorf("Unreviewed static asset %s", asset.File)
			}
			assets[asset.File] = true
			if _, err := installers.PublicAsset(descriptor.ID, asset.File); err != nil {
				return fmt.Errorf("Missing public asset for %s: %w", descriptor.ID, err)
			}
		}
		for file, present := range required {
			if !present {
				return fmt.Errorf("Missing mandatory installer %s", file)
			}
		}
		for file, present := range assets {
			if !present {
				return fmt.Errorf("Missing mandatory asset %s", file)
			}
		}
	}
	return nil
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
	// Legacy protocol assets have their own reviewed registry. Display icons in
	// presets never name or authorize installer protocol files.
	if asset, ok := BrandAsset("/" + descriptor.ID + "/icon.svg"); ok {
		entry.PublicAssets = map[string]application.Representation{"icon.svg": asset}
	}
	return entry, nil
}

// BrandAsset serves only reviewed, compiled image bytes, independently of a
// persisted application's availability. It never fetches the supplied path.
func BrandAsset(path string) (application.Representation, bool) {
	if strings.HasPrefix(path, presets.ImagePrefix) {
		image, ok := presets.Embedded().Image(path)
		return application.Representation{ContentType: image.ContentType, Body: image.Body}, ok
	}
	image, ok := presets.LegacyImage(path)
	return application.Representation{ContentType: image.ContentType, Body: image.Body}, ok
}
