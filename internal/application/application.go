// Package application defines compiled provider capabilities and immutable
// application snapshots. It contains no runtime plugin loading.
package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/identity"
)

var slug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Key struct{ Vendor, App string }

func ParseKey(id string) (Key, error) {
	p := strings.Split(id, "/")
	if len(p) != 2 {
		return Key{}, errors.New("Application identity must be vendor/app")
	}
	for _, s := range p {
		if len(s) < 1 || len(s) > 63 || !slug.MatchString(s) {
			return Key{}, errors.New("Invalid canonical application identity")
		}
	}
	switch p[0] {
	case "admin", "api", "assets", "health", "all":
		return Key{}, errors.New("Reserved application vendor")
	}
	return Key{p[0], p[1]}, nil
}
func (k Key) String() string { return k.Vendor + "/" + k.App }

type Localized map[string]string
type Installer struct {
	File   string `json:"file"`
	Source string `json:"source"`
	Shell  string `json:"shell"`
}
type Asset struct {
	File string `json:"file"`
	Kind string `json:"kind"`
}
type Descriptor struct {
	ID                       string      `json:"id"`
	Name                     Localized   `json:"name"`
	Publisher                string      `json:"publisher"`
	Summary                  Localized   `json:"summary"`
	Protocol                 string      `json:"protocol"`
	TrustRevision            int64       `json:"trust_revision"`
	Upstream                 string      `json:"upstream"`
	Channels                 []string    `json:"channels"`
	DefaultChannelTTLSeconds int         `json:"default_channel_ttl_seconds"`
	InstallerValidator       string      `json:"installer_validator"`
	Installers               []Installer `json:"installers"`
	Assets                   []Asset     `json:"assets"`
	Icon                     string      `json:"icon"`
	UpdatePolicy             Localized   `json:"update_policy"`
}

type OperationKind string

const (
	InstallerOperation OperationKind = "installer"
	StaticOperation    OperationKind = "static"
	ChannelOperation   OperationKind = "channel"
	MetadataOperation  OperationKind = "metadata"
	ArtifactOperation  OperationKind = "artifact"
	HTTPOperation      OperationKind = "http"
)

type Operation struct {
	Kind     OperationKind
	Target   string // channel or canonical version
	Resource string // version-relative logical artifact key
	Name     string // installer, public asset, or metadata filename
}
type Envelope struct{ Raw, Signature []byte }
type VerifiedArtifact struct {
	Key, Source, SHA256 string
	Size                *int64
}
type Release struct {
	Version   string
	Envelope  Envelope
	Artifacts []VerifiedArtifact
}
type ChannelResolution struct {
	Version  string
	Envelope *Envelope
}
type Representation struct {
	ContentType string
	Body        []byte
}

// Protocol is the optional immutable-release capability. GeneralHttp does not
// implement this interface or manufacture versions, channels or trusted hashes.
// A protocol is constructed with one fixed upstream, never a request-supplied URL.
type Protocol interface {
	ParsePath(string) (Operation, error)
	ValidateVersion(string) (string, error)
	CompareVersions(string, string) (int, error)
	ResolveChannel(context.Context, string) (ChannelResolution, error)
	FetchRelease(context.Context, string) (Envelope, error)
	VerifyRelease(string, Envelope) (Release, error)
	Render(Release, Operation, string) (Representation, error)
}

var ErrNotFound = errors.New("Application resource not found")
var ErrUpstream = errors.New("Trusted upstream metadata unavailable")
var ErrBusy = errors.New("Metadata concurrency limit exceeded")

// HTTPError retains the upstream status for structured operational events.
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("Metadata HTTP %d", e.Status) }
func (e *HTTPError) Unwrap() error {
	if e.Status == http.StatusNotFound {
		return ErrNotFound
	}
	return ErrUpstream
}

