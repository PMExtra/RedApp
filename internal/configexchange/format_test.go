package configexchange

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/PMExtra/RedApp/presets"
	"os"
	"strings"
	"testing"
)

func fixtureDoc() Document {
	return Document{SchemaVersion: 1, Kind: "Vendor", Metadata: &presets.Metadata{ID: "test"}, Spec: map[string]any{"name": map[string]any{"en": "Name", "zh-CN": "名称"}, "description": map[string]any{"en": "Line one\n<script>line two</script>\n", "zh-CN": ""}, "icon": "", "localized_icons": map[string]any{"en": "", "zh-CN": ""}, "proxy": map[string]any{"mode": "inherit"}}}
}
func TestExchangeYAMLLiteralsZIPIdentityAndForbiddenDocuments(t *testing.T) {
	d := fixtureDoc()
	body, e := YAML(d)
	if e != nil || !bytes.Contains(body, []byte("|")) || bytes.Contains(body, []byte(`\n<script>`)) {
		t.Fatal(string(body), e)
	}
	p, e := Parse(body)
	if e != nil || p.Documents[0].Key() != "test" {
		t.Fatal(p, e)
	}
	raw, e := ZIP(Package{Documents: []Document{d}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Parse(raw); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{string(body) + "---\nkind: App\n", strings.Replace(string(body), "kind: Vendor", "kind: Unknown", 1), string(body) + "distribution: {}\n", string(body) + "trust: true\n", strings.Replace(string(body), "spec:", "spec: &anchor", 1)} {
		if _, e = Parse([]byte(bad)); e == nil {
			t.Fatal("unsafe YAML accepted")
		}
	}
	linked := d
	linked.Spec = nil
	ref := "test"
	linked.Template = &ref
	linked.TemplateHash = strings.Repeat("a", 64)
	linked.Overrides = map[string]any{}
	body, _ = YAML(linked)
	if !bytes.Contains(body, []byte("overrides: {}")) {
		t.Fatal("sparse root missing", string(body))
	}
}
func TestExchangeZIPRejectsPathsDuplicatesKindsAndLimits(t *testing.T) {
	d := fixtureDoc()
	body, _ := YAML(d)
	for _, test := range []struct {
		name    string
		paths   []string
		body    []byte
		size    uint64
		symlink bool
	}{
		{"escape", []string{"presets/../test.yaml"}, body, 0, false}, {"absolute", []string{"/presets/test.yaml"}, body, 0, false}, {"backslash", []string{`presets\test.yaml`}, body, 0, false}, {"encoded", []string{"presets/%2e/test.yaml"}, body, 0, false}, {"duplicate", []string{"presets/test.yaml", "presets/test.yaml"}, body, 0, false}, {"wrong identity", []string{"presets/wrong.yaml"}, body, 0, false}, {"unknown file", []string{"presets/run.sh"}, body, 0, false}, {"symlink", []string{"presets/test.yaml"}, body, 0, true}, {"yaml bound", []string{"presets/test.yaml"}, bytes.Repeat([]byte("a"), MaxYAML+1), 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var b bytes.Buffer
			writer := zip.NewWriter(&b)
			for _, name := range test.paths {
				h := &zip.FileHeader{Name: name, Method: zip.Deflate}
				h.SetMode(0600)
				if test.symlink {
					h.SetMode(os.ModeSymlink | 0600)
				}
				entry, _ := writer.CreateHeader(h)
				entry.Write(test.body)
			}
			writer.Close()
			if _, e := Parse(b.Bytes()); e == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	if _, e := Parse(bytes.Repeat([]byte("a"), MaxUpload+1)); e == nil {
		t.Fatal("upload bound")
	}
	// Small compressed fixtures exercise expansion/entity directory limits before decompression.
	var b bytes.Buffer
	writer := zip.NewWriter(&b)
	for i := 0; i <= MaxEntities; i++ {
		entry, _ := writer.Create(fmt.Sprintf("presets/v%d.yaml", i))
		entry.Write(body)
	}
	writer.Close()
	if _, e := Parse(b.Bytes()); e == nil {
		t.Fatal("entity bound")
	}
}

func TestExchangeDirectoryExpansionBoundBeforeOpeningBodies(t *testing.T) {
	var b bytes.Buffer
	writer := zip.NewWriter(&b)
	for i := 0; i < 65; i++ {
		entry, _ := writer.Create(fmt.Sprintf("presets/v%d.yaml", i))
		entry.Write([]byte("tiny body"))
	}
	writer.Close()
	raw := b.Bytes()
	for offset := 0; ; {
		n := bytes.Index(raw[offset:], []byte{'P', 'K', 1, 2})
		if n < 0 {
			break
		}
		offset += n
		binary.LittleEndian.PutUint32(raw[offset+24:offset+28], MaxYAML)
		offset += 46
	}
	if _, e := Parse(raw); e == nil {
		t.Fatal("aggregate expansion bound")
	}
}

func TestExchangeExportRejectsUnimportableSizes(t *testing.T) {
	d := fixtureDoc()
	d.Spec["description"] = strings.Repeat("x", MaxYAML)
	if _, err := ZIP(Package{Documents: []Document{d}}); err == nil {
		t.Fatal("export exceeded individual YAML import limit")
	}
	d.Spec["description"] = strings.Repeat("x", MaxYAML/2)
	documents := make([]Document, 129)
	for i := range documents {
		documents[i] = d
		documents[i].Metadata = &presets.Metadata{ID: fmt.Sprintf("vendor%d", i)}
	}
	if _, err := ZIP(Package{Documents: documents}); err == nil {
		t.Fatal("export exceeded aggregate expansion import limit")
	}
}
