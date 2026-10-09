package presets

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

const taxonomyFixture = `schema_version: 1
kind: Taxonomy
spec:
  categories:
    - id: tools
      name: {en: Tools, zh-CN: 工具}
    - id: ai
      name: {en: AI, zh-CN: 人工智能}
`

func TestTaxonomyReservedLoaderAndReferences(t *testing.T) {
	f := fixture(t)
	f["_taxonomy.yaml"] = &fstest.MapFile{Data: []byte(taxonomyFixture)}
	f["openai/codex.yaml"].Data = []byte(strings.Replace(strings.Replace(string(f["openai/codex.yaml"].Data), `categories: []`, `categories: [tools, ai, tools]`, 1), `tags: []`, `tags: [" #CLI ", cli, 命令行]`, 1))
	s, err := LoadFS(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range s.Apps {
		if app.Key() == "openai/codex" && (!reflect.DeepEqual(app.Spec.Categories, []string{"ai", "tools"}) || !reflect.DeepEqual(app.Spec.Tags, []string{"CLI", "命令行"})) {
			t.Fatal(app.Spec)
		}
	}
	for _, test := range []struct{ name, dict, app string }{
		{"unknown category", taxonomyFixture, `categories: [unknown]`},
		{"invalid category", taxonomyFixture, `categories: [Not_A_Slug]`},
		{"empty tag", taxonomyFixture, `tags: ["#"]`},
		{"long tag", taxonomyFixture, `tags: [` + strings.Repeat("x", MaxTagRunes+1) + `]`},
		{"missing bilingual", strings.Replace(taxonomyFixture, "zh-CN: 工具", "zh-CN: ''", 1), `categories: []`},
		{"duplicate id", strings.Replace(taxonomyFixture, "id: ai", "id: tools", 1), `categories: []`},
		{"tag dictionary removed", taxonomyFixture + "  tags: []\n", `categories: []`},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := fixture(t)
			bad["_taxonomy.yaml"] = &fstest.MapFile{Data: []byte(test.dict)}
			data := string(bad["openai/codex.yaml"].Data)
			if strings.HasPrefix(test.app, "tags:") {
				data = strings.Replace(data, `tags: []`, test.app, 1)
			} else {
				data = strings.Replace(data, `categories: []`, test.app, 1)
			}
			bad["openai/codex.yaml"].Data = []byte(data)
			if _, err := LoadFS(bad); err == nil {
				t.Fatal("invalid taxonomy accepted")
			}
		})
	}
	f = fixture(t)
	delete(f, "_taxonomy.yaml")
	if _, err = LoadFS(f); err != nil {
		t.Fatal("optional empty dictionary", err)
	}
	f["other.yaml"] = &fstest.MapFile{Data: []byte(taxonomyFixture)}
	if _, err = LoadFS(f); err == nil {
		t.Fatal("reserved kind accepted at arbitrary path")
	}
}

func TestTagNormalizationIsLiteralNFCAndCaseInsensitive(t *testing.T) {
	decomposed := "Café"
	tags, err := NormalizeTags([]string{"  #" + decomposed + " ", "café", "CAFÉ", "a.b*", "  中文  "})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tags, []string{"Café", "a.b*", "中文"}) {
		t.Fatalf("%q", tags)
	}
	if FoldText("Café") != FoldText("CAFÉ") {
		t.Fatal("fold is not NFC and case-insensitive")
	}
	tooMany := make([]string, MaxTags+1)
	for i := range tooMany {
		tooMany[i] = strings.Repeat("t", i+1)
	}
	for _, bad := range [][]string{{""}, {"   "}, {"a\nb"}, {strings.Repeat("长", MaxTagRunes+1)}, tooMany} {
		if _, err := NormalizeTags(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if tags, err = NormalizeTags([]string{strings.Repeat("长", MaxTagRunes)}); err != nil || len(tags) != 1 {
		t.Fatal("64-character tag rejected", err)
	}
	categories, err := NormalizeCategories([]string{"b", "a", "b"})
	if err != nil || !reflect.DeepEqual(categories, []string{"a", "b"}) {
		t.Fatal(categories, err)
	}
}
