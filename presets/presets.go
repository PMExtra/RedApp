// Package presets owns the reviewed, embedded entity defaults and distribution inventory.
package presets

import (
	"embed"
	"encoding/json"
	"fmt"
	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/media"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/yamlconfig"
	"io"
	"io/fs"
	"net/url"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
)

//go:embed *.yaml */*.yaml assets
var files embed.FS

type Text struct {
	En   string `json:"en"`
	ZhCN string `json:"zh-CN"`
}

func (t Text) Localized() Localized { return Localized{"en": t.En, "zh-CN": t.ZhCN} }

type VendorMetadata struct {
	ID string `json:"id"`
}

type Metadata struct {
	ID     string `json:"id"`
	Vendor string `json:"vendor,omitempty"`
}
type VendorSpec struct {
	Proxy          networkproxy.Config `json:"proxy"`
	Name           Text                `json:"name"`
	Description    Text                `json:"description"`
	Icon           string              `json:"icon"`
	LocalizedIcons Text                `json:"localized_icons"`
}
type Retention struct {
	Enabled    bool `json:"enabled"`
	KeepLatest int  `json:"keep_latest"`
}

func (r Retention) Validate() error {
	if r.KeepLatest < 1 || r.KeepLatest > 1000 {
		return fmt.Errorf("keep_latest must be 1..1000")
	}
	return nil
}
func VersionsProvider(provider string) bool { return provider == "codex" || provider == "claude-code" }
func DefaultRetention() *Retention          { return &Retention{KeepLatest: 3} }

type AppSpec struct {
	Categories      []string            `json:"categories"`
	Tags            []string            `json:"tags"`
	Prewarm         *Prewarm            `json:"prewarm,omitempty"`
	Retention       *Retention          `json:"retention,omitempty"`
	Proxy           networkproxy.Config `json:"proxy"`
	Name            Text                `json:"name"`
	Description     Text                `json:"description"`
	Icon            string              `json:"icon"`
	Provider        string              `json:"provider"`
	BaseURL         string              `json:"base_url"`
	BaseURLs        []string            `json:"base_urls"`
	SourceStrategy  string              `json:"source_strategy"`
	CacheTTLSeconds int                 `json:"cache_ttl_seconds"`
	Instructions    Text                `json:"instructions"`
	HTTPPolicy      *cachepolicy.Config `json:"http_policy,omitempty"`
}
type Distribution struct {
	Protocol           string      `json:"protocol"`
	Channels           []string    `json:"channels"`
	TrustRevision      int64       `json:"trust_revision"`
	InstallerValidator string      `json:"installer_validator"`
	Installers         []Installer `json:"installers"`
	Assets             []Asset     `json:"assets"`
	UpdatePolicy       Text        `json:"update_policy"`
}
type Vendor struct {
	SchemaVersion int            `json:"schema_version"`
	Kind          string         `json:"kind"`
	Metadata      VendorMetadata `json:"metadata"`
	Spec          VendorSpec     `json:"spec"`
}
type App struct {
	SchemaVersion int           `json:"schema_version"`
	Kind          string        `json:"kind"`
	Metadata      Metadata      `json:"metadata"`
	Spec          AppSpec       `json:"spec"`
	Distribution  *Distribution `json:"distribution,omitempty"`
}
type Set struct {
	Taxonomy TaxonomySpec
	Vendors  []Vendor
	Apps     []App
	images   map[string]Image
}

type Image struct {
	ContentType string
	Body        []byte
}

const ImagePrefix = "/assets/presets/"

// Legacy image routes are a separate reviewed protocol/compatibility registry.
// New display resources do not require entries here.
var legacyImages = map[string]string{
	"/openai/codex/icon.svg":          "assets/openai/codex/icon.svg",
	"/anthropic/claude-code/icon.svg": "assets/anthropic/claude-code/icon.svg",
	"/assets/builtin/openai.svg":      "assets/builtin/openai.svg",
	"/assets/builtin/anthropic.svg":   "assets/builtin/anthropic.svg",
}

func LegacyImage(publicPath string) (Image, bool) {
	relative, ok := legacyImages[publicPath]
	if !ok {
		return Image{}, false
	}
	return Embedded().Image(ImagePrefix + strings.TrimPrefix(relative, "assets/"))
}

func (a App) Key() string { return a.Metadata.Vendor + "/" + a.Metadata.ID }

// Icon resolves only reviewed package resources, never a network URL or executable.
func Icon(relative string) (string, error) {
	return Embedded().Icon(relative)
}

