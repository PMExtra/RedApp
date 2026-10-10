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

// NewScopedSourceClient builds the upstream client of a current or historical
// application source, bound to its owner's proxy scope.
func NewScopedSourceClient(provider, baseURL, defaultBase, appUID, vendorUID string, pool *distributor.Pool) (*distributor.Client, error) {
	if appUID == "" || vendorUID == "" {
		return nil, errors.New("Application transport scope required")
	}
	return newSourceClient(provider, baseURL, defaultBase, appUID, vendorUID, pool)
}
func newSourceClient(provider, baseURL, defaultBase, appUID, vendorUID string, pool *distributor.Pool) (*distributor.Client, error) {
	if provider == application.Info || provider == application.Hosted {
		return nil, nil
	}
	if pool == nil {
		return nil, errors.New("A shared upstream transport pool is required")
	}
	_, ok := application.ProviderDefinition(provider)
	if !ok {
		return nil, errors.New("Unknown provider")
	}
	if baseURL == "" {
		baseURL = defaultBase
	}
	mode := distributor.ConfiguredRelease
	if provider == application.HttpCache {
		mode = distributor.GeneralHTTP
	}
	normalized, err := distributor.NormalizeBase(baseURL, mode)
	if err != nil {
		return nil, err
	}
	if defaultBase != "" && normalized == defaultBase {
		mode = distributor.PublicRelease
	}
	if appUID != "" {
		return pool.NewScopedClient(normalized, mode, appUID, vendorUID)
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
	return entriesFromConfiguration(store.DirectorySnapshot{Vendors: vendors, Applications: apps, ReviewedDescriptors: descriptors}, pool)
}
func EntriesFromConfiguration(snapshot store.DirectorySnapshot, pool *distributor.Pool) ([]application.Entry, error) {
	if err := ValidateDescriptors(snapshot.ReviewedDescriptors); err != nil {
		return nil, err
	}
	return entriesFromConfiguration(snapshot, pool)
}
func entriesFromConfiguration(snapshot store.DirectorySnapshot, pool *distributor.Pool) ([]application.Entry, error) {
	vendors, apps := snapshot.Vendors, snapshot.Applications
	templates := map[string]application.Descriptor{}
	for _, d := range snapshot.ReviewedDescriptors {
		templates[d.ID] = d
	}
	canonical, err := canonicalTemplates(snapshot.ReviewedDescriptors)
	if err != nil {
		return nil, err
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
		defaultBase := snapshot.ProviderDefaults[app.Provider]
		if defaultBase == "" {
			definition, _ := application.ProviderDefinition(app.Provider)
			defaultBase = definition.DefaultBaseURL
		}
		scopeUID, scopeVendor := "", ""
		if snapshot.ProxyScopes != nil {
			scopeUID, scopeVendor = app.UID, app.VendorUID
		}
		client, err := newSourceClient(app.Provider, config.BaseURL, defaultBase, scopeUID, scopeVendor, pool)
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
				entry.Upstreams[i], err = newSourceClient(app.Provider, base, "", scopeUID, scopeVendor, pool)
				if err != nil {
					return nil, err
				}
			}
			entry.Upstream = entry.Upstreams[0]
		} else {
			// An independent instance has no template reference and uses the
			// provider's single reviewed contract.
			key := snapshot.TemplateBindings[app.UID]
			if key == "" {
				key = canonical[app.Provider]
			}
			template, ok := templates[key]
			if !ok {
				return nil, errors.New("Provider has no reviewed release template")
			}
			entry, err = releaseEntry(template, client)
			if err != nil {
				return nil, err
			}
		}
		entry.VendorUID = vendor.UID
		entry.RuntimeRevision, entry.VendorRuntimeRevision = app.RuntimeRevision, vendor.RuntimeRevision
		entry.UID, entry.Revision, entry.VendorRevision, entry.SourceEpoch = app.UID, app.Revision, vendor.Revision, app.SourceEpoch
		entry.VendorLocalizedIcons = localized(vendor.LocalizedIcons)
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

// releaseProtocols maps each release provider to its compiled protocol.
var releaseProtocols = map[string]string{
	application.Codex:      "codex-releases-v1",
	application.ClaudeCode: "claude-manifest-v1",
}

func providerForProtocol(protocol string) (string, bool) {
	for provider, p := range releaseProtocols {
		if p == protocol {
			return provider, true
		}
	}
	return "", false
}

// canonicalTemplates maps each release provider to the ID of its reviewed
// template. Each provider has at most one, so the choice is never ambiguous.
func canonicalTemplates(descriptors []application.Descriptor) (map[string]string, error) {
	out := map[string]string{}
	for _, d := range descriptors {
		provider, ok := providerForProtocol(d.Protocol)
		if !ok {
			return nil, fmt.Errorf("unregistered protocol %s", d.Protocol)
		}
		if existing, dup := out[provider]; dup {
			return nil, fmt.Errorf("provider %s has two reviewed templates: %s and %s", provider, existing, d.ID)
		}
		out[provider] = d.ID
	}
	return out, nil
}

func localized(value store.LocalizedText) application.Localized {
	return application.Localized{"en": value.En, "zh-CN": value.ZhCN}
}

func localizedRecord(value application.Localized) store.LocalizedText {
	return store.LocalizedText{En: value["en"], ZhCN: value["zh-CN"]}
}
