package httpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/store"
)

// Public response documents (components.schemas). Handlers build these
// explicitly; internal structs are never encoded directly.

type localizedText struct {
	En   string `json:"en"`
	ZhCN string `json:"zh-CN"`
}

func fromLocalized(m application.Localized) localizedText {
	return localizedText{En: m["en"], ZhCN: m["zh-CN"]}
}
func fromStoreText(t store.LocalizedText) localizedText { return localizedText{En: t.En, ZhCN: t.ZhCN} }

type localizedFlags struct {
	En   bool `json:"en"`
	ZhCN bool `json:"zh-CN"`
}

type publicVendorDTO struct {
	ID             string        `json:"id"`
	Name           localizedText `json:"name"`
	Description    localizedText `json:"description"`
	Icon           string        `json:"icon"`
	LocalizedIcons localizedText `json:"localized_icons"`
}

func publicVendorFromStore(v store.Vendor) publicVendorDTO {
	icons := fromStoreText(v.LocalizedIcons)
	return publicVendorDTO{ID: v.ID, Name: fromStoreText(v.Name), Description: fromStoreText(v.Description), Icon: publicIcon(v.Icon), LocalizedIcons: localizedText{En: publicIcon(icons.En), ZhCN: publicIcon(icons.ZhCN)}}
}

type categoryLabelDTO struct {
	ID   string        `json:"id"`
	Name localizedText `json:"name"`
}
type categoryCountDTO struct {
	ID    string        `json:"id"`
	Name  localizedText `json:"name"`
	Count int64         `json:"count"`
}

type capabilitiesDTO struct {
	Details      bool `json:"details"`
	Instructions bool `json:"instructions"`
	HostedFiles  bool `json:"hosted_files"`
	Files        bool `json:"files"`
	Versions     bool `json:"versions"`
	Installers   bool `json:"installers"`
	TimeCleanup  bool `json:"time_cleanup"`
}

func capabilitiesFor(provider string) capabilitiesDTO {
	definition, _ := application.ProviderDefinition(provider)
	c := definition.Capabilities
	return capabilitiesDTO{Details: c.Details, Instructions: c.Instructions, HostedFiles: c.HostedFiles, Files: c.Files, Versions: c.Versions, Installers: c.Installers, TimeCleanup: c.TimeCleanup}
}

type latestVersionDTO struct {
	Version   string     `json:"version"`
	FirstSeen *time.Time `json:"first_seen"`
}

type publicAppDTO struct {
	Key                   string             `json:"key"`
	ID                    string             `json:"id"`
	Vendor                publicVendorDTO    `json:"vendor"`
	Name                  localizedText      `json:"name"`
	Description           localizedText      `json:"description"`
	Icon                  string             `json:"icon"`
	Provider              string             `json:"provider"`
	Capabilities          capabilitiesDTO    `json:"capabilities"`
	Categories            []categoryLabelDTO `json:"categories"`
	LatestKnownVersion    *latestVersionDTO  `json:"latest_known_version"`
	InstructionsAvailable localizedFlags     `json:"instructions_available"`
	Revision              string             `json:"revision"`
}

// iconPattern is components.schemas.IconPath without the empty alternative.
var iconPattern = regexp.MustCompile(`^(/assets/icons/[0-9a-f]{64}\.(png|jpg|svg)|/assets/presets/[a-z0-9][a-z0-9/._-]*\.(svg|png|jpg))$`)

// publicIcon returns a stored icon path when it is a servable site-absolute
// asset path and "" otherwise.
func publicIcon(path string) string {
	if iconPattern.MatchString(path) {
		return path
	}
	return ""
}

// publicApp builds the PublicApp document of a published application. It
// reads the instructions and the locally known versions; it never contacts
// an upstream.
func (s *Server) publicApp(e application.Entry, publicURL string, categories []store.TaxonomyLabel) (publicAppDTO, error) {
	instructions, err := s.store.Instructions(e.UID)
	if err != nil {
		return publicAppDTO{}, err
	}
	latest, firstSeen, err := s.latestKnownVersion(e)
	if err != nil {
		return publicAppDTO{}, err
	}
	key, _ := application.ParseKey(e.Descriptor.ID)
	labels := make([]categoryLabelDTO, 0, len(categories))
	for _, c := range categories {
		labels = append(labels, categoryLabelDTO{ID: c.ID, Name: fromStoreText(c.Name)})
	}
	icons := fromLocalized(e.VendorLocalizedIcons)
	out := publicAppDTO{
		Key: e.Descriptor.ID,
		ID:  key.App,
		Vendor: publicVendorDTO{ID: e.VendorID, Name: fromLocalized(e.VendorName), Description: fromLocalized(e.VendorDescription),
			Icon: publicIcon(e.VendorIcon), LocalizedIcons: localizedText{En: publicIcon(icons.En), ZhCN: publicIcon(icons.ZhCN)}},
		Name:                  fromLocalized(e.Descriptor.Name),
		Description:           fromLocalized(e.Descriptor.Summary),
		Icon:                  publicIcon(e.Descriptor.Icon),
		Provider:              e.Provider,
		Capabilities:          capabilitiesFor(e.Provider),
		Categories:            labels,
		InstructionsAvailable: localizedFlags{En: instructions.En != "", ZhCN: instructions.ZhCN != ""},
	}
	if latest != "" {
		out.LatestKnownVersion = &latestVersionDTO{Version: latest, FirstSeen: firstSeen}
	}
	// The revision changes whenever the rendered instructions document could:
	// instructions, names and other configuration (entity revisions), the
	// latest version or the public URL.
	identity, _ := json.Marshal([]any{e.UID, e.Revision, e.VendorRevision, instructions.Revision, latest, publicURL})
	digest := sha256.Sum256(identity)
	out.Revision = hex.EncodeToString(digest[:16])
	return out, nil
}

// latestKnownVersion reads only locally observed versions of the current
// source epoch; it never synchronizes with the upstream.
func (s *Server) latestKnownVersion(e application.Entry) (string, *time.Time, error) {
	if e.Protocol == nil {
		return "", nil, nil
	}
	versions, err := s.store.VersionsFor(e.StorageID())
	if err != nil {
		return "", nil, err
	}
	var latest string
	for version := range versions {
		if _, err := e.Protocol.ValidateVersion(version); err != nil {
			continue
		}
		if latest == "" {
			latest = version
			continue
		}
		if order, err := e.Protocol.CompareVersions(version, latest); err == nil && (order > 0 || order == 0 && version < latest) {
			latest = version
		}
	}
	var discovered *time.Time
	if at, err := time.Parse(time.RFC3339, versions[latest]); latest != "" && err == nil && at.Unix() > 0 {
		at = at.UTC()
		discovered = &at
	}
	return latest, discovered, nil
}

type hostedFileDTO struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type pageDTO[T any] struct {
	Items      []T   `json:"items"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

func hostedFilePage(p store.Page[store.HostedFile]) pageDTO[hostedFileDTO] {
	out := pageDTO[hostedFileDTO]{Items: make([]hostedFileDTO, 0, len(p.Items)), Page: p.Page, Limit: p.Limit, Total: p.Total, TotalPages: p.TotalPages}
	for _, f := range p.Items {
		out.Items = append(out.Items, hostedFileDTO{ID: f.ID, Path: f.Path, SHA256: f.SHA256, SizeBytes: f.SizeBytes, CreatedAt: f.CreatedAt.UTC()})
	}
	return out
}
