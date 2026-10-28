// Package releaseconfig defines the data-only contract of JSON release sources.
// It performs no network access and never evaluates expressions.
package releaseconfig

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	MaxArtifacts     = 128
	MaxRequests      = 32
	MaxMetadataBytes = 4 << 20
	MaxChecksumBytes = 64 << 10
	MaxQueryBytes    = 16 << 10
	MaxOutputBytes   = 4 << 20
)

var token = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,95}$`)
var identity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,95}$`)

// Artifact.Key describes a download choice, not its versioned filename.
// Unknown is explicit and must not be automatically recommended.
type Artifact struct {
	Key              string `json:"key"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	Format           string `json:"format"`
	Scope            string `json:"scope,omitempty"`
	Variant          string `json:"variant,omitempty"`
	URL              string `json:"url"`
	SHA256           string `json:"sha256,omitempty"`
	ChecksumURL      string `json:"checksum_url,omitempty"`
	ChecksumFilename string `json:"checksum_filename,omitempty"`
	Size             *int64 `json:"size,omitempty"`
	// Path is set only in public download choices: the artifact's address
	// relative to the application, for a downstream RedApp.
	Path string `json:"path,omitempty"`
}

type Release struct {
	Version   string     `json:"version"`
	Build     string     `json:"build,omitempty"`
	Artifacts []Artifact `json:"artifacts"`
}

// Validate distinguishes query output (sidecars still unresolved) from a
// verified snapshot. Source authorization is a separate, mandatory step.
func (r Release) Validate(verified bool) error {
	if !identity.MatchString(r.Version) || r.Build != "" && !identity.MatchString(r.Build) {
		return errors.New("invalid release version or build")
	}
	if len(r.Artifacts) == 0 || len(r.Artifacts) > MaxArtifacts {
		return errors.New("invalid artifact count")
	}
	seen := map[string]bool{}
	for _, a := range r.Artifacts {
		if !token.MatchString(a.Key) || seen[a.Key] {
			return errors.New("invalid or duplicate artifact key")
		}
		seen[a.Key] = true
		if !oneOf(a.OS, "windows", "linux", "macos", "unknown") || !oneOf(a.Arch, "x64", "arm64", "x86", "arm", "universal", "unknown") {
			return errors.New("unrecognized normalized platform")
		}
		if !token.MatchString(a.Format) || !oneOf(a.Scope, "", "user", "system", "portable", "unknown") || a.Variant != "" && !token.MatchString(a.Variant) {
			return errors.New("invalid artifact format, scope or variant")
		}
		if !ValidURL(a.URL) || a.Size != nil && *a.Size < 0 {
			return errors.New("invalid artifact URL or size")
		}
		if a.SHA256 != "" && !ValidSHA256(a.SHA256) {
			return errors.New("invalid expected SHA256")
		}
		if a.ChecksumURL != "" && !ValidURL(a.ChecksumURL) {
			return errors.New("invalid checksum URL")
		}
		if len(a.ChecksumFilename) > 512 || strings.ContainsAny(a.ChecksumFilename, "\r\n\x00") {
			return errors.New("invalid checksum filename")
		}
		if a.SHA256 == "" && (verified || a.ChecksumURL == "") {
			return errors.New("expected SHA256 is required")
		}
	}
	return nil
}
func oneOf(v string, values ...string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

// ValidKey accepts one canonical stable artifact key, not a filename or URL.
func ValidKey(s string) bool { return token.MatchString(s) }

func ValidSHA256(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
func ValidURL(raw string) bool {
	if len(raw) > 8192 || strings.ContainsAny(raw, "\r\n\t\x00") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.RawFragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return false
	}
	return true
}

// Merge combines platform fanout only when all outputs describe one release.
// A duplicate choice is rejected even when its bytes happen to match.
func Merge(parts []Release) (Release, error) {
	if len(parts) == 0 || len(parts) > MaxRequests {
		return Release{}, errors.New("invalid release result count")
	}
	out := Release{Version: parts[0].Version, Build: parts[0].Build}
	for _, part := range parts {
		if err := part.Validate(false); err != nil {
			return Release{}, err
		}
		if part.Version != out.Version || part.Build != out.Build {
			return Release{}, errors.New("platform releases are not synchronized")
		}
		if len(out.Artifacts)+len(part.Artifacts) > MaxArtifacts {
			return Release{}, errors.New("artifact count exceeds limit")
		}
		out.Artifacts = append(out.Artifacts, part.Artifacts...)
	}
	if err := out.Validate(false); err != nil {
		return Release{}, fmt.Errorf("merged release: %w", err)
	}
	return out, nil
}
