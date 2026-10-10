package claude

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/jsoncheck"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`)
var platforms = map[string]string{"darwin-arm64": "claude", "darwin-x64": "claude", "linux-arm64": "claude", "linux-x64": "claude", "linux-arm64-musl": "claude", "linux-x64-musl": "claude", "win32-x64": "claude.exe", "win32-arm64": "claude.exe"}

func ValidVersion(v string) bool {
	if len(v) > 128 || !versionPattern.MatchString(v) {
		return false
	}
	for _, n := range strings.Split(strings.SplitN(v, "-", 2)[0], ".") {
		if _, err := strconv.ParseUint(n, 10, 64); err != nil {
			return false
		}
	}
	return true
}

type Platform struct {
	Binary   string `json:"binary"`
	Checksum string `json:"checksum"`
	Size     int64  `json:"size"`
}
type Manifest struct {
	Version   string              `json:"version"`
	Platforms map[string]Platform `json:"platforms"`
}

func parse(raw []byte, requested string) (Manifest, error) {
	if err := jsoncheck.Unique(raw); err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if json.Unmarshal(raw, &m) != nil || !ValidVersion(m.Version) || m.Version != requested || len(m.Platforms) == 0 || len(m.Platforms) > len(platforms) {
		return m, errors.New("invalid manifest version or platform count")
	}
	for platform, p := range m.Platforms {
		if platforms[platform] == "" || platforms[platform] != p.Binary || len(p.Checksum) != 64 || strings.ToLower(p.Checksum) != p.Checksum || p.Size <= 0 || p.Size > 4<<30 {
			return m, errors.New("invalid manifest platform")
		}
		if _, err := hex.DecodeString(p.Checksum); err != nil {
			return m, errors.New("invalid manifest digest")
		}
	}
	return m, nil
}
func Compare(a, b string) (int, error) {
	if !ValidVersion(a) || !ValidVersion(b) {
		return 0, errors.New("invalid Claude version")
	}
	x, y := strings.SplitN(a, "-", 2), strings.SplitN(b, "-", 2)
	xs, ys := strings.Split(x[0], "."), strings.Split(y[0], ".")
	for i := range xs {
		u, _ := strconv.ParseUint(xs[i], 10, 64)
		v, _ := strconv.ParseUint(ys[i], 10, 64)
		if u < v {
			return -1, nil
		}
		if u > v {
			return 1, nil
		}
	}
	if len(x) == 1 && len(y) == 1 {
		return 0, nil
	}
	if len(x) == 1 {
		return 1, nil
	}
	if len(y) == 1 {
		return -1, nil
	}
	// Conservatively retain prereleases when their ordering is not an ordinary stable threshold.
	if x[1] == y[1] {
		return 0, nil
	}
	return 0, errors.New("unordered prerelease identifiers")
}

type Protocol struct {
	upstream *distributor.Client
	verify   verifier
}

func NewProtocol(upstream *distributor.Client) *Protocol { return newProtocol(upstream, Verify) }

func newProtocol(upstream *distributor.Client, verify verifier) *Protocol {
	return &Protocol{upstream: upstream, verify: verify}
}
func (p *Protocol) ValidateVersion(v string) (string, error) {
	if !ValidVersion(v) {
		return "", errors.New("invalid Claude version")
	}
	return v, nil
}
func (p *Protocol) CompareVersions(a, b string) (int, error) { return Compare(a, b) }
func (p *Protocol) ParsePath(path string) (application.Operation, error) {
	if path == "latest" || path == "stable" {
		return application.Operation{Kind: application.ChannelOperation, Target: path}, nil
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || !ValidVersion(parts[0]) {
		return application.Operation{}, application.ErrNotFound
	}
	if len(parts) == 2 && (parts[1] == "manifest.json" || parts[1] == "manifest.json.sig") {
		return application.Operation{Kind: application.MetadataOperation, Target: parts[0], Name: parts[1]}, nil
	}
	if len(parts) == 3 && platforms[parts[1]] != "" && parts[2] == platforms[parts[1]] {
		return application.Operation{Kind: application.ArtifactOperation, Target: parts[0], Resource: parts[1] + "/" + parts[2]}, nil
	}
	return application.Operation{}, application.ErrNotFound
}
func (p *Protocol) ResolveChannel(ctx context.Context, name string) (application.ChannelResolution, error) {
	if name != "latest" && name != "stable" {
		return application.ChannelResolution{}, application.ErrNotFound
	}
	raw, err := application.ReadBody(ctx, p.upstream, name, 256)
	if err != nil {
		return application.ChannelResolution{}, err
	}
	version := strings.TrimSpace(string(raw))
	if !ValidVersion(version) {
		return application.ChannelResolution{}, fmt.Errorf("%w: invalid channel version", application.ErrUpstream)
	}
	return application.ChannelResolution{Version: version}, nil
}
func (p *Protocol) FetchRelease(ctx context.Context, version string) (application.Envelope, error) {
	raw, err := application.ReadBody(ctx, p.upstream, version+"/manifest.json", 1<<20)
	if err != nil {
		return application.Envelope{}, err
	}
	signature, err := application.ReadBody(ctx, p.upstream, version+"/manifest.json.sig", 16<<10)
	if err != nil {
		// The cause is formatted, not wrapped: a missing signature must stay an
		// upstream failure and never match application.ErrNotFound.
		return application.Envelope{}, fmt.Errorf("%w: signed manifest requires a valid signature: %v", application.ErrUpstream, err)
	}
	return application.Envelope{Raw: raw, Signature: signature}, nil
}
func (p *Protocol) VerifyRelease(version string, envelope application.Envelope) (application.Release, error) {
	if err := p.verify(envelope.Raw, envelope.Signature); err != nil {
		return application.Release{}, err
	}
	m, err := parse(envelope.Raw, version)
	if err != nil {
		return application.Release{}, err
	}
	out := application.Release{Version: version, Envelope: envelope}
	keys := make([]string, 0, len(m.Platforms))
	for platform := range m.Platforms {
		keys = append(keys, platform)
	}
	sort.Strings(keys)
	for _, platform := range keys {
		a := m.Platforms[platform]
		key := platform + "/" + a.Binary
		size := a.Size
		source, err := p.upstream.RelativeURL(version + "/" + key)
		if err != nil {
			return application.Release{}, fmt.Errorf("artifact %s source: %w", key, err)
		}
		out.Artifacts = append(out.Artifacts, application.VerifiedArtifact{Key: key, Source: source, SHA256: a.Checksum, Size: &size})
	}
	return out, nil
}
func (p *Protocol) Render(release application.Release, op application.Operation, _ string) (application.Representation, error) {
	if op.Kind == application.ChannelOperation {
		return application.Representation{ContentType: "text/plain; charset=utf-8", Body: []byte(release.Version + "\n")}, nil
	}
	if op.Kind == application.MetadataOperation && op.Name == "manifest.json" {
		return application.Representation{ContentType: "application/json", Body: release.Envelope.Raw}, nil
	}
	if op.Kind == application.MetadataOperation && op.Name == "manifest.json.sig" {
		return application.Representation{ContentType: "application/pgp-signature", Body: release.Envelope.Signature}, nil
	}
	return application.Representation{}, application.ErrNotFound
}
