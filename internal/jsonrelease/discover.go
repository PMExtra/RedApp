// Package jsonrelease prepares verified JSON release snapshots using trusted
// administrator queries and explicitly authorized upstream sources.
package jsonrelease

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/jsoncheck"
	"github.com/PMExtra/RedApp/internal/releaseconfig"
)

// QueryFunc returns exactly one JSON release object for a matrix row. A runner
// must enforce cancellation and output limits while producing it, not only
// after returning. This interface makes no hard memory-isolation promise.
type QueryFunc func(context.Context, string, json.RawMessage) (json.RawMessage, error)

// FetchFunc performs GET and must enforce allow on EVERY redirect, plus the
// distributor's resolved-address policy. Discover also checks initial/final
// URLs. The response body must honor ctx and an idle timeout while reading.
type FetchFunc func(context.Context, string, func(string) bool) (*http.Response, error)

type Discoverer struct {
	Query QueryFunc
	Fetch FetchFunc
}

var ErrLimit = errors.New("release metadata exceeds resource limits")
var ErrSource = errors.New("release source is unavailable or unauthorized")

// ErrVersionNotFound reports a looked-up version the upstream does not have.
var ErrVersionNotFound = errors.New("the upstream does not have this version")

// errMissing is a metadata request the upstream answered with 404.
var errMissing = fmt.Errorf("%w: not found", ErrSource)

// Discover is also the metadata-only preview path: artifact bodies are never
// requested. No publication occurs here, including on partial matrix failure.
func (d Discoverer) Discover(ctx context.Context, c releaseconfig.Config, channel string) (releaseconfig.Release, error) {
	if err := c.Validate(); err != nil {
		return releaseconfig.Release{}, err
	}
	if d.Query == nil || d.Fetch == nil {
		return releaseconfig.Release{}, errors.New("release discovery is not configured")
	}
	name, err := ResolveChannel(c, channel)
	if err != nil {
		return releaseconfig.Release{}, err
	}
	return d.discover(ctx, c, c.Channels[name], false)
}

// DiscoverVersion discovers one version with the lookup requests. A request
// the upstream answers with 404, or whose query returns null, is a package
// that version does not have; without any package the version is not found.
// The result must be the requested version. Nothing is published here.
func (d Discoverer) DiscoverVersion(ctx context.Context, c releaseconfig.Config, version string) (releaseconfig.Release, error) {
	if err := c.Validate(); err != nil {
		return releaseconfig.Release{}, err
	}
	if d.Query == nil || d.Fetch == nil {
		return releaseconfig.Release{}, errors.New("release discovery is not configured")
	}
	if c.Lookup == nil {
		return releaseconfig.Release{}, errors.New("the release configuration has no lookup")
	}
	if !releaseconfig.ValidVersion(version) {
		return releaseconfig.Release{}, ErrVersionNotFound
	}
	release, err := d.discover(ctx, c, c.LookupChannel(version), true)
	if err != nil {
		return releaseconfig.Release{}, err
	}
	if release.Version != version {
		return releaseconfig.Release{}, fmt.Errorf("the lookup returned version %q, not %q", release.Version, version)
	}
	return release, nil
}

