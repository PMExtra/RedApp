// Package pathmatch matches already-decoded application-relative HTTP paths.
// It never interprets a filesystem path or walks directories.
package pathmatch

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
)

const MaxPatternBytes = 1024

var ErrInvalidPattern = errors.New("invalid path pattern")
var ErrInvalidPath = errors.New("expected a canonical decoded application path beginning with /")

type Spec struct {
	Type    string `json:"type"`
	Pattern string `json:"pattern"`
}

type Matcher struct {
	glob            string
	directoriesOnly bool
	all             bool
	regex           *regexp.Regexp
}

// Compile validates at configuration-save time. Glob syntax is doublestar's
// slash-separated Match syntax; RE2 expressions always match the entire path.
func Compile(spec Spec) (*Matcher, error) {
	if spec.Pattern == "" || len(spec.Pattern) > MaxPatternBytes || !utf8.ValidString(spec.Pattern) {
		return nil, fmt.Errorf("%w: pattern must contain 1..%d UTF-8 bytes", ErrInvalidPattern, MaxPatternBytes)
	}
	for _, r := range spec.Pattern {
		if unicode.IsControl(r) {
			return nil, fmt.Errorf("%w: control characters are not allowed", ErrInvalidPattern)
		}
	}
	switch spec.Type {
	case "glob":
		if !doublestar.ValidatePattern(spec.Pattern) {
			return nil, fmt.Errorf("%w: malformed glob", ErrInvalidPattern)
		}
		m := &Matcher{glob: spec.Pattern, all: spec.Pattern == "/", directoriesOnly: strings.HasSuffix(spec.Pattern, "/")}
		if m.directoriesOnly && !m.all {
			m.glob = strings.TrimSuffix(spec.Pattern, "/")
			if !doublestar.ValidatePattern(m.glob) {
				return nil, fmt.Errorf("%w: malformed directory glob", ErrInvalidPattern)
			}
		}
		return m, nil
	case "re2":
		compiled, err := regexp.Compile(`\A(?:` + spec.Pattern + `)\z`)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidPattern, err)
		}
		return &Matcher{regex: compiled}, nil
	default:
		return nil, fmt.Errorf("%w: type must be glob or re2", ErrInvalidPattern)
	}
}

// ValidatePath rejects alternate spellings instead of cleaning or decoding them.
// The HTTP boundary decodes once; persisted paths and matcher-test input obey
// the same contract. Query strings are never part of a match.
func ValidatePath(path string) error {
	if !strings.HasPrefix(path, "/") || !utf8.ValidString(path) || strings.ContainsAny(path, "\\%?#") || strings.Contains(path, "//") {
		return ErrInvalidPath
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return ErrInvalidPath
		}
	}
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return ErrInvalidPath
		}
	}
	return nil
}

// Match is shared by selection and explanation. A glob matches either the full
// path or a directory ancestor; its trailing slash excludes an exact file match.
// RE2 matches only the full path, never an ancestor or a substring.
func (m *Matcher) Match(path string) bool {
	if m == nil || ValidatePath(path) != nil {
		return false
	}
	if m.regex != nil {
		return m.regex.MatchString(path)
	}
	if m.all {
		return true
	}
	if !m.directoriesOnly {
		matched, _ := doublestar.Match(m.glob, path)
		if matched {
			return true
		}
	}
	for end := strings.LastIndexByte(path, '/'); end > 0; end = strings.LastIndexByte(path[:end], '/') {
		matched, _ := doublestar.Match(m.glob, path[:end])
		if matched {
			return true
		}
	}
	return false
}
