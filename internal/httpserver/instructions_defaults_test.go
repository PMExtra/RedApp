package httpserver

import (
	"html"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/store"
)

func TestDefaultVersionInstructionsUseOnlyObservedVersions(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	for _, key := range []string{"openai/codex", "anthropic/claude-code"} {
		entry, _ := h.server.Registry.Lookup(key)
		for _, lang := range []string{"en", "zh-CN"} {
			route := "/api/apps/" + key + "/instructions/document?lang=" + lang
			raw, _ := h.request("GET", route, nil, 200, nil)
			doc := string(raw)
			count := 4
			if key == "anthropic/claude-code" {
				count = 6
			}
			if strings.Count(doc, `class="copy-block"`) != count || !strings.Contains(doc, "&lt;version&gt;") || strings.Contains(doc, "&amp;lt;version") {
				t.Fatal("unknown version must retain literal placeholder commands", key, lang)
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
			doc := html.UnescapeString(string(raw))
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
		entry.SourceEpoch++
		result := h.server.interpolateInstructionVariables(entry, "<code>{{latest_version}}</code>", "https://example.test", "en")
		if result != "<code>&lt;version&gt;</code>" {
			t.Fatal("old epoch version leaked", result)
		}
	}
}

func TestEditableInstructionVariablesAndEscaping(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	key := "openai/codex"
	entry, _ := h.server.Registry.Lookup(key)
	current, _ := h.server.DB.Instructions(entry.UID)
	custom := "# Custom\n\n```sh\nprintf '%s' '{{latest_version}}' '{{base_url}}{{app_path}}'\n```\n\n`{{latest_version}}` {{unknown}}\n\n<!-- redapp:known-version -->"
	if _, err := h.server.DB.SaveInstructions(key, current.Revision, store.LocalizedText{En: custom, ZhCN: custom}); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "zh-CN"} {
		path := "/api/apps/" + key + "/instructions/document?lang=" + lang
		raw, _ := h.request("GET", path, nil, 200, nil)
		if !strings.Contains(string(raw), "&lt;version&gt;") || strings.Contains(string(raw), "&amp;lt;version") || !strings.Contains(string(raw), "{{unknown}}") || !strings.Contains(string(raw), "<!-- redapp:known-version -->") {
			t.Fatal("custom interpolation", string(raw))
		}
	}
	if err := h.server.DB.SeenFor(entry.StorageID(), "3.1.0"); err != nil {
		t.Fatal(err)
	}
	raw, _ := h.request("GET", "/api/apps/"+key+"/instructions/document?lang=en", nil, 200, nil)
	if !strings.Contains(string(raw), "3.1.0") || strings.Contains(string(raw), "&lt;version&gt;") {
		t.Fatal("editable variable did not update")
	}
	stored, _ := h.server.DB.Instructions(entry.UID)
	if stored.En != custom || stored.ZhCN != custom {
		t.Fatal("render modified stored custom Markdown")
	}
	entry.Descriptor.Name = map[string]string{"en": "<img src=x onerror=alert(1)> & \" '{{latest_version}}"}
	result := h.server.interpolateInstructionVariables(entry, `<code>{{app_name}}</code><a title="{{app_name}}">{{latest_version}}</a>`, "https://example.test", "en")
	if strings.Contains(result, "<img") || !strings.Contains(result, "&lt;img") || !strings.Contains(result, "{{latest_version}}") || !strings.Contains(result, "&#34;") {
		t.Fatal("unsafe or recursive scalar interpolation", result)
	}
	if _, err := h.server.DB.SaveInstructions(key, stored.Revision, store.LocalizedText{}); err != nil {
		t.Fatal(err)
	}
	raw, _ = h.request("GET", "/api/apps/"+key+"/instructions/document?lang=en", nil, 200, nil)
	if strings.Contains(string(raw), `class="copy-block"`) {
		t.Fatal("explicit empty was replaced")
	}
	if err := h.server.DB.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if got := h.server.interpolateInstructionVariables(entry, "{{latest_version}}", "", "en"); got != "&lt;version&gt;" {
		t.Fatal("version lookup failure must fall back", got)
	}
}
