package presets

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func fixture(t *testing.T) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	if err := fs.WalkDir(files, ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			b, e := fs.ReadFile(files, p)
			if e != nil {
				return e
			}
			out[p] = &fstest.MapFile{Data: b}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestReviewedEquivalence(t *testing.T) {
	s := Embedded()
	if len(s.Vendors) != 2 || len(s.Apps) != 2 {
		t.Fatalf("unexpected entity count: %+v", s)
	}
	expected := map[string][2]string{
		"openai/codex":          {"c67dd70cfe28cb5ee2312790461dd1bbbc8449b90da5fd1d5faa4d2c0328f411", "6d65b60e90504947edc71f264a0d199069eb244a7d39edd31a29fb373bcd02de"},
		"anthropic/claude-code": {"0e01324021561d6312032735dbd91e7a7597d498a93533f3af5b198d648d0113", "a182fda9c01ce28ae750c32c4bbc4fae3b6dc8c34c057d105b9cf65989f08f25"},
	}
	for _, a := range s.Apps {
		h := expected[a.Key()]
		for i, text := range []string{a.Spec.Instructions.En, a.Spec.Instructions.ZhCN} {
			if fmt.Sprintf("%x", sha256.Sum256([]byte(text))) != h[i] {
				t.Fatalf("%s language %d instructions changed", a.Key(), i)
			}
		}
		if a.Spec.CacheTTLSeconds != 60 {
			t.Fatal("TTL changed")
		}
		if icon, e := Icon(a.Spec.Icon); e != nil || icon != ImagePrefix+a.Key()+"/icon.svg" {
			t.Fatal("icon changed")
		}
	}
	d := s.Descriptors()
	if d[0].ID != "openai/codex" || d[1].ID != "anthropic/claude-code" || d[0].Installers[0].Shell != "sh" || d[1].Installers[0].Shell != "bash" {
		t.Fatal("reviewed inventory changed")
	}
}

func TestCommunityPresetImages(t *testing.T) {
	f := fixture(t)
	body := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle cx="5" cy="5" r="4"/></svg>`)
	f["assets/community/new-logo.svg"] = &fstest.MapFile{Data: body}
	f["community.yaml"] = &fstest.MapFile{Data: []byte(`schema_version: 1
kind: Vendor
metadata: {id: community}
spec:
  name: {en: Community, zh-CN: Community}
  icon: assets/community/new-logo.svg
  localized_icons: {en: assets/community/new-logo.svg}
`)}
	f["community/tool.yaml"] = &fstest.MapFile{Data: []byte(`schema_version: 1
kind: App
metadata: {id: tool, vendor: community}
spec:
  name: {en: Tool, zh-CN: Tool}
  provider: info
  icon: assets/community/new-logo.svg
`)}
	s, err := LoadFS(f)
	if err != nil {
		t.Fatal(err)
	}
	var appIcon, vendorIcon, localizedIcon string
	for _, a := range s.Apps {
		if a.Key() == "community/tool" {
			appIcon = a.Spec.Icon
		}
	}
	for _, v := range s.Vendors {
		if v.Metadata.ID == "community" {
			vendorIcon, localizedIcon = v.Spec.Icon, v.Spec.LocalizedIcons.En
		}
	}
	if appIcon == "" || appIcon != vendorIcon || appIcon != localizedIcon {
		t.Fatal("new entity image references were not loaded")
	}
	url, err := s.Icon(appIcon)
	if err != nil || url != "/assets/presets/community/new-logo.svg" {
		t.Fatal(url, err)
	}
	image, ok := s.Image(url)
	if !ok || image.ContentType != "image/svg+xml" || string(image.Body) != string(body) {
		t.Fatal("image bytes or type changed")
	}
	image.Body[0] = 'X'
	again, _ := s.Image(url)
	if string(again.Body) != string(body) {
		t.Fatal("mutable image bytes")
	}
	for _, invalid := range []string{"assets/../openai.yaml", "/assets/community/new-logo.svg", `assets\community\new-logo.svg`, "https://example.test/image.svg", "assets/missing.svg"} {
		bad := fixture(t)
		bad["openai.yaml"].Data = []byte(strings.Replace(string(bad["openai.yaml"].Data), "assets/builtin/openai.svg", invalid, 1))
		if _, err := LoadFS(bad); err == nil {
			t.Fatalf("accepted invalid icon %q", invalid)
		}
	}
}

func TestRejectInvalidPresetImages(t *testing.T) {
	for name, body := range map[string]string{
		"script.svg":    `<svg><script>alert(1)</script></svg>`,
		"external.svg":  `<svg><path fill="url(https://example.test/a)"/></svg>`,
		"forged.svg":    "not an image",
		"forged.png":    `<svg/>`,
		"role.svg":      `<svg role="button"/>`,
		"run.sh":        "#!/bin/sh\necho bad",
		"oversized.svg": strings.Repeat(" ", 2<<20+1),
	} {
		t.Run(name, func(t *testing.T) {
			f := fixture(t)
			f["assets/"+name] = &fstest.MapFile{Data: []byte(body)}
			if _, err := LoadFS(f); err == nil || !strings.Contains(err.Error(), "assets/"+name) {
				t.Fatalf("missing resource rejection: %v", err)
			}
		})
	}
}
func TestWholePackageRejectsInvalidEntities(t *testing.T) {
	cases := []struct{ name, file, from, to string }{
		{"wrong kind", "openai.yaml", "kind: \"Vendor\"", "kind: \"App\""},
		{"path mismatch", "openai/codex.yaml", "id: \"codex\"", "id: \"other\""},
		{"duplicate key", "openai.yaml", "schema_version: 1", "schema_version: 1\nschema_version: 1"},
		{"unknown root", "openai.yaml", "schema_version: 1", "unknown: true\nschema_version: 1"},
		{"unknown spec", "openai.yaml", "spec:", "spec:\n  enabled: false"},
		{"invalid provider", "openai/codex.yaml", "provider: \"codex\"", "provider: \"command\""},
		{"null", "openai.yaml", "en: \"\"", "en: null"},
		{"external icon", "openai.yaml", "assets/builtin/openai.svg", "https://example.com/icon.svg"},
		{"executable icon", "openai.yaml", "assets/builtin/openai.svg", "install.sh"},
		{"unknown distribution", "openai/codex.yaml", "distribution:", "distribution:\n  command: dangerous"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := fixture(t)
			f[c.file].Data = []byte(strings.Replace(string(f[c.file].Data), c.from, c.to, 1))
			if _, e := LoadFS(f); e == nil || !strings.Contains(e.Error(), c.file) {
				t.Fatalf("missing file diagnostic: %v", e)
			}
		})
	}
	t.Run("missing vendor", func(t *testing.T) {
		f := fixture(t)
		delete(f, "openai.yaml")
		if _, e := LoadFS(f); e == nil {
			t.Fatal("accepted orphan")
		}
	})
	t.Run("second document", func(t *testing.T) {
		f := fixture(t)
		f["openai.yaml"].Data = append(f["openai.yaml"].Data, []byte("\n---\nkind: Vendor\n")...)
		if _, e := LoadFS(f); e == nil {
			t.Fatal("accepted multiple documents")
		}
	})
	t.Run("oversized", func(t *testing.T) {
		f := fixture(t)
		f["openai.yaml"].Data = append(f["openai.yaml"].Data, []byte(strings.Repeat(" ", 1<<20))...)
		if _, e := LoadFS(f); e == nil {
			t.Fatal("accepted oversized file")
		}
	})
}

func TestEmbeddedCallersCannotMutateReviewedDefaults(t *testing.T) {
	a := Embedded()
	a.Apps[0].Spec.Name.En = "changed"
	a.Apps[0].Distribution.Installers[0].Source = "https://changed.invalid/install.sh"
	a.Apps[0].Distribution.Channels[0] = "changed"
	b := Embedded()
	if b.Apps[0].Spec.Name.En == "changed" || b.Apps[0].Distribution.Channels[0] == "changed" || strings.Contains(b.Apps[0].Distribution.Installers[0].Source, "changed.invalid") {
		t.Fatal("caller changed reviewed shared defaults")
	}
}
