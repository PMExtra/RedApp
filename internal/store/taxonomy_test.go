package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/presets"
	"reflect"
	"testing"
	"time"
)

func taxonomySet() presets.Set {
	s := presets.Embedded()
	s.Taxonomy = presets.TaxonomySpec{Categories: []presets.TaxonomyEntry{{ID: "tools", Name: presets.Text{En: "Tools", ZhCN: "工具"}}}, Tags: []presets.TaxonomyEntry{{ID: "cli", Name: presets.Text{En: "CLI", ZhCN: "命令行"}}, {ID: "ai", Name: presets.Text{En: "AI", ZhCN: "人工智能"}}}}
	return s
}
func TestTaxonomyLanguageOverlayMissingAndRuntimeIsolation(t *testing.T) {
	s := openTest(t)
	set := taxonomySet()
	if err := s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	item, _ := s.TaxonomyItem("categories", "tools")
	before, _ := s.Application("openai/codex")
	public, _ := s.TaxonomyPublicRevision()
	var err error
	item, err = s.PatchTaxonomy("categories", "tools", ConfigurationPatch{Revision: item.Revision, Set: mapRaw("name.en", "Tools")})
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
	item, _ = s.TaxonomyItem("categories", "tools")
	if item.Name.En != "Tools" || item.Name.ZhCN != "新工具" {
		t.Fatal(item)
	}
	revision := item.Revision
	if err = s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	item, _ = s.TaxonomyItem("categories", "tools")
	if item.Revision != revision {
		t.Fatal("unchanged preset incremented revision")
	}
	item, err = s.PatchTaxonomy("categories", "tools", ConfigurationPatch{Revision: item.Revision, Unset: []string{"name.en"}})
	if err != nil || item.Name.En != "New tools" || item.Fields["name.en"].Source != "inherited" {
		t.Fatal(item, err)
	}
	set.Taxonomy.Categories = nil
	if err = s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	item, _ = s.TaxonomyItem("categories", "tools")
	if !item.TemplateMissing || item.Name.En != "New tools" {
		t.Fatal("missing default lost", item)
	}
	if !errors.Is(s.DeleteTaxonomy(item.Kind, item.ID, item.Revision), ErrBuiltinTemplate) {
		t.Fatal("built-in deleted")
	}
}
func mapRaw(path string, value any) map[string]json.RawMessage {
	return map[string]json.RawMessage{path: encode(value)}
}
func TestTaxonomyAppLeavesAndReferenceDeletion(t *testing.T) {
	s := openTest(t)
	set := taxonomySet()
	set.Apps[0].Spec.Category = "tools"
	set.Apps[0].Spec.Tags = []string{"cli"}
	if err := s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	key := set.Apps[0].Key()
	before, _ := s.Application(key)
	public, _ := s.TaxonomyPublicRevision()
	c := patch(t, s, key, map[string]any{"category": "", "tags": []string{}})
	a, _ := s.Application(key)
	if a.Category != "" || len(a.Tags) != 0 || a.Revision <= before.Revision || a.SourceEpoch != before.SourceEpoch || a.RuntimeRevision != before.RuntimeRevision || c.Fields["tags"].Source != "custom" {
		t.Fatal(a, c)
	}
	now, _ := s.TaxonomyPublicRevision()
	if now <= public {
		t.Fatal("public revision unchanged")
	}
	c = patch(t, s, key, nil, "category")
	if c.Effective["category"] != "tools" || c.Fields["tags"].Source != "custom" {
		t.Fatal("leaf restore affected sibling", c)
	}
	for _, v := range []any{[]string{"cli", "cli"}, []string{"unknown"}, nil} {
		cfg, _ := s.ApplicationConfiguration(key)
		_, err := s.PatchApplicationConfiguration(key, ConfigurationPatch{Revision: cfg.Revision, Set: mapRaw("tags", v)})
		if err == nil {
			t.Fatal("invalid tags accepted", v)
		}
	}
	custom, err := s.CreateTaxonomy("tags", "custom", LocalizedText{En: "Custom", ZhCN: "自定义"})
	if err != nil {
		t.Fatal(err)
	}
	patch(t, s, key, map[string]any{"tags": []string{"custom"}})
	err = s.DeleteTaxonomy("tags", "custom", custom.Revision)
	var used *TaxonomyInUse
	if !errors.As(err, &used) || used.References != 1 {
		t.Fatal("disabled app reference ignored", err)
	}
	patch(t, s, key, map[string]any{"tags": []string{}})
	if err = s.DeleteTaxonomy("tags", "custom", custom.Revision); err != nil {
		t.Fatal(err)
	}
	// A user-created dictionary entry that is used only by a valid template must also block deletion.
	custom, err = s.CreateTaxonomy("tags", "template-only", LocalizedText{En: "Template", ZhCN: "模板"})
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := s.DB.Begin()
	snap := templateSnapshot{Key: "App:fixture", Present: true, Spec: func() Object {
		spec := set.Apps[0].Spec
		spec.Tags = []string{custom.ID}
		spec.Category = ""
		return object(spec)
	}()}
	if err = projectTemplateTaxonomy(tx, snap); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	err = s.DeleteTaxonomy("tags", custom.ID, custom.Revision)
	if !errors.As(err, &used) || used.References != 1 {
		t.Fatal("template reference ignored", err)
	}
	_, err = s.PatchTaxonomy("tags", custom.ID, ConfigurationPatch{Revision: custom.Revision + 1, Set: mapRaw("name.en", "Changed")})
	if !errors.Is(err, ErrConflict) {
		t.Fatal("CAS", err)
	}
}
func TestTaxonomyPublicFilteringAndRelatedOrder(t *testing.T) {
	s := openTest(t)
	if err := s.ReconcileTemplates(taxonomySet()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateVendor(VendorInput{ID: "test", Name: LocalizedText{En: "Test", ZhCN: "测试"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	create := func(id string, tags []string, enabled bool) Application {
		a, e := s.CreateApplication("test", ApplicationInput{ID: id, Provider: "info", Name: LocalizedText{En: "Search " + id, ZhCN: "应用"}, Category: "tools", Tags: tags, Enabled: enabled})
		if e != nil {
			t.Fatal(e)
		}
		return a
	}
	own := create("own", []string{"cli", "ai"}, true)
	two := create("two", []string{"ai", "cli"}, true)
	popular := create("popular", []string{"cli"}, true)
	for i := 0; i < 3; i++ {
		if err := s.RecordDownload(popular.UID, fmt.Sprintf("192.0.2.%d", i), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		create(id, []string{"cli"}, true)
	}
	create("disabled", []string{"cli", "ai"}, false)
	create("no-tag", nil, true)
	deleted := create("deleted", []string{"cli", "ai"}, true)
	if err := s.DeleteApplication(deleted.Key, deleted.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateVendor(VendorInput{ID: "hidden", Name: LocalizedText{En: "Hidden", ZhCN: "隐藏"}, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	hidden, err := s.CreateApplication("hidden", ApplicationInput{ID: "peer", Provider: "info", Name: LocalizedText{En: "Search hidden", ZhCN: "隐藏"}, Category: "tools", Tags: []string{"cli", "ai"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	related, err := s.RelatedApplications(own.UID, time.Now())
	if err != nil || !reflect.DeepEqual(related, []string{two.Key, popular.Key, "test/a", "test/b", "test/c", "test/d"}) {
		t.Fatal(related, err)
	}
	page, err := s.ApplicationCategoryPage("", 1, 2, "Search", "tools")
	if err != nil || page.Total != 11 || len(page.Items) != 2 {
		t.Fatal(page, err)
	}
	projections, categories, err := s.PublicTaxonomy()
	if err != nil || len(categories) != 1 || categories[0].Name.ZhCN != "工具" {
		t.Fatal(categories, err)
	}
	if _, ok := projections[hidden.UID]; ok {
		t.Fatal("disabled vendor projection")
	}
	if _, ok := projections[deleted.UID]; ok {
		t.Fatal("deleted app projection")
	}
	if _, ok := projections[own.UID]; !ok {
		t.Fatal("missing public projection")
	}
	none := create("empty", nil, true)
	related, err = s.RelatedApplications(none.UID, time.Now())
	if err != nil || len(related) != 0 {
		t.Fatal(related, err)
	}
}

func TestTaxonomyCustomIdentityDoesNotBecomeTemplate(t *testing.T) {
	s := openTest(t)
	set := taxonomySet()
	if err := s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	item, err := s.CreateTaxonomy("tags", "custom", LocalizedText{En: "My name", ZhCN: "我的名称"})
	if err != nil {
		t.Fatal(err)
	}
	set.Taxonomy.Tags = append(set.Taxonomy.Tags, presets.TaxonomyEntry{ID: "custom", Name: presets.Text{En: "Default name", ZhCN: "默认名称"}})
	set.Apps[0].Spec.Tags = []string{"custom"}
	if err = s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	current, _ := s.TaxonomyItem("tags", "custom")
	if current.Builtin || current.TemplateRef != nil || current.Defaults != nil || current.Revision != item.Revision || current.Name != item.Name {
		t.Fatal("custom identity rebound", current)
	}
	patch(t, s, set.Apps[0].Key(), map[string]any{"tags": []string{}})
	var used *TaxonomyInUse
	if err = s.DeleteTaxonomy(current.Kind, current.ID, current.Revision); !errors.As(err, &used) || used.References != 1 {
		t.Fatal("valid preset-only reference lost", err)
	}
}