func (s Set) Icon(relative string) (string, error) {
	if relative == "" {
		return "", nil
	}
	if !fs.ValidPath(relative) || !strings.HasPrefix(relative, "assets/") || strings.ContainsAny(relative, "\\:?#") {
		return "", fmt.Errorf("icon must reference a relative assets/ image")
	}
	if _, ok := s.images[relative]; !ok {
		return "", fmt.Errorf("missing reviewed image %q", relative)
	}
	return ImagePrefix + strings.TrimPrefix(relative, "assets/"), nil
}

// Image returns detached bytes from the validated embedded resource map only.
func (s Set) Image(publicPath string) (Image, bool) {
	if !strings.HasPrefix(publicPath, ImagePrefix) {
		return Image{}, false
	}
	image, ok := s.images["assets/"+strings.TrimPrefix(publicPath, ImagePrefix)]
	image.Body = slices.Clone(image.Body)
	return image, ok
}
func presentation(name, desc Text, icon string) error {
	for _, n := range []string{name.En, name.ZhCN} {
		if strings.TrimSpace(n) == "" || len(n) > 256 || strings.ContainsRune(n, 0) {
			return fmt.Errorf("both names are required, at most 256 bytes, without NUL")
		}
	}
	for _, v := range []string{desc.En, desc.ZhCN} {
		if len(v) > 16384 || strings.ContainsRune(v, 0) {
			return fmt.Errorf("invalid description")
		}
	}
	return nil
}
func source(s AppSpec) error {
	switch s.Provider {
	case "info", "hosted":
		if s.BaseURL != "" || len(s.BaseURLs) > 0 || s.SourceStrategy != "" || s.CacheTTLSeconds != 0 {
			return fmt.Errorf("content provider cannot configure source/cache")
		}
		return nil
	case "http-cache", "codex", "claude-code":
	default:
		return fmt.Errorf("invalid provider %q", s.Provider)
	}
	if s.CacheTTLSeconds < 0 || s.CacheTTLSeconds > 86400 || (s.Provider != "http-cache" && s.CacheTTLSeconds == 0) {
		return fmt.Errorf("invalid cache_ttl_seconds")
	}
	bases := []string{s.BaseURL}
	if s.Provider == "http-cache" {
		bases = s.BaseURLs
		if len(bases) == 0 && s.BaseURL != "" {
			bases = []string{s.BaseURL}
		}
		if len(bases) < 1 || len(bases) > 16 {
			return fmt.Errorf("HTTP Cache requires 1..16 sources")
		}
		if s.SourceStrategy != "" && s.SourceStrategy != "ordered" && s.SourceStrategy != "round_robin" && s.SourceStrategy != "random" {
			return fmt.Errorf("invalid source_strategy")
		}
	} else if len(s.BaseURLs) > 0 || s.SourceStrategy != "" {
		return fmt.Errorf("multiple sources require HTTP Cache")
	}
	seen := map[string]bool{}
	for _, base := range bases {
		u, err := url.Parse(base)
		if err != nil || len(base) > 4096 || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") || strings.ContainsAny(base, "\\\r\n\t ") {
			return fmt.Errorf("invalid base_url")
		}
		if seen[base] {
			return fmt.Errorf("duplicate source")
		}
		seen[base] = true
	}
	return nil
}
func distribution(a App) error {
	d := a.Distribution
	release := a.Spec.Provider == "codex" || a.Spec.Provider == "claude-code"
	if !release {
		if d != nil {
			return fmt.Errorf("distribution requires reviewed release provider")
		}
		return nil
	}
	if d == nil {
		return fmt.Errorf("missing distribution")
	}
	protocol, validator := "codex-releases-v1", "codex"
	if a.Spec.Provider == "claude-code" {
		protocol, validator = "claude-manifest-v1", "claude-code"
	}
	if d.Protocol != protocol || d.InstallerValidator != validator || d.TrustRevision < 1 || len(d.Channels) == 0 || len(d.Installers) == 0 {
		return fmt.Errorf("invalid reviewed distribution protocol/validator/trust/channels/installers")
	}
	seen := map[string]bool{}
	for _, c := range d.Channels {
		if !identity.ValidSlug(c) || seen[c] {
			return fmt.Errorf("invalid duplicate channel")
		}
		seen[c] = true
	}
	seen = map[string]bool{}
	for _, i := range d.Installers {
		u, e := url.Parse(i.Source)
		shell := i.Shell == "sh" || i.Shell == "bash"
		if i.File == "install.ps1" {
			shell = i.Shell == "powershell"
		}
		if (i.File != "install.sh" && i.File != "install.ps1") || seen[i.File] || !shell || e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return fmt.Errorf("invalid reviewed installer")
		}
		seen[i.File] = true
	}
	for _, asset := range d.Assets {
		if !fs.ValidPath(asset.File) || (asset.Kind != "license" && asset.Kind != "notice" && asset.Kind != "public_key") {
			return fmt.Errorf("invalid reviewed asset")
		}
	}
	return nil
}

