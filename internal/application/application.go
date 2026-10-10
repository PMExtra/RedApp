// Package application defines compiled provider capabilities and immutable
// application snapshots. It contains no runtime plugin loading.
package application

import (
	"context"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/presets"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/identity"
)

type Key struct{ Vendor, App string }

// ParseKey accepts only a canonical vendor/app identity; identity owns the
// slug grammar and the reserved vendor names.
func ParseKey(id string) (Key, error) {
	vendor, app, ok := strings.Cut(id, "/")
	if !ok || !identity.ValidKey(id) {
		return Key{}, errors.New("Invalid canonical application identity")
	}
	return Key{vendor, app}, nil
}
func (k Key) String() string { return k.Vendor + "/" + k.App }

type Localized = presets.Localized
type Installer = presets.Installer
type Asset = presets.Asset
type Descriptor = presets.Descriptor

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

// ErrUntrusted marks upstream metadata that was fetched but failed verification
// (digest, signature, URL or immutability rules). It also matches ErrUpstream.
var ErrUntrusted = fmt.Errorf("%w: metadata failed verification", ErrUpstream)
var ErrBusy = errors.New("Metadata concurrency limit exceeded")

// MetadataTimeout bounds one whole metadata request, including its body.
const MetadataTimeout = 5 * time.Minute

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
	// Metadata keeps an overall deadline; only artifact streams are unbounded.
	ctx, cancel := context.WithTimeout(ctx, MetadataTimeout)
	defer cancel()
	source, err := client.RelativeURL(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUpstream, err)
	}
	r, err := client.Get(ctx, source, http.Header{})
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
	Descriptor            Descriptor
	Protocol              Protocol
	Upstream              *distributor.Client
	Upstreams             []*distributor.Client
	SourceStrategy        string
	PublicAssets          map[string]Representation
	UID                   string
	Provider              string
	Revision              int64 // administrative configuration CAS revision
	VendorRevision        int64
	RuntimeRevision       int64 // admission/publication fence, independent of metadata/proxy edits
	VendorRuntimeRevision int64
	SourceEpoch           int64
	Enabled               bool // effective app and vendor status for persisted entries
	DeletedAt             *time.Time
	TemplateID            string // reviewed built-in template, independent of public identity
	VendorUID             string
	VendorID              string
	VendorName            Localized
	VendorDescription     Localized
	VendorIcon            string
	VendorLocalizedIcons  Localized
}

// StorageID isolates cache data whenever an application's source changes.
func (e Entry) StorageID() string { return identity.StorageID(e.UID, e.SourceEpoch) }

// MetricsID is stable across source changes and display metadata edits.
func (e Entry) MetricsID() string { return identity.MetricsID(e.UID) }

func (e Entry) Active() bool { return e.DeletedAt == nil && e.Enabled }

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

type PreparedRegistry struct{ snapshot *registrySnapshot }

func (r *Registry) Replace(entries []Entry) error {
	plan, err := PrepareRegistry(entries)
	if err != nil {
		return err
	}
	r.Publish(plan)
	return nil
}

// Publish only swaps a fully validated immutable snapshot; it cannot fail.
func (r *Registry) Publish(plan *PreparedRegistry) { r.current.Store(plan.snapshot) }

func PrepareRegistry(entries []Entry) (*PreparedRegistry, error) {
	snapshot := &registrySnapshot{byID: make(map[string]int, len(entries)), byStore: make(map[string]int, len(entries))}
	uids := map[string]bool{}
	for _, e := range entries {
		d := e.Descriptor
		if _, err := ParseKey(d.ID); err != nil {
			return nil, err
		}
		if _, found := snapshot.byID[d.ID]; found {
			return nil, fmt.Errorf("Duplicate application %s", d.ID)
		}
		if _, known := ProviderDefinition(e.Provider); !known {
			return nil, fmt.Errorf("Unknown provider for %s", d.ID)
		}
		// Every entry comes from a persisted directory application.
		if !identity.ValidUID(e.UID) || e.Revision < 1 || e.VendorRevision < 1 || e.SourceEpoch < 1 || uids[e.UID] {
			return nil, fmt.Errorf("Invalid runtime identity for %s", d.ID)
		}
		uids[e.UID] = true
		if e.Upstream == nil && e.Provider != Info && e.Provider != Hosted || d.DefaultChannelTTLSeconds < 0 || d.DefaultChannelTTLSeconds > 86400 {
			return nil, fmt.Errorf("Incomplete application %s", d.ID)
		}
		if e.Provider == Info || e.Provider == Hosted {
			if e.Upstream != nil || len(e.Upstreams) != 0 || e.Protocol != nil || len(d.Channels) != 0 || len(d.Installers) != 0 || len(d.Assets) != 0 || len(e.PublicAssets) != 0 || d.Upstream != "" || d.DefaultChannelTTLSeconds != 0 || d.TrustRevision != 0 || e.TemplateID != "" {
				return nil, fmt.Errorf("Content providers cannot declare upstream capabilities for %s", d.ID)
			}
		} else if e.Provider == HttpCache {
			if e.Protocol != nil || len(d.Channels) != 0 || len(d.Installers) != 0 || d.TrustRevision != 0 || e.TemplateID != "" {
				return nil, fmt.Errorf("GeneralHttp cannot declare release capabilities for %s", d.ID)
			}
		} else if e.Protocol == nil || d.TrustRevision < 1 || d.DefaultChannelTTLSeconds < 1 || len(d.Channels) == 0 {
			return nil, fmt.Errorf("Incomplete release application %s", d.ID)
		}
		channels := map[string]bool{}
		for _, ch := range d.Channels {
			if !identity.ValidSlug(ch) || channels[ch] {
				return nil, fmt.Errorf("Invalid or duplicate channel for %s", d.ID)
			}
			channels[ch] = true
		}
		files := map[string]bool{}
		for _, installer := range d.Installers {
			if !safePath(installer.File) || files[installer.File] || (installer.Shell != "sh" && installer.Shell != "bash" && installer.Shell != "powershell") {
				return nil, fmt.Errorf("Invalid installer descriptor for %s", d.ID)
			}
			files[installer.File] = true
		}
		for _, asset := range d.Assets {
			if !safePath(asset.File) || files[asset.File] {
				return nil, fmt.Errorf("Invalid public asset descriptor for %s", d.ID)
			}
			files[asset.File] = true
		}
		snapshot.byID[d.ID] = len(snapshot.entries)
		snapshot.byStore[e.StorageID()] = len(snapshot.entries)
		snapshot.entries = append(snapshot.entries, cloneEntry(e))
	}
	return &PreparedRegistry{snapshot: snapshot}, nil
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
