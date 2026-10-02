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
func Load(db *store.Store) (Settings, error) {
	value := Defaults()
	if db == nil {
		return value, nil
	}
	if err := db.Get("setting", "site", &value); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Settings{}, err
	}
	if err := value.Validate(); err != nil {
		return Settings{}, err
	}
	return value, nil
}
func Save(db *store.Store, value Settings) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return db.Put("setting", "site", value)
}
