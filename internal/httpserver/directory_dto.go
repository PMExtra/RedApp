package httpserver

import (
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/store"
)

// Administrator directory documents (components.schemas Provider, Vendor,
// App and their list items), built explicitly from store records. Internal
// runtime revisions and storage namespaces are never exposed.

type providerDTO struct {
	Key                    string          `json:"key"`
	Name                   localizedText   `json:"name"`
	Description            localizedText   `json:"description"`
	DefaultBaseURL         *string         `json:"default_base_url"`
	DefaultCacheTTLSeconds *int            `json:"default_cache_ttl_seconds"`
	Capabilities           capabilitiesDTO `json:"capabilities"`
}

type providerListDTO struct {
	Items []providerDTO `json:"items"`
}

func providerList() providerListDTO {
	out := providerListDTO{Items: []providerDTO{}}
	for _, d := range application.Definitions() {
		p := providerDTO{Key: d.Key, Name: fromLocalized(d.Name), Description: fromLocalized(d.Description), Capabilities: capabilitiesFor(d.Key)}
		if d.DefaultBaseURL != "" {
			base := d.DefaultBaseURL
			p.DefaultBaseURL = &base
		}
		if store.AppPathApplies(d.Key, "cache_ttl_seconds") {
			ttl := d.DefaultCacheTTLSeconds
			p.DefaultCacheTTLSeconds = &ttl
		}
		out.Items = append(out.Items, p)
	}
	return out
}

type vendorDTO struct {
	UID            string        `json:"uid"`
	ID             string        `json:"id"`
	Name           localizedText `json:"name"`
	Description    localizedText `json:"description"`
	Icon           string        `json:"icon"`
	LocalizedIcons localizedText `json:"localized_icons"`
	Enabled        bool          `json:"enabled"`
	HasTemplate    bool          `json:"has_template"`
	Revision       int64         `json:"revision"`
	DeletedAt      *time.Time    `json:"deleted_at"`
}

func vendorDocument(v store.Vendor) vendorDTO {
	return vendorDTO{UID: v.UID, ID: v.ID, Name: fromStoreText(v.Name), Description: fromStoreText(v.Description), Icon: publicIcon(v.Icon),
		LocalizedIcons: localizedText{En: publicIcon(v.LocalizedIcons.En), ZhCN: publicIcon(v.LocalizedIcons.ZhCN)},
		Enabled:        v.Enabled, HasTemplate: v.HasTemplate, Revision: v.Revision, DeletedAt: utcTime(v.DeletedAt)}
}

// appDTO is the App schema. Upstream fields are present only for the
// providers they apply to (store.AppPathApplies).
type appDTO struct {
	UID             string        `json:"uid"`
	ID              string        `json:"id"`
	Key             string        `json:"key"`
	VendorUID       string        `json:"vendor_uid"`
	VendorID        string        `json:"vendor_id"`
	Name            localizedText `json:"name"`
	Description     localizedText `json:"description"`
	Icon            string        `json:"icon"`
	Provider        string        `json:"provider"`
	BaseURL         *string       `json:"base_url,omitempty"`
	BaseURLs        []string      `json:"base_urls,omitempty"`
	SourceStrategy  string        `json:"source_strategy,omitempty"`
	CacheTTLSeconds *int          `json:"cache_ttl_seconds,omitempty"`
	Categories      []string      `json:"categories"`
	Tags            []string      `json:"tags"`
	Enabled         bool          `json:"enabled"`
	BuiltinTemplate bool          `json:"builtin_template"`
	SourceEpoch     int64         `json:"source_epoch"`
	Revision        int64         `json:"revision"`
	DeletedAt       *time.Time    `json:"deleted_at"`
}

func appDocument(a store.Application) appDTO {
	out := appDTO{UID: a.UID, ID: a.ID, Key: a.Key, VendorUID: a.VendorUID, VendorID: a.VendorID, Name: fromStoreText(a.Name), Description: fromStoreText(a.Description),
		Icon: publicIcon(a.Icon), Provider: a.Provider, Categories: nonNil(a.Categories), Tags: nonNil(a.Tags), Enabled: a.Enabled,
		BuiltinTemplate: a.BuiltinTemplate, SourceEpoch: a.SourceEpoch, Revision: a.Revision, DeletedAt: utcTime(a.DeletedAt)}
	if !store.AppPathApplies(a.Provider, "cache_ttl_seconds") {
		out.SourceEpoch = 0
	}
	if store.AppPathApplies(a.Provider, "base_url") {
		base := a.BaseURL
		out.BaseURL = &base
	}
	if store.AppPathApplies(a.Provider, "base_urls") {
		out.BaseURLs, out.SourceStrategy = nonNil(a.BaseURLs), a.SourceStrategy
	}
	if store.AppPathApplies(a.Provider, "cache_ttl_seconds") {
		ttl := a.CacheTTLSeconds
		out.CacheTTLSeconds = &ttl
	}
	return out
}

type appListItemDTO struct {
	appDTO
	LatestVersion       *string    `json:"latest_version"`
	VersionDiscoveredAt *time.Time `json:"version_discovered_at"`
	SuccessfulDownloads *int64     `json:"successful_downloads"`
}

type vendorListItemDTO struct {
	vendorDTO
	Apps     []appDTO `json:"apps"`
	AppTotal int64    `json:"app_total"`
}

type appDeletionDTO struct {
	CleanupPending bool `json:"cleanup_pending"`
}

type storedIconDTO struct {
	Icon string `json:"icon"`
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}