func (d Discoverer) discover(ctx context.Context, c releaseconfig.Config, config releaseconfig.Channel, lookup bool) (releaseconfig.Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	parts := make([]releaseconfig.Release, 0, len(config.Requests))
	inputBudget, outputBudget := releaseconfig.MaxMetadataBytes, releaseconfig.MaxOutputBytes
	for _, request := range config.Requests {
		raw, err := d.read(ctx, request.URL, c.Sources.Metadata, inputBudget)
		if lookup && errors.Is(err, errMissing) {
			continue
		}
		if err != nil {
			return releaseconfig.Release{}, err
		}
		inputBudget -= len(raw)
		if !utf8.Valid(raw) || jsoncheck.Unique(raw) != nil {
			return releaseconfig.Release{}, errors.New("invalid metadata JSON")
		}
		input, err := json.Marshal(struct {
			Body    json.RawMessage   `json:"body"`
			Context map[string]string `json:"context"`
		}{raw, request.Context})
		if err != nil {
			return releaseconfig.Release{}, err
		}
		if len(input) > releaseconfig.MaxMetadataBytes {
			return releaseconfig.Release{}, ErrLimit
		}
		output, err := d.Query(ctx, config.Query, input)
		if err != nil {
			return releaseconfig.Release{}, err
		}
		if err = ctx.Err(); err != nil {
			return releaseconfig.Release{}, err
		}
		if len(output) > outputBudget {
			return releaseconfig.Release{}, ErrLimit
		}
		outputBudget -= len(output)
		if lookup && string(output) == "null" {
			continue
		}
		part, err := DecodeRelease(output, false)
		if err != nil {
			return releaseconfig.Release{}, err
		}
		parts = append(parts, part)
	}
	if lookup && len(parts) == 0 {
		return releaseconfig.Release{}, ErrVersionNotFound
	}
	merged, err := releaseconfig.Merge(parts)
	if err != nil {
		return releaseconfig.Release{}, err
	}
	if err = c.AuthorizeArtifacts(merged); err != nil {
		return releaseconfig.Release{}, err
	}
	checksumBudget := 1 << 20
	for i := range merged.Artifacts {
		artifact := &merged.Artifacts[i]
		if artifact.ChecksumURL == "" {
			continue
		}
		raw, err := d.read(ctx, artifact.ChecksumURL, c.Sources.Checksums, min(checksumBudget, releaseconfig.MaxChecksumBytes))
		if err != nil {
			return releaseconfig.Release{}, err
		}
		checksumBudget -= len(raw)
		digest, err := releaseconfig.ParseChecksum(raw, artifact.ChecksumFilename)
		if err != nil {
			return releaseconfig.Release{}, err
		}
		if artifact.SHA256 != "" && artifact.SHA256 != digest {
			return releaseconfig.Release{}, errors.New("metadata and sidecar SHA256 disagree")
		}
		artifact.SHA256 = digest
	}
	if err = ctx.Err(); err != nil {
		return releaseconfig.Release{}, err
	}
	if err = merged.Validate(true); err != nil {
		return releaseconfig.Release{}, err
	}
	return merged, nil
}

func ResolveChannel(c releaseconfig.Config, name string) (string, error) {
	if name == "latest" {
		name = c.DefaultChannel
	}
	if _, ok := c.Channels[name]; !ok {
		return "", errors.New("release channel is not declared")
	}
	return name, nil
}
func DecodeRelease(raw []byte, verified bool) (releaseconfig.Release, error) {
	if len(raw) == 0 || len(raw) > releaseconfig.MaxOutputBytes {
		return releaseconfig.Release{}, ErrLimit
	}
	if !utf8.Valid(raw) || jsoncheck.Unique(raw) != nil {
		return releaseconfig.Release{}, errors.New("invalid release JSON")
	}
	var r releaseconfig.Release
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&r); err != nil {
		return releaseconfig.Release{}, err
	}
	if err := r.Validate(verified); err != nil {
		return releaseconfig.Release{}, err
	}
	return r, nil
}
func (d Discoverer) read(ctx context.Context, raw string, sources []releaseconfig.Source, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, ErrLimit
	}
	allow := func(target string) bool { return releaseconfig.Allows(sources, target) }
	if !allow(raw) {
		return nil, ErrSource
	}
	response, err := d.Fetch(ctx, raw, allow)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrSource
	}
	if response == nil || response.Body == nil {
		return nil, ErrSource
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound && response.Request != nil && response.Request.URL != nil && allow(response.Request.URL.String()) {
		return nil, errMissing
	}
	if response.Request == nil || response.Request.URL == nil || !allow(response.Request.URL.String()) || response.StatusCode != http.StatusOK || response.Uncompressed || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return nil, ErrSource
	}
	if response.ContentLength > int64(limit) {
		return nil, ErrLimit
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > limit {
		return nil, ErrLimit
	}
	if response.ContentLength >= 0 && int64(len(body)) != response.ContentLength {
		return nil, io.ErrUnexpectedEOF
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}
