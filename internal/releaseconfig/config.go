package releaseconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// VersionPlaceholder in a lookup request's URL or context values stands for
// the version an administrator looks up.
const VersionPlaceholder = "{version}"

// Request is one explicitly configured matrix row. Context carries normalized
// labels to the pure transform; it cannot alter the request destination.
type Request struct {
	URL     string            `json:"url"`
	Context map[string]string `json:"context,omitempty"`
}
type Channel struct {
	Requests []Request `json:"requests"`
	Query    string    `json:"query"`
}
type PrewarmSelection struct {
	Channel string   `json:"channel"`
	Keys    []string `json:"keys"`
}
type Config struct {
	DefaultChannel string             `json:"default_channel"`
	Channels       map[string]Channel `json:"channels"`
	// Lookup discovers one given version on request. Its requests may contain
	// VersionPlaceholder; its query returns null for a package the version
	// does not have.
	Lookup         *Channel           `json:"lookup,omitempty"`
	Sources        Sources            `json:"sources"`
	RefreshSeconds int                `json:"refresh_seconds"`
	Prewarm        []PrewarmSelection `json:"prewarm,omitempty"`
}

func (c Config) Validate() error {
	if len(c.Channels) == 0 || len(c.Channels) > 8 || !token.MatchString(c.DefaultChannel) {
		return errors.New("invalid release channels")
	}
	if _, ok := c.Channels[c.DefaultChannel]; !ok {
		return errors.New("default channel is not declared")
	}
	if c.RefreshSeconds < 60 || c.RefreshSeconds > 86400 {
		return errors.New("refresh interval must be 60..86400 seconds")
	}
	for _, group := range [][]Source{c.Sources.Metadata, c.Sources.Artifacts, c.Sources.Checksums} {
		if len(group) > 16 {
			return errors.New("too many source boundaries")
		}
		for _, source := range group {
			if err := source.Validate(); err != nil {
				return err
			}
		}
	}
	if len(c.Sources.Metadata) == 0 || len(c.Sources.Artifacts) == 0 {
		return errors.New("metadata and artifact boundaries are required")
	}
	for name, ch := range c.Channels {
		if !token.MatchString(name) || name == "latest" {
			return errors.New("invalid channel; latest is reserved for the default alias")
		}
		if err := c.validateRequests(ch); err != nil {
			return err
		}
	}
	if c.Lookup != nil {
		mentioned := false
		for _, request := range c.Lookup.Requests {
			mentioned = mentioned || strings.Contains(request.URL, VersionPlaceholder)
			for _, v := range request.Context {
				mentioned = mentioned || strings.Contains(v, VersionPlaceholder)
			}
		}
		if !mentioned {
			return errors.New("lookup requests must use " + VersionPlaceholder)
		}
		// A sample version checks the requests as they will be sent.
		if err := c.validateRequests(c.LookupChannel("0")); err != nil {
			return err
		}
	}
	if len(c.Prewarm) > len(c.Channels) {
		return errors.New("too many prewarm selections")
	}
	seenChannels := map[string]bool{}
	for _, selection := range c.Prewarm {
		if _, ok := c.Channels[selection.Channel]; !ok || seenChannels[selection.Channel] {
			return errors.New("invalid or duplicate prewarm channel")
		}
		seenChannels[selection.Channel] = true
		if len(selection.Keys) == 0 || len(selection.Keys) > MaxArtifacts {
			return errors.New("invalid prewarm selection count")
		}
		seen := map[string]bool{}
		for _, key := range selection.Keys {
			if !token.MatchString(key) || seen[key] {
				return errors.New("invalid or duplicate prewarm key")
			}
			seen[key] = true
		}
	}
	return nil
}

func (c Config) validateRequests(ch Channel) error {
	if len(ch.Requests) == 0 || len(ch.Requests) > MaxRequests || len(ch.Query) > MaxQueryBytes || strings.TrimSpace(ch.Query) == "" {
		return errors.New("invalid request matrix or query size")
	}
	for _, request := range ch.Requests {
		if !Allows(c.Sources.Metadata, request.URL) {
			return errors.New("metadata request is outside authorized sources")
		}
		if len(request.Context) > 16 {
			return errors.New("too many context fields")
		}
		for k, v := range request.Context {
			if !token.MatchString(k) || len(v) > 256 {
				return errors.New("invalid request context")
			}
		}
	}
	return nil
}

// LookupChannel is the lookup with VersionPlaceholder replaced by version:
// escaped in URLs, as is in context values. It requires a lookup.
func (c Config) LookupChannel(version string) Channel {
	out := Channel{Query: c.Lookup.Query, Requests: make([]Request, len(c.Lookup.Requests))}
	for i, request := range c.Lookup.Requests {
		out.Requests[i].URL = strings.ReplaceAll(request.URL, VersionPlaceholder, url.QueryEscape(version))
		if request.Context != nil {
			out.Requests[i].Context = map[string]string{}
			for k, v := range request.Context {
				out.Requests[i].Context[k] = strings.ReplaceAll(v, VersionPlaceholder, version)
			}
		}
	}
	return out
}

// AuthorizeArtifacts is applied both before fetching sidecars and when a
// persisted snapshot is read under current configuration.
func (c Config) AuthorizeArtifacts(r Release) error {
	for _, a := range r.Artifacts {
		if !Allows(c.Sources.Artifacts, a.URL) {
			return errors.New("artifact is outside authorized sources")
		}
		if a.ChecksumURL != "" && !Allows(c.Sources.Checksums, a.ChecksumURL) {
			return errors.New("checksum is outside authorized sources")
		}
	}
	return nil
}

// SameSource excludes scheduling, prewarm choices and the lookup, which only
// adds releases: changing those must not discard verified immutable artifacts. Query/origin changes create a new epoch.
func SameSource(a, b *Config) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	left, right := *a, *b
	left.RefreshSeconds, right.RefreshSeconds = 0, 0
	left.Prewarm, right.Prewarm = nil, nil
	left.Lookup, right.Lookup = nil, nil
	x, _ := json.Marshal(left)
	y, _ := json.Marshal(right)
	return bytes.Equal(x, y)
}