// ReadBody centralizes bounded reads and preserves missing-resource versus
// unavailable-upstream errors. Signature failures must never become a 404.
func ReadBody(ctx context.Context, client *distributor.Client, path string, limit int64) ([]byte, error) {
	r, err := client.Get(ctx, client.URL(path), http.Header{})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil, &HTTPError{Status: r.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	if len(b) == 0 || int64(len(b)) > limit {
		return nil, fmt.Errorf("%w: metadata exceeds size limits", ErrUpstream)
	}
	return b, nil
}

type Entry struct {
	Descriptor           Descriptor
	Protocol             Protocol
	Upstream             *distributor.Client
	Upstreams            []*distributor.Client
	SourceStrategy       string
	PublicAssets         map[string]Representation
	UID                  string
	Provider             string
	Revision             int64
	VendorRevision       int64
	SourceEpoch          int64
	Enabled              bool // effective app and vendor status for persisted entries
	DeletedAt            *time.Time
	TemplateID           string // reviewed built-in template, independent of public identity
	VendorID             string
	VendorName           Localized
	VendorDescription    Localized
	VendorIcon           string
	VendorLocalizedIcons Localized
}

// StorageID isolates cache data whenever an application's source changes.
// Legacy in-memory fixtures have no UID and retain their descriptor identity.
func (e Entry) StorageID() string {
	if e.UID == "" {
		return e.Descriptor.ID
	}
	return identity.StorageID(e.UID, e.SourceEpoch)
}

// MetricsID is stable across source changes and display metadata edits.
func (e Entry) MetricsID() string {
	if e.UID == "" {
		return e.Descriptor.ID
	}
	return identity.MetricsID(e.UID)
}

func (e Entry) Active() bool { return e.DeletedAt == nil && (e.UID == "" || e.Enabled) }

type registrySnapshot struct {
	entries []Entry
	byID    map[string]int
	byStore map[string]int
}

// Registry swaps a complete validated snapshot atomically. Readers holding an
// earlier Entry keep its immutable origin and source epoch for admitted work.
type Registry struct {
	current atomic.Pointer[registrySnapshot]
}

func NewRegistry(entries []Entry) (*Registry, error) {
	r := &Registry{}
	if err := r.Replace(entries); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) Replace(entries []Entry) error {
	snapshot := &registrySnapshot{byID: make(map[string]int, len(entries)), byStore: make(map[string]int, len(entries))}
	uids := map[string]bool{}
	for _, e := range entries {
		d := e.Descriptor
		if _, err := ParseKey(d.ID); err != nil {
			return err
		}
		if _, found := snapshot.byID[d.ID]; found {
			return fmt.Errorf("Duplicate application %s", d.ID)
		}
		if e.Provider != "" {
			if _, known := ProviderDefinition(e.Provider); !known {
				return fmt.Errorf("Unknown provider for %s", d.ID)
			}
		}
		if e.UID != "" {
			if !identity.ValidUID(e.UID) || e.Revision < 1 || e.VendorRevision < 1 || e.SourceEpoch < 1 || uids[e.UID] {
				return fmt.Errorf("Invalid runtime identity for %s", d.ID)
			}
			uids[e.UID] = true
		}
		if e.Upstream == nil && e.Provider != Info && e.Provider != Hosted || d.DefaultChannelTTLSeconds < 0 || d.DefaultChannelTTLSeconds > 86400 {
			return fmt.Errorf("Incomplete application %s", d.ID)
		}
		if e.Provider == Info || e.Provider == Hosted {
			if e.Upstream != nil || len(e.Upstreams) != 0 || e.Protocol != nil || len(d.Channels) != 0 || len(d.Installers) != 0 || len(d.Assets) != 0 || len(e.PublicAssets) != 0 || d.Upstream != "" || d.DefaultChannelTTLSeconds != 0 || d.TrustRevision != 0 || e.TemplateID != "" {
				return fmt.Errorf("Content providers cannot declare upstream capabilities for %s", d.ID)
			}
		} else if e.Provider == HttpCache {
			if e.Protocol != nil || len(d.Channels) != 0 || len(d.Installers) != 0 || d.TrustRevision != 0 || e.TemplateID != "" {
				return fmt.Errorf("GeneralHttp cannot declare release capabilities for %s", d.ID)
			}
		} else if e.Protocol == nil || d.TrustRevision < 1 || d.DefaultChannelTTLSeconds < 1 || len(d.Channels) == 0 {
			return fmt.Errorf("Incomplete release application %s", d.ID)
		}
		channels := map[string]bool{}
		for _, ch := range d.Channels {
			if !slug.MatchString(ch) || channels[ch] {
				return fmt.Errorf("Invalid or duplicate channel for %s", d.ID)
			}
			channels[ch] = true
		}
		files := map[string]bool{}
		for _, installer := range d.Installers {
			if !safePath(installer.File) || files[installer.File] || (installer.Shell != "sh" && installer.Shell != "bash" && installer.Shell != "powershell") {
				return fmt.Errorf("Invalid installer descriptor for %s", d.ID)
			}
			files[installer.File] = true
		}
		for _, asset := range d.Assets {
			if !safePath(asset.File) || files[asset.File] {
				return fmt.Errorf("Invalid public asset descriptor for %s", d.ID)
			}
			files[asset.File] = true
		}
		snapshot.byID[d.ID] = len(snapshot.entries)
		snapshot.byStore[e.StorageID()] = len(snapshot.entries)
		snapshot.entries = append(snapshot.entries, cloneEntry(e))
	}
	r.current.Store(snapshot)
	return nil
}
func (r *Registry) Lookup(id string) (Entry, bool) {
	e, ok := r.LookupAny(id)
	if !ok || !e.Active() {
		return Entry{}, false
	}
	return e, true
}

func (r *Registry) LookupAny(id string) (Entry, bool) {
	if r == nil {
		return Entry{}, false
	}
	snapshot := r.current.Load()
	if snapshot == nil {
		return Entry{}, false
	}
	i, ok := snapshot.byID[id]
	if !ok {
		return Entry{}, false
	}
	return cloneEntry(snapshot.entries[i]), true
}

func (r *Registry) LookupStorage(id string) (Entry, bool) {
	if r == nil {
		return Entry{}, false
	}
	snapshot := r.current.Load()
	if snapshot == nil {
		return Entry{}, false
	}
	i, ok := snapshot.byStore[id]
	if !ok || !snapshot.entries[i].Active() {
		return Entry{}, false
	}
	return cloneEntry(snapshot.entries[i]), true
}

func (r *Registry) AllEntries() []Entry {
	if r == nil {
		return nil
	}
	snapshot := r.current.Load()
	if snapshot == nil {
		return nil
	}
	out := make([]Entry, 0, len(snapshot.entries))
	for _, e := range snapshot.entries {
		out = append(out, cloneEntry(e))
	}
	return out
}

func (r *Registry) Entries() []Entry {
	all := r.AllEntries()
	out := all[:0]
	for _, e := range all {
		if e.Active() {
			out = append(out, e)
		}
	}
	return out
}
func (r *Registry) Upstreams() map[string]*distributor.Client {
	out := map[string]*distributor.Client{}
	for _, e := range r.AllEntries() {
		out[e.StorageID()] = e.Upstream
	}
	return out
}

func cloneEntry(e Entry) Entry {
	e.Upstreams = append([]*distributor.Client(nil), e.Upstreams...)
	e.VendorName = cloneLocalized(e.VendorName)
	e.VendorDescription = cloneLocalized(e.VendorDescription)
	e.Descriptor.Name = cloneLocalized(e.Descriptor.Name)
	e.Descriptor.Summary = cloneLocalized(e.Descriptor.Summary)
	e.Descriptor.UpdatePolicy = cloneLocalized(e.Descriptor.UpdatePolicy)
	e.Descriptor.Channels = append([]string(nil), e.Descriptor.Channels...)
	e.Descriptor.Installers = append([]Installer(nil), e.Descriptor.Installers...)
	e.Descriptor.Assets = append([]Asset(nil), e.Descriptor.Assets...)
	if e.DeletedAt != nil {
		t := *e.DeletedAt
		e.DeletedAt = &t
	}
	if e.PublicAssets != nil {
		out := make(map[string]Representation, len(e.PublicAssets))
		for key, value := range e.PublicAssets {
			value.Body = append([]byte(nil), value.Body...)
			out[key] = value
		}
		e.PublicAssets = out
	}
	return e
}

func cloneLocalized(in Localized) Localized {
	if in == nil {
		return nil
	}
	out := make(Localized, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
func (e Entry) HasChannel(channel string) bool {
	for _, c := range e.Descriptor.Channels {
		if c == channel {
			return true
		}
	}
	return false
}

func safePath(path string) bool {
	if path == "" || !utf8.ValidString(path) || strings.ContainsAny(path, "\\%?#\x00") || strings.HasPrefix(path, "/") {
		return false
	}
	for _, ch := range path {
		if unicode.IsControl(ch) {
			return false
		}
	}
	for _, p := range strings.Split(path, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return true
}

// ParsePath routes reviewed installer assets before protocol-specific content.
func (e Entry) ParsePath(path string) (Operation, error) {
	if !safePath(path) {
		return Operation{}, ErrNotFound
	}
	if _, ok := e.PublicAssets[path]; ok {
		return Operation{Kind: StaticOperation, Name: path}, nil
	}
	for _, i := range e.Descriptor.Installers {
		if path == i.File {
			return Operation{Kind: InstallerOperation, Name: path}, nil
		}
	}
	for _, a := range e.Descriptor.Assets {
		if path == a.File {
			return Operation{Kind: StaticOperation, Name: path}, nil
		}
	}
	if e.Provider == HttpCache {
		return Operation{Kind: HTTPOperation, Resource: path}, nil
	}
	if e.Protocol == nil {
		return Operation{}, ErrNotFound
	}
	return e.Protocol.ParsePath(path)
}
func (e Entry) PublicAsset(name string) (Representation, bool) {
	v, ok := e.PublicAssets[name]
	return v, ok
}
