package jsonrelease

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/releaseconfig"
)

// Protocol adapts discovery to the existing immutable catalog contract. The
// configuration is copied once; later administrative edits need a new source
// epoch and protocol, rather than mutating an admitted request's URL policy.
// Registration, channel scheduling and HTTP API wiring are separate owners.
type Protocol struct {
	config   releaseconfig.Config
	discover Discoverer
}

var _ application.Protocol = (*Protocol)(nil)

func NewProtocol(config releaseconfig.Config, discover Discoverer) (*Protocol, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if discover.Query == nil || discover.Fetch == nil {
		return nil, errors.New("release discovery is not configured")
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	var owned releaseconfig.Config
	if err = json.Unmarshal(raw, &owned); err != nil {
		return nil, err
	}
	return &Protocol{config: owned, discover: discover}, nil
}
func (p *Protocol) CanonicalChannel(channel string) (string, error) {
	return ResolveChannel(p.config, channel)
}
func (p *Protocol) ValidateVersion(version string) (string, error) {
	if len(version) != 66 || !strings.HasPrefix(version, "r-") {
		return "", application.ErrNotFound
	}
	if _, err := hex.DecodeString(version[2:]); err != nil || strings.ToLower(version) != version {
		return "", application.ErrNotFound
	}
	return version, nil
}
func (p *Protocol) CompareVersions(a, b string) (int, error) {
	if _, err := p.ValidateVersion(a); err != nil {
		return 0, err
	}
	if _, err := p.ValidateVersion(b); err != nil {
		return 0, err
	}
	if a == b {
		return 0, nil
	}
	// A digest is not a version ordering. Retention must use the stored display
	// version/build, never compare these path identities lexicographically.
	return 0, errors.New("release identities have no intrinsic version ordering")
}
func (p *Protocol) ResolveChannel(ctx context.Context, channel string) (application.ChannelResolution, error) {
	release, err := p.discover.Discover(ctx, p.config, channel)
	if err != nil {
		if errors.Is(err, ErrSource) {
			return application.ChannelResolution{}, fmt.Errorf("%w: %w", application.ErrUpstream, err)
		}
		return application.ChannelResolution{}, fmt.Errorf("%w: %w", application.ErrUntrusted, err)
	}
	snapshot, err := NewSnapshot(release)
	if err != nil {
		return application.ChannelResolution{}, err
	}
	return application.ChannelResolution{Version: snapshot.ID(), Envelope: &application.Envelope{Raw: append([]byte(nil), snapshot.canonical...)}}, nil
}
func (p *Protocol) FetchRelease(context.Context, string) (application.Envelope, error) {
	// A latest-only upstream cannot reconstruct an old immutable snapshot. Do
	// not rerun discovery and pretend its result belongs to a requested version.
	return application.Envelope{}, application.ErrNotFound
}
func (p *Protocol) snapshot(version string, envelope application.Envelope) (*Snapshot, error) {
	if _, err := p.ValidateVersion(version); err != nil {
		return nil, err
	}
	if len(envelope.Signature) != 0 {
		return nil, errors.New("unexpected JSON release signature")
	}
	release, err := DecodeRelease(envelope.Raw, true)
	if err != nil {
		return nil, err
	}
	if err = p.config.AuthorizeArtifacts(release); err != nil {
		return nil, err
	}
	snapshot, err := NewSnapshot(release)
	if err != nil {
		return nil, err
	}
	if snapshot.ID() != version {
		return nil, errors.New("release version/build does not match snapshot identity")
	}
	return snapshot, nil
}
func (p *Protocol) VerifyRelease(version string, envelope application.Envelope) (application.Release, error) {
	snapshot, err := p.snapshot(version, envelope)
	if err != nil {
		return application.Release{}, err
	}
	out := application.Release{Version: version, Envelope: application.Envelope{Raw: append([]byte(nil), snapshot.canonical...)}}
	for _, artifact := range snapshot.Release().Artifacts {
		out.Artifacts = append(out.Artifacts, application.VerifiedArtifact{Key: artifact.Key, Source: artifact.URL, SHA256: artifact.SHA256, Size: artifact.Size})
	}
	return out, nil
}
func (p *Protocol) ParsePath(path string) (application.Operation, error) {
	parts := strings.Split(path, "/")
	if len(parts) == 3 && parts[0] == "download" && releaseconfig.ValidKey(parts[2]) {
		channel, err := p.CanonicalChannel(parts[1])
		if err != nil {
			return application.Operation{}, application.ErrNotFound
		}
		return application.Operation{Kind: application.DownloadAliasOperation, Target: channel, Resource: parts[2]}, nil
	}
	if len(parts) != 3 || parts[0] != "releases" {
		return application.Operation{}, application.ErrNotFound
	}
	if _, err := p.ValidateVersion(parts[1]); err != nil {
		return application.Operation{}, err
	}
	// Stable artifact keys are rechecked against the authorized snapshot by the
	// catalog. No arbitrary file or upstream URL is reconstructed from this path.
	if !releaseconfig.ValidKey(parts[2]) {
		return application.Operation{}, application.ErrNotFound
	}
	return application.Operation{Kind: application.ArtifactOperation, Target: parts[1], Resource: parts[2]}, nil
}
func (p *Protocol) Render(application.Release, application.Operation, string) (application.Representation, error) {
	// Normalized public download options belong to the public API, not a copy
	// of private discovery metadata (which contains upstream URLs and sidecars).
	return application.Representation{}, application.ErrNotFound
}

func (p *Protocol) PrewarmKeys(channel string) []string {
	for _, selection := range p.config.Prewarm {
		if selection.Channel == channel {
			return append([]string(nil), selection.Keys...)
		}
	}
	return nil
}
func (p *Protocol) SelectArtifactKeys(release application.Release, keys []string) ([]string, error) {
	snapshot, err := p.snapshot(release.Version, release.Envelope)
	if err != nil {
		return nil, err
	}
	if _, err = snapshot.Select(keys); err != nil {
		return nil, err
	}
	return append([]string(nil), keys...), nil
}
