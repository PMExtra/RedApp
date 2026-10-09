package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/presets"
	"reflect"
	"strings"
	"testing"
)

func taxonomySet() presets.Set {
	s := presets.Embedded()
	s.Taxonomy = presets.TaxonomySpec{Categories: []presets.TaxonomyEntry{{ID: "tools", Name: presets.Text{En: "Tools", ZhCN: "工具"}}}}
	return s
}
func mapRaw(path string, value any) map[string]json.RawMessage {
	return map[string]json.RawMessage{path: encode(value)}
}
func categoryItem(t *testing.T, s *Store, id string) (TaxonomyItem, bool) {
	t.Helper()
	item, err := scanTaxonomy(s.DB.QueryRow(`SELECT `+taxonomyColumns+` FROM categories WHERE id=?`, id))
	if err != nil {
		return item, false
	}
	return item, true
}
func categoryIDs(t *testing.T, s *Store) []string {
	t.Helper()
	page, err := s.TaxonomyPage("", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, item := range page.Items {
		out = append(out, item.ID)
	}
	return out
}
func patchCategories(s *Store, key string, ids []string, names ...string) (Configuration, error) {
	c, err := s.ApplicationConfiguration(key)
	if err != nil {
		return c, err
	}
	return s.PatchApplicationConfiguration(key, ConfigurationPatch{Revision: c.Revision, Set: mapRaw("categories", ids), NewCategories: names})
}

func TestTaxonomyLanguageOverlayMissingAndRuntimeIsolation(t *testing.T) {
	s := openTest(t)
	set := taxonomySet()
	if err := s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	item, _ := categoryItem(t, s, "tools")
	before, _ := s.Application("openai/codex")
	public, _ := s.TaxonomyPublicRevision()
	var err error
	item, err = s.PatchTaxonomy("tools", ConfigurationPatch{Revision: item.Revision, Set: mapRaw("name.en", "Tools")})
	if err != nil {
		t.Fatal(err)
	}
	if item.Fields["name.en"].Source != "custom" || *item.Fields["name.en"].Differs {
		t.Fatal("same value override lost", item)
	}
	after, _ := s.Application(before.Key)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rename changed app", before, after)
	}
	next, _ := s.TaxonomyPublicRevision()
	if next <= public {
		t.Fatal("public revision unchanged")
	}
	set.Taxonomy.Categories[0].Name = presets.Text{En: "New tools", ZhCN: "新工具"}
	if err = s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	item, _ = categoryItem(t, s, "tools")
	if item.Name.En != "Tools" || item.Name.ZhCN != "新工具" {
		t.Fatal(item)
	}
	revision := item.Revision
	if err = s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	item, _ = categoryItem(t, s, "tools")
	if item.Revision != revision {
		t.Fatal("unchanged preset incremented revision")
	}
	item, err = s.PatchTaxonomy("tools", ConfigurationPatch{Revision: item.Revision, Unset: []string{"name.en"}})
	if err != nil || item.Name.En != "New tools" || item.Fields["name.en"].Source != "inherited" {
		t.Fatal(item, err)
	}
	if _, err = s.PatchTaxonomy("tools", ConfigurationPatch{Revision: item.Revision, Set: mapRaw("name.en", "x"), NewCategories: []string{"y"}}); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatal("rename accepted category creation", err)
	}
	set.Taxonomy.Categories = nil
	if err = s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	item, ok := categoryItem(t, s, "tools")
	if !ok || !item.TemplateMissing || item.Name.En != "New tools" {
		t.Fatal("missing built-in default lost or pruned", item)
	}
}

