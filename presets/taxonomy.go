package presets

import (
	"fmt"
	"github.com/PMExtra/RedApp/internal/identity"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxCategories = 32
	MaxTags       = 50
	MaxTagRunes   = 64
)

type TaxonomyEntry struct {
	ID   string `json:"id"`
	Name Text   `json:"name"`
}
type TaxonomySpec struct {
	Categories []TaxonomyEntry `json:"categories"`
}
type Taxonomy struct {
	SchemaVersion int          `json:"schema_version"`
	Kind          string       `json:"kind"`
	Spec          TaxonomySpec `json:"spec"`
}

func (s TaxonomySpec) Validate() error {
	seen := map[string]bool{}
	for _, item := range s.Categories {
		if !identity.ValidSlug(item.ID) || seen[item.ID] || !ValidTaxonomyName(item.Name) {
			return fmt.Errorf("invalid category identity or bilingual name")
		}
		seen[item.ID] = true
	}
	return nil
}
func ValidTaxonomyName(name Text) bool {
	for _, v := range []string{name.En, name.ZhCN} {
		if strings.TrimSpace(v) == "" || !utf8.ValidString(v) || utf8.RuneCountInString(v) > 256 || strings.ContainsAny(v, "\x00\r\n") {
			return false
		}
	}
	return true
}

// NormalizeCategories returns the sorted set of category IDs; repeated IDs collapse to one association.
func NormalizeCategories(ids []string) ([]string, error) {
	out := append([]string{}, ids...)
	sort.Strings(out)
	out = slices.Compact(out)
	if len(out) > MaxCategories {
		return nil, fmt.Errorf("too many categories")
	}
	for _, id := range out {
		if !identity.ValidSlug(id) {
			return nil, fmt.Errorf("invalid category")
		}
	}
	return out, nil
}

// FoldText is the shared literal comparison form for tags, category names and tag search.
func FoldText(value string) string {
	return cases.Fold().String(norm.NFC.String(value))
}

// NormalizeTag trims, strips the display-only # prefix and applies NFC.
func NormalizeTag(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("invalid tag")
	}
	value = strings.TrimSpace(norm.NFC.String(value))
	value = strings.TrimSpace(strings.TrimLeft(value, "#"))
	if value == "" || utf8.RuneCountInString(value) > MaxTagRunes {
		return "", fmt.Errorf("tag must be 1..%d characters", MaxTagRunes)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("invalid tag")
		}
	}
	return value, nil
}

// NormalizeTags keeps the first spelling of case-insensitively equal tags in their given order.
func NormalizeTags(values []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range values {
		tag, err := NormalizeTag(raw)
		if err != nil {
			return nil, err
		}
		if key := FoldText(tag); !seen[key] {
			seen[key] = true
			out = append(out, tag)
		}
	}
	if len(out) > MaxTags {
		return nil, fmt.Errorf("too many tags")
	}
	return out, nil
}
func (s Set) ValidateTaxonomyReferences() error {
	if err := s.Taxonomy.Validate(); err != nil {
		return err
	}
	categories := map[string]bool{}
	for _, v := range s.Taxonomy.Categories {
		categories[v.ID] = true
	}
	for _, app := range s.Apps {
		ids, err := NormalizeCategories(app.Spec.Categories)
		if err != nil {
			return fmt.Errorf("%s: %w", app.Key(), err)
		}
		if _, err = NormalizeTags(app.Spec.Tags); err != nil {
			return fmt.Errorf("%s: %w", app.Key(), err)
		}
		for _, id := range ids {
			if !categories[id] {
				return fmt.Errorf("%s: unknown category", app.Key())
			}
		}
	}
	return nil
}
