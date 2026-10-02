// Package site stores public, plain-text bilingual site settings.
package site

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/store"
)

//go:embed defaults.json
var defaults []byte

type Text struct {
	EN   string `json:"en"`
	ZHCN string `json:"zh-CN"`
}
type Settings struct {
	Title      Text `json:"title"`
	Subtitle   Text `json:"subtitle"`
	Disclaimer Text `json:"disclaimer"`
}

func Defaults() Settings {
	var value Settings
	if err := json.Unmarshal(defaults, &value); err != nil {
		panic(err)
	}
	return value
}
func (s *Settings) Validate() error {
	for _, field := range []struct {
		value    *Text
		limit    int
		required bool
	}{{&s.Title, 80, true}, {&s.Subtitle, 160, false}, {&s.Disclaimer, 500, false}} {
		for _, value := range []*string{&field.value.EN, &field.value.ZHCN} {
			*value = strings.TrimSpace(*value)
			if !utf8.ValidString(*value) || utf8.RuneCountInString(*value) > field.limit || field.required && *value == "" {
				return errors.New("Site text is missing or exceeds its limit")
			}
			for _, r := range *value {
				if unicode.IsControl(r) && r != '\n' && r != '\t' {
					return errors.New("Site text contains control characters")
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
	revision, err := db.ReadSetting("global", "", "site", &value.Settings)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
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
	revision, err := db.CompareAndSwapSetting("global", "", "site", expected, value)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Settings: value, Revision: revision}, nil
}