func TestCategoriesCreateReuseRollbackAndCleanupInOneTransaction(t *testing.T) {
	s := openTest(t)
	set := taxonomySet()
	set.Apps[0].Spec.Categories = []string{"tools"}
	if err := s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	bound := set.Apps[0].Key()
	if _, err := s.CreateVendor(VendorInput{ID: "acme", Name: LocalizedText{"Acme", "Acme"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateApplication("acme", ApplicationInput{ID: "other", Provider: "info", Name: LocalizedText{"Other", "其他"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Application(bound)
	c, err := patchCategories(s, bound, []string{"tools", "tools"}, "效率工具", "AI Tools", "ai tools")
	if err != nil {
		t.Fatal(err)
	}
	ids := c.Effective["categories"]
	after, _ := s.Application(bound)
	if after.Revision != before.Revision+1 || after.RuntimeRevision != before.RuntimeRevision || after.SourceEpoch != before.SourceEpoch {
		t.Fatal("category save changed runtime identity", before, after)
	}
	if len(after.Categories) != 3 || !strings.Contains(strings.Join(after.Categories, ","), "ai-tools") || c.Fields["categories"].Source != "custom" {
		t.Fatal("new categories not bound once", ids, after.Categories)
	}
	var chinese string
	for _, id := range after.Categories {
		if strings.HasPrefix(id, "category-") {
			chinese = id
		}
	}
	if item, ok := categoryItem(t, s, chinese); !ok || item.Name.ZhCN != "效率工具" || item.Name.En != "效率工具" || item.Builtin {
		t.Fatal("typed category not created with stable random ID", chinese, item)
	}
	// Same name in another language or case reuses the existing ID.
	if _, err = patchCategories(s, other.Key, []string{}, "AI TOOLS", "效率工具"); err != nil {
		t.Fatal(err)
	}
	other, _ = s.Application(other.Key)
	if !reflect.DeepEqual(other.Categories, []string{"ai-tools", chinese}) {
		t.Fatal("existing names not reused", other.Categories)
	}
	known := categoryIDs(t, s)
	// Ambiguity, stale revision and invalid sibling fields all roll back the whole save.
	first, _ := categoryItem(t, s, "ai-tools")
	if _, err = s.PatchTaxonomy("ai-tools", ConfigurationPatch{Revision: first.Revision, Set: mapRaw("name.zh-CN", "Shared")}); err != nil {
		t.Fatal(err)
	}
	second, _ := categoryItem(t, s, chinese)
	if _, err = s.PatchTaxonomy(chinese, ConfigurationPatch{Revision: second.Revision, Set: mapRaw("name.en", "shared")}); err != nil {
		t.Fatal(err)
	}
	if _, err = patchCategories(s, bound, []string{}, "SHARED"); !errors.Is(err, ErrCategoryAmbiguous) {
		t.Fatal("ambiguous name bound", err)
	}
	cfg, _ := s.ApplicationConfiguration(bound)
	invalid := ConfigurationPatch{Revision: cfg.Revision, Set: map[string]json.RawMessage{"categories": encode([]string{}), "cache_ttl_seconds": encode(-1)}, NewCategories: []string{"Rolled back"}}
	if _, err = s.PatchApplicationConfiguration(bound, invalid); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatal("invalid save accepted", err)
	}
	if _, err = s.PatchApplicationConfiguration(bound, ConfigurationPatch{Revision: cfg.Revision - 1, Set: mapRaw("categories", []string{}), NewCategories: []string{"Rolled back"}}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale save accepted", err)
	}
	if _, err = s.PatchApplicationConfiguration(bound, ConfigurationPatch{Revision: cfg.Revision, Set: mapRaw("name.en", "x"), NewCategories: []string{"Rolled back"}}); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatal("new category without categories field accepted", err)
	}
	if got := categoryIDs(t, s); !reflect.DeepEqual(got, known) {
		t.Fatal("failed save left categories", got, known)
	}
	if again, _ := s.ApplicationConfiguration(bound); again.Revision != cfg.Revision {
		t.Fatal("failed save advanced revision")
	}
	// A disabled App still owns its categories; the last live reference removes a custom one.
	if _, err = s.PatchApplicationFields(other.Key, other.Revision, nil, new(bool)); err != nil {
		t.Fatal(err)
	}
	public, _ := s.TaxonomyPublicRevision()
	patch(t, s, bound, map[string]any{"categories": []string{"tools"}})
	if _, ok := categoryItem(t, s, "ai-tools"); !ok {
		t.Fatal("category of disabled app pruned")
	}
	patch(t, s, other.Key, map[string]any{"categories": []string{"ai-tools"}})
	if _, ok := categoryItem(t, s, chinese); ok {
		t.Fatal("unused custom category kept")
	}
	if next, _ := s.TaxonomyPublicRevision(); next <= public {
		t.Fatal("cleanup did not advance public revision")
	}
	// Deleting the last App using a category prunes it in the same transaction.
	other, _ = s.Application(other.Key)
	if err = s.DeleteApplication(other.Key, other.Revision); err != nil {
		t.Fatal(err)
	}
	if _, ok := categoryItem(t, s, "ai-tools"); ok {
		t.Fatal("deleted app category kept")
	}
	// Template-referenced built-in categories survive both override and reset.
	patch(t, s, bound, map[string]any{"categories": []string{}})
	if _, ok := categoryItem(t, s, "tools"); !ok {
		t.Fatal("template category pruned")
	}
	c = patch(t, s, bound, nil, "categories")
	if !reflect.DeepEqual(c.Effective["categories"], []string{"tools"}) || c.Fields["categories"].Source != "inherited" {
		t.Fatal("reset did not restore template categories", c.Effective["categories"])
	}
}

func TestTagsNormalizeSearchLiterallyAndStayIndependentOfCategories(t *testing.T) {
	s := openTest(t)
	if err := s.ReconcileTemplates(taxonomySet()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateVendor(VendorInput{ID: "search", Name: LocalizedText{"Search", "搜索"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateApplication("search", ApplicationInput{ID: "tagged", Provider: "info", Name: LocalizedText{"Tagged", "标签"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	c := patch(t, s, a.Key, map[string]any{"tags": []string{" #Command Line ", "command line", "Café", "a.b*"}})
	if !reflect.DeepEqual(c.Effective["tags"], []string{"Command Line", "Café", "a.b*"}) {
		t.Fatal("tags not normalized in order", c.Effective["tags"])
	}
	for _, bad := range [][]string{{""}, {strings.Repeat("x", presets.MaxTagRunes+1)}, make([]string, presets.MaxTags+1)} {
		for i := range bad {
			if bad[i] == "" && len(bad) > 1 {
				bad[i] = fmt.Sprintf("tag-%d", i)
			}
		}
		cfg, _ := s.ApplicationConfiguration(a.Key)
		if _, err = s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: cfg.Revision, Set: mapRaw("tags", bad)}); !errors.Is(err, ErrInvalidDirectory) {
			t.Fatal("invalid tags accepted", len(bad), err)
		}
	}
	for _, q := range []string{"line", "#command", "COMMAND LINE", "café", "a.b*"} {
		page, err := s.ApplicationPage("", 1, 10, q, "current")
		if err != nil || page.Total != 1 || page.Items[0].Key != a.Key {
			t.Fatal("tag search missed", q, page.Total, err)
		}
	}
	for _, q := range []string{"a.b+", "c.mmand", "#"} {
		page, _ := s.ApplicationPage("", 1, 10, q, "current")
		if page.Total != 0 {
			t.Fatal("search treated tag text as a pattern", q)
		}
	}
	if got := categoryIDs(t, s); !reflect.DeepEqual(got, []string{"tools"}) {
		t.Fatal("tags created dictionary entries", got)
	}
}

func TestPublicCategoryCountsFilteringAndPagingWithoutDuplicates(t *testing.T) {
	s := openTest(t)
	if err := s.ReconcileTemplates(taxonomySet()); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"public", "hidden"} {
		if _, err := s.CreateVendor(VendorInput{ID: v, Name: LocalizedText{v, v}, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	create := func(vendor, id string, enabled bool, categories ...string) Application {
		a, err := s.CreateApplication(vendor, ApplicationInput{ID: id, Provider: "info", Name: LocalizedText{"Search " + id, "应用"}, Enabled: enabled, Categories: categories, Tags: []string{"shared", "search"}})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	if _, err := patchCategories(s, create("public", "seed", true).Key, []string{"tools"}, "Network"); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		create("public", fmt.Sprintf("app-%d", i), true, "tools", "network")
	}
	create("public", "disabled", false, "tools", "network")
	hiddenApp := create("hidden", "app", true, "tools")
	hidden, _ := s.Vendor("hidden")
	if _, err := s.PatchVendorFields(hidden.ID, hidden.Revision, nil, new(bool)); err != nil {
		t.Fatal(err)
	}
	labels, counts, err := s.PublicTaxonomy()
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 2 || counts[0].ID != "network" || counts[0].Count != 6 || counts[1].ID != "tools" || counts[1].Count != 6 {
		t.Fatal("public counts include hidden apps", counts)
	}
	if _, leaked := labels[hiddenApp.UID]; leaked {
		t.Fatal("hidden vendor app labelled")
	}
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		result, err := s.ApplicationCategoryPage("", page, 2, "search", "tools")
		if err != nil || result.Total != 6 || result.TotalPages != 3 {
			t.Fatal("multi-category join changed totals", result.Total, err)
		}
		for _, a := range result.Items {
			if seen[a.Key] || !a.Enabled {
				t.Fatal("duplicate or hidden app across pages", a.Key)
			}
			seen[a.Key] = true
		}
	}
	if len(seen) != 6 {
		t.Fatal("pages skipped apps", seen)
	}
}

func TestTaxonomyCustomIdentityDoesNotBecomeTemplate(t *testing.T) {
	s := openTest(t)
	set := taxonomySet()
	if err := s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	if _, err := patchCategories(s, set.Apps[0].Key(), []string{}, "custom"); err != nil {
		t.Fatal(err)
	}
	item, _ := categoryItem(t, s, "custom")
	if _, err := s.PatchTaxonomy("custom", ConfigurationPatch{Revision: item.Revision, Set: mapRaw("name.zh-CN", "我的名称")}); err != nil {
		t.Fatal(err)
	}
	set.Taxonomy.Categories = append(set.Taxonomy.Categories, presets.TaxonomyEntry{ID: "custom", Name: presets.Text{En: "Default name", ZhCN: "默认名称"}})
	set.Apps[1].Spec.Categories = []string{"custom"}
	if err := s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	current, _ := categoryItem(t, s, "custom")
	if current.Builtin || current.Name.ZhCN != "我的名称" {
		t.Fatal("preset took administrator ownership", current)
	}
	// The template reference now protects the administrator-owned category from cleanup.
	patch(t, s, set.Apps[0].Key(), map[string]any{"categories": []string{}})
	patch(t, s, set.Apps[1].Key(), map[string]any{"categories": []string{}})
	if _, ok := categoryItem(t, s, "custom"); !ok {
		t.Fatal("template-referenced category pruned")
	}
}
