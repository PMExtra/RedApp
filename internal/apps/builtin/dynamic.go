package builtin

import (
	"errors"
	"fmt"
	"strings"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
)

func Definitions() []application.Definition { return application.Definitions() }

// NormalizeApplication applies provider BaseURL defaults. The API supplies the
// default TTL only when its input field was omitted, so an explicit GeneralHttp
// TTL of zero remains meaningful (always revalidate).
func NormalizeApplication(input store.ApplicationInput) (store.ApplicationInput, error) {
	config, err := application.NormalizeConfig(input.Provider, application.ProviderConfig{BaseURL: input.BaseURL, BaseURLs: input.BaseURLs, SourceStrategy: input.SourceStrategy, CacheTTLSeconds: input.CacheTTLSeconds})
	if err != nil {
		return store.ApplicationInput{}, err
	}
	input.BaseURL, input.CacheTTLSeconds = config.BaseURL, config.CacheTTLSeconds
	input.BaseURLs, input.SourceStrategy = config.BaseURLs, config.SourceStrategy
	return input, nil
}

// Seeds describes the two defaults; Store.SeedDirectory records its one-time
// execution so restarts never recreate a deleted app or overwrite admin edits.
func Seeds() ([]store.VendorInput, []store.ApplicationSeed, error) {
	descriptors, err := reviewedDescriptors()
	if err != nil {
		return nil, nil, err
	}
	var vendors []store.VendorInput
	var apps []store.ApplicationSeed
	seen := map[string]bool{}
	for _, descriptor := range descriptors {
		key, err := application.ParseKey(descriptor.ID)
		if err != nil {
			return nil, nil, err
		}
		if !seen[key.Vendor] {
			vendors = append(vendors, store.VendorInput{ID: key.Vendor, Name: store.LocalizedText{En: descriptor.Publisher, ZhCN: descriptor.Publisher}, Enabled: true})
			seen[key.Vendor] = true
		}
		provider := application.Codex
		if descriptor.Protocol == "claude-manifest-v1" {
			provider = application.ClaudeCode
		}
		icon := ""
		if descriptor.Icon != "" {
			icon = "/" + descriptor.ID + "/" + descriptor.Icon
		}
		apps = append(apps, store.ApplicationSeed{VendorID: key.Vendor, ApplicationInput: store.ApplicationInput{
			ID: key.App, Name: localizedRecord(descriptor.Name), Description: localizedRecord(descriptor.Summary), Icon: icon,
			Provider: provider, BaseURL: descriptor.Upstream, CacheTTLSeconds: descriptor.DefaultChannelTTLSeconds, Enabled: true,
		}})
	}
	return vendors, apps, nil
}

// NewSourceClient is also used for retired source records during recovery.
// A historical client is never implicitly admitted as an active public app.
func NewSourceClient(provider, baseURL string, pool *distributor.Pool) (*distributor.Client, error) {
	if provider == application.Info || provider == application.Hosted {
		return nil, nil
	}
	if pool == nil {
		return nil, errors.New("A shared upstream transport pool is required")
	}
	definition, ok := application.ProviderDefinition(provider)
	if !ok {
		return nil, errors.New("Unknown provider")
	}
	if baseURL == "" {
		baseURL = definition.DefaultBaseURL
	}
	mode := distributor.ConfiguredRelease
	if provider == application.HttpCache {
		mode = distributor.GeneralHTTP
	}
	normalized, err := distributor.NormalizeBase(baseURL, mode)
	if err != nil {
		return nil, err
	}
	if definition.DefaultBaseURL != "" && normalized == definition.DefaultBaseURL {
		mode = distributor.PublicRelease
	}
	return pool.NewClient(normalized, mode)
}

func NewDynamic(vendors []store.Vendor, apps []store.Application, pool *distributor.Pool) (*application.Registry, error) {
	entries, err := EntriesFromRecords(vendors, apps, pool)
	if err != nil {
		return nil, err
	}
	return application.NewRegistry(entries)
}

