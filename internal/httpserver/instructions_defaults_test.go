package httpserver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/store"
)

func TestDefaultVersionInstructionsUseOnlyObservedVersions(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	for _, key := range []string{"openai/codex", "anthropic/claude-code"} {
		entry, _ := h.server.Registry.Lookup(key)
		template, _ := store.BuiltinApplicationTemplate(key)
		for _, lang := range []string{"en", "zh-CN"} {
			route := "/api/apps/" + key + "/instructions/document?lang=" + lang
			raw, _ := h.request("GET", route, nil, 200, nil)
			doc := string(raw)
			count := 2
			if key == "anthropic/claude-code" {
				count = 4
			}
			if strings.Count(doc, `class="copy-block"`) != count || !strings.Contains(doc, versionInstructionsFallback(lang)) {
				t.Fatal("unknown version must have no invented command", key, lang)
			}
			if strings.Contains(doc, "1.2.3") || strings.Contains(doc, `<details class="installer-options" open`) {
				t.Fatal("fictional example or initially open disclosure")
			}
			summary := "Install a specific version"
			if lang == "zh-CN" {
				summary = "安装指定版本"
			}
			if !strings.Contains(doc, "<summary>"+summary+"</summary>") {
				t.Fatal("missing native disclosure", doc)
			}
		}
		for _, version := range []string{"1.9.0", "1.10.0", "999.0.0-evil'$(touch-x)"} {
			if err := h.server.DB.SeenFor(entry.StorageID(), version); err != nil {
				t.Fatal(err)
			}
		}
		for _, lang := range []string{"en", "zh-CN"} {
			raw, _ := h.request("GET", "/api/apps/"+key+"/instructions/document?lang="+lang, nil, 200, nil)
			doc := string(raw)
			advanced := strings.Split(doc, `<details class="installer-options">`)[1]
			count := 2
			if key == "anthropic/claude-code" {
				count = 4
			}
			if strings.Count(advanced, `class="copy-block"`) != count || !strings.Contains(advanced, "1.10.0") || strings.Contains(advanced, "touch-x") || strings.Contains(advanced, "1.2.3") {
				t.Fatal("expected Markdown version code within disclosure", advanced)
			}
			for _, forbidden := range []string{"--release latest", "-s -- latest", "'latest'"} {
				if strings.Contains(doc, forbidden) {
					t.Fatal("redundant latest example", forbidden)
				}
			}
			if key == "openai/codex" {
				if strings.Contains(advanced, "stable") || !strings.Contains(advanced, "--release '1.10.0'") || !strings.Contains(advanced, "-Release '1.10.0'") {
					t.Fatal(advanced)
				}
			} else if !strings.Contains(advanced, "bash -s -- '1.10.0'") || !strings.Contains(advanced, "-Target '1.10.0'") || !strings.Contains(advanced, "-Target 'stable'") {
				t.Fatal(advanced)
			}
		}
		if err := h.server.DB.SeenFor(entry.StorageID(), "2.0.0"); err != nil {
			t.Fatal(err)
		}
		raw, _ := h.request("GET", "/api/apps/"+key+"/instructions/document?lang=en", nil, 200, nil)
		if !strings.Contains(string(raw), "2.0.0") || strings.Contains(string(raw), "1.10.0") {
			t.Fatal("new observation not reflected")
		}
		for _, custom := range []string{"", template.Instructions.En + "\n\nCustom text: 1.2.3", "<pre><code>1.2.3</code></pre>\n" + knownVersionMarker} {
			result, err := h.server.defaultVersionInstructions(entry, custom, "en")
			if err != nil || result != custom {
				t.Fatal("custom document rewritten", fmt.Sprint(err))
			}
		}
		entry.SourceEpoch++
		result, err := h.server.defaultVersionInstructions(entry, template.Instructions.En, "en")
		if err != nil || result != template.Instructions.En {
			t.Fatal("old epoch version leaked")
		}
	}
}
