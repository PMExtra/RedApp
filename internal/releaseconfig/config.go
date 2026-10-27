package releaseconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

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

// SameSource excludes scheduling and prewarm choices: changing those must not
// discard verified immutable artifacts. Query/origin changes create a new epoch.
func SameSource(a, b *Config) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	left, right := *a, *b
	left.RefreshSeconds, right.RefreshSeconds = 0, 0
	left.Prewarm, right.Prewarm = nil, nil
	x, _ := json.Marshal(left)
	y, _ := json.Marshal(right)
	return bytes.Equal(x, y)
}
