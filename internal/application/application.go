// Package application defines the small, statically registered application contract.
// It contains no default application and no runtime plugin loading.
package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/PMExtra/RedApp/internal/distributor"
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
	case "admin", "api", "assets", "health":
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

// Protocol is the only application implementation interface. The two built-in
// protocols differ in paths, channels, versions, signatures, and representation.
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
	Descriptor   Descriptor
	Protocol     Protocol
	Upstream     *distributor.Client
	PublicAssets map[string]Representation
}
type Registry struct {
	entries []Entry
	byID    map[string]int
}

func NewRegistry(entries []Entry) (*Registry, error) {
	r := &Registry{byID: make(map[string]int, len(entries))}
	for _, e := range entries {
		d := e.Descriptor
		if _, err := ParseKey(d.ID); err != nil {
			return nil, err
		}
		if _, found := r.byID[d.ID]; found {
			return nil, fmt.Errorf("Duplicate application %s", d.ID)
		}
		if e.Protocol == nil || e.Upstream == nil || d.TrustRevision < 1 || d.DefaultChannelTTLSeconds < 1 || d.DefaultChannelTTLSeconds > 86400 || len(d.Channels) == 0 {
			return nil, fmt.Errorf("Incomplete application %s", d.ID)
		}
		channels := map[string]bool{}
		for _, ch := range d.Channels {
			if !slug.MatchString(ch) || channels[ch] {
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
		r.byID[d.ID] = len(r.entries)
		r.entries = append(r.entries, e)
	}
	return r, nil
}
func (r *Registry) Lookup(id string) (Entry, bool) {
	if r == nil {
		return Entry{}, false
	}
	i, ok := r.byID[id]
	if !ok {
		return Entry{}, false
	}
	return r.entries[i], true
}
func (r *Registry) Entries() []Entry { return append([]Entry(nil), r.entries...) }
func (r *Registry) Upstreams() map[string]*distributor.Client {
	out := map[string]*distributor.Client{}
	for _, e := range r.entries {
		out[e.Descriptor.ID] = e.Upstream
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
	if path == "" || strings.ContainsAny(path, "\\%?#\x00") || strings.HasPrefix(path, "/") {
		return false
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
	return e.Protocol.ParsePath(path)
}
func (e Entry) PublicAsset(name string) (Representation, bool) {
	v, ok := e.PublicAssets[name]
	return v, ok
}