// EntriesFromRecords builds before publication: callers can persist a directory
// mutation and atomically Replace the registry while holding admission fencing.
func EntriesFromRecords(vendors []store.Vendor, apps []store.Application, pool *distributor.Pool) ([]application.Entry, error) {
	descriptors, err := reviewedDescriptors()
	if err != nil {
		return nil, err
	}
	templates := map[string]application.Descriptor{}
	for _, d := range descriptors {
		provider := application.Codex
		if d.Protocol == "claude-manifest-v1" {
			provider = application.ClaudeCode
		}
		templates[provider] = d
	}
	byVendor := make(map[string]store.Vendor, len(vendors))
	for _, vendor := range vendors {
		if _, exists := byVendor[vendor.ID]; exists {
			return nil, fmt.Errorf("Duplicate vendor %s", vendor.ID)
		}
		byVendor[vendor.ID] = vendor
	}
	entries := make([]application.Entry, 0, len(apps))
	for _, app := range apps {
		vendor, ok := byVendor[app.VendorID]
		if !ok || !identity.ValidUID(app.UID) || !identity.ValidUID(vendor.UID) || vendor.UID != app.VendorUID || app.Key != app.VendorID+"/"+app.ID {
			return nil, fmt.Errorf("Application vendor binding is invalid for %s", app.Key)
		}
		config, err := application.NormalizeConfig(app.Provider, application.ProviderConfig{BaseURL: app.BaseURL, BaseURLs: app.BaseURLs, SourceStrategy: app.SourceStrategy, CacheTTLSeconds: app.CacheTTLSeconds})
		if err != nil {
			return nil, fmt.Errorf("Invalid provider configuration for %s: %w", app.Key, err)
		}
		client, err := NewSourceClient(app.Provider, config.BaseURL, pool)
		if err != nil {
			return nil, err
		}
		entry := application.Entry{Provider: app.Provider, Upstream: client}
		if app.Provider == application.Info || app.Provider == application.Hosted {
			entry.Descriptor = application.Descriptor{Protocol: app.Provider}
		} else if app.Provider == application.HttpCache {
			entry.Descriptor = application.Descriptor{Protocol: application.HttpCache}
			entry.SourceStrategy = config.SourceStrategy
			entry.Upstreams = make([]*distributor.Client, len(config.BaseURLs))
			for i, base := range config.BaseURLs {
				entry.Upstreams[i], err = NewSourceClient(app.Provider, base, pool)
				if err != nil {
					return nil, err
				}
			}
			entry.Upstream = entry.Upstreams[0]
		} else {
			template, ok := templates[app.Provider]
			if !ok {
				return nil, errors.New("Provider has no reviewed release template")
			}
			entry, err = releaseEntry(template, client)
			if err != nil {
				return nil, err
			}
		}
		entry.UID, entry.Revision, entry.VendorRevision, entry.SourceEpoch = app.UID, app.Revision, vendor.Revision, app.SourceEpoch
		entry.VendorID, entry.VendorName, entry.VendorDescription, entry.VendorIcon = vendor.ID, localized(vendor.Name), localized(vendor.Description), vendor.Icon
		entry.Enabled, entry.DeletedAt = app.Enabled && vendor.Enabled && vendor.DeletedAt == nil, app.DeletedAt
		entry.Descriptor.ID = app.Key
		entry.Descriptor.Name, entry.Descriptor.Summary = localized(app.Name), localized(app.Description)
		entry.Descriptor.Publisher = vendor.Name.En
		entry.Descriptor.Upstream = config.BaseURL
		entry.Descriptor.DefaultChannelTTLSeconds = config.CacheTTLSeconds
		entry.Descriptor.Icon = app.Icon
		if entry.Descriptor.Icon == "" {
			entry.Descriptor.Icon = vendor.Icon
		}
		// Seed icons are addressed absolutely. A custom instance may still expose
		// the reviewed template icon without borrowing another instance's root.
		if strings.HasPrefix(entry.Descriptor.Icon, "/"+entry.TemplateID+"/") {
			entry.Descriptor.Icon = "/" + app.Key + strings.TrimPrefix(entry.Descriptor.Icon, "/"+entry.TemplateID)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func localized(value store.LocalizedText) application.Localized {
	return application.Localized{"en": value.En, "zh-CN": value.ZhCN}
}

func localizedRecord(value application.Localized) store.LocalizedText {
	return store.LocalizedText{En: value["en"], ZhCN: value["zh-CN"]}
}
