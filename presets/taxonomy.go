package presets

import (
	"fmt"
	"github.com/PMExtra/RedApp/internal/identity"
	"sort"
	"strings"
	"unicode/utf8"
)

type TaxonomyEntry struct {
	ID   string `json:"id"`
	Name Text   `json:"name"`
}
type TaxonomySpec struct {
	Categories []TaxonomyEntry `json:"categories"`
	Tags       []TaxonomyEntry `json:"tags"`
}
type Taxonomy struct {
	SchemaVersion int          `json:"schema_version"`
	Kind          string       `json:"kind"`
	Spec          TaxonomySpec `json:"spec"`
}

func (s TaxonomySpec) Validate() error {
	for _, items := range [][]TaxonomyEntry{s.Categories, s.Tags} {
		seen := map[string]bool{}
		for _, item := range items {
			if !identity.ValidSlug(item.ID) || seen[item.ID] || !ValidTaxonomyName(item.Name) {
				return fmt.Errorf("invalid taxonomy identity or bilingual name")
			}
			seen[item.ID] = true
		}
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
func NormalizeTaxonomy(category string, tags []string) ([]string, error) {
	if category != "" && !identity.ValidSlug(category) {
		return nil, fmt.Errorf("invalid category")
	}
	out := append([]string{}, tags...)
	sort.Strings(out)
	for i, id := range out {
		if !identity.ValidSlug(id) || i > 0 && out[i-1] == id {
			return nil, fmt.Errorf("invalid or duplicate tag")
		}
	}
	return out, nil
}
func (s Set) ValidateTaxonomyReferences() error {
	if err := s.Taxonomy.Validate(); err != nil {
		return err
	}
	categories, tags := map[string]bool{}, map[string]bool{}
	for _, v := range s.Taxonomy.Categories {
		categories[v.ID] = true
	}
	for _, v := range s.Taxonomy.Tags {
		tags[v.ID] = true
	}
	for _, app := range s.Apps {
		if _, err := NormalizeTaxonomy(app.Spec.Category, app.Spec.Tags); err != nil {
			return fmt.Errorf("%s: %w", app.Key(), err)
		}
		if app.Spec.Category != "" && !categories[app.Spec.Category] {
			return fmt.Errorf("%s: unknown category", app.Key())
		}
		for _, id := range app.Spec.Tags {
			if !tags[id] {
				return fmt.Errorf("%s: unknown tag", app.Key())
			}
		}
	}
	return nil
}
