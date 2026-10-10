package codex

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
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta)(\.(0|[1-9][0-9]*)){0,2})?$`)
var assetPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)
var numberPattern = regexp.MustCompile(`[0-9]+`)

func Normalize(v string) (string, error) {
	v = strings.TrimPrefix(strings.TrimPrefix(v, "rust-v"), "v")
	if !versionPattern.MatchString(v) {
		return "", errors.New("Invalid version format")
	}
	for _, n := range numberPattern.FindAllString(v, -1) {
		if _, e := strconv.ParseUint(n, 10, 64); e != nil {
			return "", errors.New("Version number exceeds limit")
		}
	}
	return v, nil
}
func Compare(a, b string) (int, error) {
	var e error
	a, e = Normalize(a)
	if e != nil {
		return 0, e
	}
	b, e = Normalize(b)
	if e != nil {
		return 0, e
	}
	aa := strings.SplitN(a, "-", 2)
	bb := strings.SplitN(b, "-", 2)
	an := strings.Split(aa[0], ".")
	bn := strings.Split(bb[0], ".")
	for i := range an {
		x, _ := strconv.ParseUint(an[i], 10, 64)
		y, _ := strconv.ParseUint(bn[i], 10, 64)
		if x < y {
			return -1, nil
		}
		if x > y {
			return 1, nil
		}
	}
	if len(aa) == 1 && len(bb) == 1 {
		return 0, nil
	}
	if len(aa) == 1 {
		return 1, nil
	}
	if len(bb) == 1 {
		return -1, nil
	}
	ap := strings.Split(aa[1], ".")
	bp := strings.Split(bb[1], ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if i == 0 {
			if ap[i] < bp[i] {
				return -1, nil
			}
			if ap[i] > bp[i] {
				return 1, nil
			}
		} else {
			x, _ := strconv.ParseUint(ap[i], 10, 64)
			y, _ := strconv.ParseUint(bp[i], 10, 64)
			if x < y {
				return -1, nil
			}
			if x > y {
				return 1, nil
			}
		}
	}
	if len(ap) < len(bp) {
		return -1, nil
	}
	if len(ap) > len(bp) {
		return 1, nil
	}
	return 0, nil
}

type Asset struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	URL    string `json:"browser_download_url"`
	Size   *int64 `json:"size,omitempty"`
}
type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

type Protocol struct{ upstream *distributor.Client }

func NewProtocol(upstream *distributor.Client) *Protocol { return &Protocol{upstream: upstream} }
func (p *Protocol) ValidateVersion(input string) (string, error) {
	v, err := Normalize(input)
	if err != nil || v != input {
		return "", errors.New("Version must use canonical form")
	}
	return v, nil
}
func (p *Protocol) CompareVersions(a, b string) (int, error) { return Compare(a, b) }
func (p *Protocol) ParsePath(path string) (application.Operation, error) {
	if path == "channels/latest" {
		return application.Operation{Kind: application.ChannelOperation, Target: "latest", Name: "release.json"}, nil
	}
	parts := strings.Split(path, "/")
	if len(parts) != 3 || parts[0] != "releases" {
		return application.Operation{}, application.ErrNotFound
	}
	if _, err := p.ValidateVersion(parts[1]); err != nil || !assetPattern.MatchString(parts[2]) || parts[2] == "." || parts[2] == ".." {
		return application.Operation{}, application.ErrNotFound
	}
	if parts[2] == "release.json" {
		return application.Operation{Kind: application.MetadataOperation, Target: parts[1], Name: parts[2]}, nil
	}
	return application.Operation{Kind: application.ArtifactOperation, Target: parts[1], Resource: parts[2]}, nil
}
func (p *Protocol) ResolveChannel(ctx context.Context, name string) (application.ChannelResolution, error) {
	if name != "latest" {
		return application.ChannelResolution{}, application.ErrNotFound
	}
	raw, err := application.ReadBody(ctx, p.upstream, "channels/latest", 4<<20)
	if err != nil {
		return application.ChannelResolution{}, err
	}
	r, err := p.parse(raw, "latest")
	if err != nil {
		return application.ChannelResolution{}, fmt.Errorf("%w: %w", application.ErrUpstream, err)
	}
	v, _ := Normalize(r.Tag)
	return application.ChannelResolution{Version: v, Envelope: &application.Envelope{Raw: raw}}, nil
}
func (p *Protocol) FetchRelease(ctx context.Context, version string) (application.Envelope, error) {
	raw, err := application.ReadBody(ctx, p.upstream, "releases/"+version+"/release.json", 4<<20)
	return application.Envelope{Raw: raw}, err
}
func (p *Protocol) VerifyRelease(version string, envelope application.Envelope) (application.Release, error) {
	if len(envelope.Signature) != 0 {
		return application.Release{}, errors.New("Unexpected Codex metadata signature")
	}
	r, err := p.parse(envelope.Raw, version)
	if err != nil {
		return application.Release{}, err
	}
	out := application.Release{Version: version, Envelope: envelope, Artifacts: make([]application.VerifiedArtifact, 0, len(r.Assets))}
	for _, a := range r.Assets {
		source, err := p.upstream.RelativeURL("releases/" + version + "/" + a.Name)
		if err != nil {
			return application.Release{}, fmt.Errorf("artifact %s source: %w", a.Name, err)
		}
		out.Artifacts = append(out.Artifacts, application.VerifiedArtifact{Key: a.Name, Source: source, SHA256: strings.ToLower(a.Digest[7:]), Size: a.Size})
	}
	return out, nil
}
func (p *Protocol) Render(release application.Release, op application.Operation, publicBase string) (application.Representation, error) {
	if (op.Kind != application.ChannelOperation && op.Kind != application.MetadataOperation) || op.Name != "release.json" {
		return application.Representation{}, application.ErrNotFound
	}
	r, err := p.parse(release.Envelope.Raw, release.Version)
	if err != nil {
		return application.Representation{}, err
	}
	for i := range r.Assets {
		r.Assets[i].URL = publicBase + "/releases/" + release.Version + "/" + r.Assets[i].Name
	}
	body, err := json.Marshal(r)
	return application.Representation{ContentType: "application/json", Body: body}, err
}

func (p *Protocol) parse(body []byte, requested string) (Release, error) {
	if e := jsoncheck.Unique(body); e != nil {
		return Release{}, e
	}
	var r Release
	if e := json.Unmarshal(body, &r); e != nil {
		return Release{}, errors.New("Invalid JSON metadata")
	}
	v, e := Normalize(r.Tag)
	if e != nil || r.Tag != "rust-v"+v {
		return Release{}, errors.New("Invalid release tag")
	}
	if requested != "latest" && requested != v {
		return Release{}, errors.New("Release tag does not match request")
	}
	if len(r.Assets) == 0 || len(r.Assets) > 1024 {
		return Release{}, errors.New("Asset count exceeds limit")
	}
	seen := map[string]bool{}
	for _, a := range r.Assets {
		if !assetPattern.MatchString(a.Name) || a.Name == "." || a.Name == ".." || seen[a.Name] {
			return Release{}, errors.New("Invalid or duplicate asset name")
		}
		seen[a.Name] = true
		if !strings.HasPrefix(a.Digest, "sha256:") || len(a.Digest) != 71 {
			return Release{}, errors.New("Missing trusted SHA256")
		}
		if _, e := hex.DecodeString(a.Digest[7:]); e != nil {
			return Release{}, errors.New("Invalid SHA256")
		}
		if a.Size != nil && (*a.Size < 0 || *a.Size > 4<<30) {
			return Release{}, errors.New("Invalid asset length")
		}
		// A mirror may preserve the official canonical metadata URL. Never use
		// that field as a fetch destination: the authorized relative path is
		// always rebound to this instance's configured upstream in VerifyRelease.
		path := "releases/" + v + "/" + a.Name
		expected, e := p.upstream.RelativeURL(path)
		configured := e == nil && a.URL == expected
		official := a.URL == "https://releases.openai.com/codex/"+path
		if !configured && !official {
			return Release{}, errors.New("Asset URL is not authorized")
		}
	}
	return r, nil
}
