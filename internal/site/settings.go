// Package site stores public site texts and navigation.
package site

import (
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/config"
	"github.com/PMExtra/RedApp/internal/store"
)

//go:embed defaults.json
var defaults []byte

type Text struct {
	EN   string `json:"en"`
	ZHCN string `json:"zh-CN"`
}
type NavigationItem struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	NewTab bool   `json:"new_tab"`
}

type Settings struct {
	Navigation []NavigationItem `json:"navigation"`
	Title      Text             `json:"title"`
	Subtitle   Text             `json:"subtitle"`
	Disclaimer Text             `json:"disclaimer"`
}

func Defaults() Settings {
	var value Settings
	if err := json.Unmarshal(defaults, &value); err != nil {
		panic(err)
	}
	return value
}
func (s *Settings) Validate() error {
	if s.Navigation == nil {
		s.Navigation = []NavigationItem{}
	}
	if len(s.Navigation) > 20 {
		return errors.New("navigation accepts at most 20 items")
	}
	for i := range s.Navigation {
		item := &s.Navigation[i]
		item.Title = strings.TrimSpace(item.Title)
		if item.Title == "" || !utf8.ValidString(item.Title) || utf8.RuneCountInString(item.Title) > 80 || strings.IndexFunc(item.Title, unicode.IsControl) >= 0 {
			return errors.New("navigation title needs 1 to 80 characters without controls")
		}
		if !ValidNavigationURL(item.URL) {
			return errors.New("navigation URL must be a root-relative path or HTTP(S) URL without credentials, whitespace or backslashes")
		}
	}
	for _, field := range []struct {
		value    *Text
		limit    int
		required bool
	}{{&s.Title, 80, true}, {&s.Subtitle, 160, false}, {&s.Disclaimer, 500, false}} {
		for _, value := range []*string{&field.value.EN, &field.value.ZHCN} {
			*value = strings.TrimSpace(*value)
			if !utf8.ValidString(*value) || utf8.RuneCountInString(*value) > field.limit || field.required && *value == "" {
				return errors.New("site text is missing or exceeds its limit")
			}
			for _, r := range *value {
				if unicode.IsControl(r) && r != '\n' && r != '\t' {
					return errors.New("site text contains control characters")
				}
			}
		}
	}
	return nil
}

type Snapshot struct {
	Settings
	Revision int64 `json:"revision"`
}

func LoadSnapshot(db *store.Store) (Snapshot, error) {
	value := Snapshot{Settings: Defaults()}
	if db == nil {
		return value, nil
	}
	revision, err := db.ReadSiteSettings(&value.Settings)
	if err != nil {
		return Snapshot{}, err
	}
	value.Revision = revision
	if err := value.Validate(); err != nil {
		return Snapshot{}, err
	}
	return value, nil
}

func SaveCAS(db *store.Store, value Settings, expected int64) (Snapshot, error) {
	if err := value.Validate(); err != nil {
		return Snapshot{}, err
	}
	revision, err := db.SaveSiteSettings(expected, value)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Settings: value, Revision: revision}, nil
}

// ValidNavigationURL accepts browser navigation targets without scheme-relative or
// backslash ambiguity. Encoded controls and leading-slash ambiguity are rejected too.
func ValidNavigationURL(raw string) bool {
	if raw == "" || len(raw) > 2048 || !utf8.ValidString(raw) {
		return false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(decoded) {
		return false
	}
	for _, value := range []string{raw, decoded} {
		if strings.ContainsAny(value, "\\") || strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
			return false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Opaque != "" {
		return false
	}
	if strings.HasPrefix(raw, "/") {
		return !strings.HasPrefix(decoded, "//") && u.Host == "" && u.Scheme == ""
	}
	return (u.Scheme == "http" || u.Scheme == "https") && config.ValidHost(u.Host)
}
