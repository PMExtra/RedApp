package presets

import (
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
  tags:
    - id: cli
      name: {en: CLI, zh-CN: 命令行}
    - id: ai
      name: {en: AI, zh-CN: 人工智能}
`

func TestTaxonomyReservedLoaderAndReferences(t *testing.T) {
	f := fixture(t)
	f["_taxonomy.yaml"] = &fstest.MapFile{Data: []byte(taxonomyFixture)}
	f["openai/codex.yaml"].Data = []byte(strings.Replace(strings.Replace(string(f["openai/codex.yaml"].Data), `category: ""`, `category: tools`, 1), `tags: []`, `tags: [cli, ai]`, 1))
	s, err := LoadFS(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range s.Apps {
		if app.Key() == "openai/codex" && (app.Spec.Category != "tools" || strings.Join(app.Spec.Tags, ",") != "ai,cli") {
			t.Fatal(app.Spec)
		}
	}
	for _, test := range []struct{ name, dict, app string }{
		{"unknown", taxonomyFixture, `tags: [unknown]`}, {"duplicate", taxonomyFixture, `tags: [cli, cli]`}, {"missing bilingual", strings.Replace(taxonomyFixture, "zh-CN: 工具", "zh-CN: ''", 1), `tags: []`}, {"duplicate id", strings.Replace(taxonomyFixture, "id: ai", "id: cli", 1), `tags: []`},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := fixture(t)
			bad["_taxonomy.yaml"] = &fstest.MapFile{Data: []byte(test.dict)}
			bad["openai/codex.yaml"].Data = []byte(strings.Replace(string(bad["openai/codex.yaml"].Data), `tags: []`, test.app, 1))
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