// LoadFS is for trusted repository tooling/tests. The running service uses Embedded only.
func LoadFS(input fs.FS) (Set, error) {
	out := Set{images: map[string]Image{}}
	if err := fs.WalkDir(input, "assets", func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !fs.ValidPath(file) || strings.ContainsAny(file, "\\:?#") {
			return fmt.Errorf("%s: invalid relative image path", file)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s: image must be a regular embedded file", file)
		}
		reader, err := input.Open(file)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(io.LimitReader(reader, media.MaxBytes+1))
		reader.Close()
		if err != nil {
			return err
		}
		contentType, err := media.ValidateStaticImage(body, path.Ext(file))
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		out.images[file] = Image{ContentType: contentType, Body: body}
		return nil
	}); err != nil {
		return Set{}, err
	}
	vendors := map[string]bool{}
	apps := map[string]bool{}
	err := fs.WalkDir(input, ".", func(file string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() || strings.HasPrefix(file, "assets/") || !strings.HasSuffix(file, ".yaml") {
			return nil
		}
		reader, e := input.Open(file)
		if e != nil {
			return e
		}
		raw, e := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
		reader.Close()
		if e != nil {
			return e
		}
		fail := func(e error) error { return yamlconfig.ErrorAt(raw, file, "spec", e) }
		if file == "_taxonomy.yaml" {
			var taxonomy Taxonomy
			if e = yamlconfig.Decode(raw, file, 1<<20, &taxonomy); e != nil {
				return e
			}
			if taxonomy.SchemaVersion != 1 || taxonomy.Kind != "Taxonomy" {
				return fail(fmt.Errorf("expected schema 1 Taxonomy"))
			}
			if e = taxonomy.Spec.Validate(); e != nil {
				return fail(e)
			}
			out.Taxonomy = taxonomy.Spec
			return nil
		}
		if strings.Count(file, "/") == 0 {
			var v Vendor
			if e = yamlconfig.Decode(raw, file, 1<<20, &v); e != nil {
				return e
			}
			if v.Spec.Proxy.Mode == "" {
				v.Spec.Proxy = networkproxy.Inherit()
			}
			if e = v.Spec.Proxy.Validate(true); e != nil {
				return fail(e)
			}
			if v.SchemaVersion != 1 {
				return yamlconfig.ErrorAt(raw, file, "schema_version", fmt.Errorf("expected 1"))
			}
			if v.Kind != "Vendor" {
				return yamlconfig.ErrorAt(raw, file, "kind", fmt.Errorf("expected Vendor for this path"))
			}
			if !identity.ValidVendor(v.Metadata.ID) || file != v.Metadata.ID+".yaml" || vendors[v.Metadata.ID] {
				return yamlconfig.ErrorAt(raw, file, "metadata", fmt.Errorf("kind/metadata/path or duplicate identity is invalid"))
			}
			if e = presentation(v.Spec.Name, v.Spec.Description, v.Spec.Icon); e != nil {
				return fail(e)
			}
			for _, icon := range []string{v.Spec.Icon, v.Spec.LocalizedIcons.En, v.Spec.LocalizedIcons.ZhCN} {
				if _, e = out.Icon(icon); e != nil {
					return fail(e)
				}
			}
			vendors[v.Metadata.ID] = true
			out.Vendors = append(out.Vendors, v)
		} else {
			var a App
			if e = yamlconfig.Decode(raw, file, 1<<20, &a); e != nil {
				return e
			}
			if VersionsProvider(a.Spec.Provider) && a.Spec.Prewarm == nil {
				a.Spec.Prewarm = DefaultPrewarm()
			}
			if a.Spec.Prewarm != nil {
				if e = a.Spec.Prewarm.Validate(a.Spec.Provider); e != nil {
					return fail(e)
				}
			}
			if VersionsProvider(a.Spec.Provider) && a.Spec.Retention == nil {
				a.Spec.Retention = DefaultRetention()
			}
			if a.Spec.Retention != nil {
				if !VersionsProvider(a.Spec.Provider) {
					return fail(fmt.Errorf("retention requires versions provider"))
				}
				if e = a.Spec.Retention.Validate(); e != nil {
					return fail(e)
				}
			}
			if a.Spec.Proxy.Mode == "" {
				a.Spec.Proxy = networkproxy.Inherit()
			}
			if e = a.Spec.Proxy.Validate(true); e != nil {
				return fail(e)
			}
			if a.SchemaVersion != 1 {
				return yamlconfig.ErrorAt(raw, file, "schema_version", fmt.Errorf("expected 1"))
			}
			if a.Kind != "App" {
				return yamlconfig.ErrorAt(raw, file, "kind", fmt.Errorf("expected App for this path"))
			}
			if !identity.ValidVendor(a.Metadata.Vendor) || !identity.ValidSlug(a.Metadata.ID) || file != a.Key()+".yaml" || apps[a.Key()] {
				return yamlconfig.ErrorAt(raw, file, "metadata", fmt.Errorf("kind/metadata/path or duplicate identity is invalid"))
			}
			if a.Spec.Categories, e = NormalizeCategories(a.Spec.Categories); e != nil {
				return fail(e)
			}
			if a.Spec.Tags, e = NormalizeTags(a.Spec.Tags); e != nil {
				return fail(e)
			}
			if e = presentation(a.Spec.Name, a.Spec.Description, a.Spec.Icon); e != nil {
				return fail(e)
			}
			if _, e = out.Icon(a.Spec.Icon); e != nil {
				return fail(e)
			}
			if a.Spec.HTTPPolicy != nil {
				if a.Spec.Provider != "http-cache" {
					return fail(fmt.Errorf("HTTP policy requires http-cache"))
				}
				if _, e = cachepolicy.Normalize(*a.Spec.HTTPPolicy); e != nil {
					return fail(e)
				}
			}
			if e = source(a.Spec); e != nil {
				return fail(e)
			}
			if e = distribution(a); e != nil {
				return yamlconfig.ErrorAt(raw, file, "distribution", e)
			}
			apps[a.Key()] = true
			out.Apps = append(out.Apps, a)
		}
		return nil
	})
	if err != nil {
		return Set{}, err
	}
	if err = out.ValidateTaxonomyReferences(); err != nil {
		return Set{}, err
	}
	for _, a := range out.Apps {
		if !vendors[a.Metadata.Vendor] {
			return Set{}, fmt.Errorf("%s: missing vendor", a.Key())
		}
	}
	// Preserve the existing two-provider inventory order with deterministic identity ordering.
	sort.Slice(out.Apps, func(i, j int) bool { return out.Apps[i].Key() > out.Apps[j].Key() })
	sort.Slice(out.Vendors, func(i, j int) bool { return out.Vendors[i].Metadata.ID > out.Vendors[j].Metadata.ID })
	return out, nil
}

