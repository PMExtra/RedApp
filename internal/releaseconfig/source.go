package releaseconfig

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// Source is an explicit origin and directory boundary for one role. Network
// callers must additionally enforce the distributor's address/redirect policy.
// Matching this rule alone does not authorize a connection to a resolved IP.
type Source struct {
	Origin string `json:"origin"`
	Prefix string `json:"prefix"`
}

type Sources struct {
	Metadata  []Source `json:"metadata"`
	Artifacts []Source `json:"artifacts"`
	Checksums []Source `json:"checksums"`
}

func (s Sources) MarshalJSON() ([]byte, error) {
	type wire Sources
	if s.Metadata == nil {
		s.Metadata = []Source{}
	}
	if s.Artifacts == nil {
		s.Artifacts = []Source{}
	}
	if s.Checksums == nil {
		s.Checksums = []Source{}
	}
	return json.Marshal(wire(s))
}

func (s Source) Validate() error {
	u, err := url.Parse(s.Origin)
	if err != nil || !ValidURL(s.Origin) || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Host != strings.ToLower(u.Host) {
		return errors.New("source must be a canonical HTTP(S) origin")
	}
	if s.Prefix == "" || !strings.HasPrefix(s.Prefix, "/") || !strings.HasSuffix(s.Prefix, "/") || !safePath(s.Prefix) {
		return errors.New("source prefix must be an absolute directory path")
	}
	return nil
}
func safePath(path string) bool {
	if strings.ContainsAny(path, "\\%?#\x00\r\n\t") {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// Allows must be called for the initial URL AND each redirect, using the
// relevant role's list; metadata origins do not implicitly authorize artifacts.
func Allows(sources []Source, raw string) bool {
	if !ValidURL(raw) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || !safePath(u.Path) {
		return false
	}
	escaped := strings.ToLower(u.EscapedPath())
	if strings.Contains(escaped, "%2f") || strings.Contains(escaped, "%5c") {
		return false
	}
	origin := u.Scheme + "://" + strings.ToLower(u.Host)
	for _, s := range sources {
		if s.Validate() == nil && s.Origin == origin && strings.HasPrefix(u.Path, s.Prefix) {
			return true
		}
	}
	return false
}