var embeddedOnce sync.Once
var embeddedSet Set

func Embedded() Set {
	embeddedOnce.Do(func() {
		var err error
		embeddedSet, err = LoadFS(files)
		if err != nil {
			panic(err)
		}
	})
	out := Set{Taxonomy: TaxonomySpec{Categories: slices.Clone(embeddedSet.Taxonomy.Categories)}, Vendors: slices.Clone(embeddedSet.Vendors), Apps: slices.Clone(embeddedSet.Apps), images: embeddedSet.images}
	for i := range out.Apps {
		a := &out.Apps[i]
		a.Spec.BaseURLs = slices.Clone(a.Spec.BaseURLs)
		a.Spec.Categories = append([]string{}, a.Spec.Categories...)
		a.Spec.Tags = append([]string{}, a.Spec.Tags...)
		if a.Spec.HTTPPolicy != nil {
			policy, _ := cachepolicy.Normalize(*a.Spec.HTTPPolicy)
			a.Spec.HTTPPolicy = &policy
		}
		if a.Distribution != nil {
			d := *a.Distribution
			d.Channels = slices.Clone(d.Channels)
			d.Installers = slices.Clone(d.Installers)
			d.Assets = slices.Clone(d.Assets)
			a.Distribution = &d
		}
	}
	return out
}
func (s Set) Descriptors() []Descriptor {
	vendors := map[string]Vendor{}
	for _, v := range s.Vendors {
		vendors[v.Metadata.ID] = v
	}
	out := []Descriptor{}
	for _, a := range s.Apps {
		d := a.Distribution
		if d == nil {
			continue
		}
		icon, _ := s.Icon(a.Spec.Icon)
		out = append(out, Descriptor{ID: a.Key(), Name: a.Spec.Name.Localized(), Publisher: vendors[a.Metadata.Vendor].Spec.Name.En, Summary: a.Spec.Description.Localized(), Protocol: d.Protocol, TrustRevision: d.TrustRevision, Upstream: a.Spec.BaseURL, Channels: d.Channels, DefaultChannelTTLSeconds: a.Spec.CacheTTLSeconds, InstallerValidator: d.InstallerValidator, Installers: d.Installers, Assets: d.Assets, Icon: icon, UpdatePolicy: d.UpdatePolicy.Localized()})
	}
	return out
}
func Inventory(s Set) ([]byte, error) {
	return json.MarshalIndent(struct {
		SchemaVersion int          `json:"schema_version"`
		Applications  []Descriptor `json:"applications"`
	}{1, s.Descriptors()}, "", "  ")
}
