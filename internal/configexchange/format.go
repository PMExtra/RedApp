// Package configexchange handles inert, untrusted administrative configuration files.
// Its document schema deliberately has no trusted distribution or executable fields.
package configexchange

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/media"
	"github.com/PMExtra/RedApp/internal/yamlconfig"
	"github.com/PMExtra/RedApp/presets"
	"go.yaml.in/yaml/v3"
	"io"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	MaxUpload   = 32 << 20
	MaxExpanded = 64 << 20
	MaxEntities = 1000
	MaxYAML     = 1 << 20
)

var ErrPackage = errors.New("Invalid configuration package")

type Document struct {
	SchemaVersion int               `json:"schema_version"`
	Kind          string            `json:"kind"`
	Metadata      *presets.Metadata `json:"metadata,omitempty"`
	Spec          map[string]any    `json:"spec,omitempty"`
	Template      *string           `json:"template,omitempty"`
	TemplateHash  string            `json:"template_hash,omitempty"`
	Overrides     map[string]any    `json:"overrides,omitempty"`
	AdminNotes    *string           `json:"admin_notes,omitempty"`
	OmittedFields []string          `json:"omitted_fields,omitempty"`
}
type Package struct {
	Documents []Document
	Assets    map[string][]byte
}

func (d Document) Key() string {
	if d.Metadata == nil {
		return ""
	}
	if d.Kind == "App" {
		return d.Metadata.Vendor + "/" + d.Metadata.ID
	}
	return d.Metadata.ID
}
func (d Document) Path() string {
	if d.Kind == "Taxonomy" {
		return "presets/_taxonomy.yaml"
	}
	return "presets/" + d.Key() + ".yaml"
}
func (d Document) Validate() error {
	if d.SchemaVersion != 1 {
		return ErrPackage
	}
	if d.Kind == "Taxonomy" {
		if d.Metadata != nil || d.Template != nil || d.TemplateHash != "" || d.Overrides != nil || d.AdminNotes != nil || len(d.OmittedFields) != 0 {
			return ErrPackage
		}
		var spec presets.TaxonomySpec
		if strict(d.Spec, &spec) != nil || spec.Validate() != nil || len(spec.Categories)+len(spec.Tags) > MaxEntities {
			return ErrPackage
		}
		return nil
	}
	if d.Kind != "Vendor" && d.Kind != "App" || d.Metadata == nil || !identity.ValidSlug(d.Metadata.ID) {
		return ErrPackage
	}
	if d.Kind == "Vendor" && (!identity.ValidVendor(d.Metadata.ID) || d.Metadata.Vendor != "") || d.Kind == "App" && !identity.ValidVendor(d.Metadata.Vendor) {
		return ErrPackage
	}
	if d.Template != nil {
		if d.Spec != nil || *d.Template == "" || d.Kind == "Vendor" && !identity.ValidVendor(*d.Template) || d.Kind == "App" && !identity.ValidKey(*d.Template) {
			return ErrPackage
		}
		if len(d.TemplateHash) != 64 {
			return ErrPackage
		}
		if _, e := hex.DecodeString(d.TemplateHash); e != nil {
			return ErrPackage
		}
	} else if d.Spec == nil || d.Overrides != nil || d.TemplateHash != "" {
		return ErrPackage
	}
	if len(d.OmittedFields) > 1 || len(d.OmittedFields) == 1 && d.OmittedFields[0] != "proxy" {
		return ErrPackage
	}
	for _, o := range d.OmittedFields {
		if _, ok := d.Spec[o]; ok {
			return ErrPackage
		}
		if _, ok := d.Overrides[o]; ok {
			return ErrPackage
		}
	}
	return nil
}
func strict(value any, target any) error {
	raw, e := json.Marshal(value)
	if e != nil {
		return e
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
func Parse(raw []byte) (Package, error) {
	if len(raw) > MaxUpload || len(raw) == 0 {
		return Package{}, ErrPackage
	}
	p := Package{Assets: map[string][]byte{}}
	if !bytes.HasPrefix(raw, []byte("PK")) {
		d, e := decode(raw)
		if e != nil {
			return p, e
		}
		p.Documents = []Document{d}
		if !entityBound(p.Documents) {
			return p, ErrPackage
		}
		return p, nil
	}
	z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		return p, ErrPackage
	}
	seen := map[string]bool{}
	var total uint64
	entities := 0
	// Check the central directory before opening any compressed body.
	for _, f := range z.File {
		name := f.Name
		if !safePath(name) || seen[name] || !f.Mode().IsRegular() {
			return p, ErrPackage
		}
		seen[name] = true
		limit := uint64(MaxYAML)
		if strings.HasPrefix(name, "presets/assets/") {
			limit = media.MaxBytes
			if !assetPath(name) {
				return p, ErrPackage
			}
		} else if strings.HasSuffix(name, ".yaml") {
			entities++
			if entities > MaxEntities {
				return p, ErrPackage
			}
		} else {
			return p, ErrPackage
		}
		if f.UncompressedSize64 > limit || f.UncompressedSize64 > MaxExpanded-total {
			return p, ErrPackage
		}
		total += f.UncompressedSize64
	}
	if entities == 0 {
		return p, ErrPackage
	}
	for _, f := range z.File {
		reader, e := f.Open()
		if e != nil {
			return p, ErrPackage
		}
		body, e := io.ReadAll(io.LimitReader(reader, int64(f.UncompressedSize64)+1))
		reader.Close()
		if e != nil || uint64(len(body)) != f.UncompressedSize64 {
			return p, ErrPackage
		}
		if strings.HasPrefix(f.Name, "presets/assets/") {
			if _, e = media.ValidateStaticImage(body, path.Ext(f.Name)); e != nil {
				return p, ErrPackage
			}
			digest := sha256.Sum256(body)
			if path.Base(f.Name) != hex.EncodeToString(digest[:])+path.Ext(f.Name) {
				return p, ErrPackage
			}
			p.Assets[strings.TrimPrefix(f.Name, "presets/")] = body
			continue
		}
		d, e := decode(body)
		if e != nil || d.Path() != f.Name {
			return p, ErrPackage
		}
		p.Documents = append(p.Documents, d)
	}
	if !entityBound(p.Documents) {
		return p, ErrPackage
	}
	sort.Slice(p.Documents, func(i, j int) bool { return p.Documents[i].Path() < p.Documents[j].Path() })
	return p, nil
}
func safePath(name string) bool {
	return strings.HasPrefix(name, "presets/") && path.Clean(name) == name && !strings.ContainsAny(name, "\\%\x00") && !strings.HasPrefix(name, "/") && !strings.Contains(name, "../")
}
func assetPath(name string) bool {
	base := path.Base(name)
	ext := path.Ext(base)
	if path.Dir(name) != "presets/assets" || len(base) != 64+len(ext) || (ext != ".png" && ext != ".jpg" && ext != ".svg") {
		return false
	}
	_, e := hex.DecodeString(strings.TrimSuffix(base, ext))
	return e == nil && strings.ToLower(base) == base
}
func decode(raw []byte) (Document, error) {
	var d Document
	if !utf8.Valid(raw) || yamlconfig.Decode(raw, "configuration", MaxYAML, &d) != nil || d.Validate() != nil {
		return d, ErrPackage
	}
	return d, nil
}

// JSON is only an intermediate ordered tree; all multiline strings are YAML literals.
func YAML(d Document) ([]byte, error) {
	raw, e := json.Marshal(d)
	if e != nil {
		return nil, e
	}
	var node yaml.Node
	if e = yaml.Unmarshal(raw, &node); e != nil {
		return nil, e
	}
	if d.Template != nil {
		root := node.Content[0]
		found := false
		for i := 0; i < len(root.Content); i += 2 {
			if root.Content[i].Value == "overrides" {
				found = true
			}
		}
		if !found {
			root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "overrides"}, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
		}
	}
	literal(&node)
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	e = encoder.Encode(&node)
	encoder.Close()
	return out.Bytes(), e
}
func literal(n *yaml.Node) {
	n.Style = 0
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" && strings.Contains(n.Value, "\n") {
		n.Style = yaml.LiteralStyle
	}
	for _, child := range n.Content {
		literal(child)
	}
}
func ZIP(p Package) ([]byte, error) {
	if !entityBound(p.Documents) {
		return nil, ErrPackage
	}
	files := map[string][]byte{}
	for _, d := range p.Documents {
		if d.Validate() != nil {
			return nil, ErrPackage
		}
		b, e := YAML(d)
		if e != nil {
			return nil, e
		}
		if _, ok := files[d.Path()]; ok {
			return nil, ErrPackage
		}
		files[d.Path()] = b
	}
	for name, body := range p.Assets {
		if !assetPath("presets/" + name) {
			return nil, ErrPackage
		}
		files["presets/"+name] = body
	}
	names := make([]string, 0, len(files))
	total := 0
	for name, body := range files {
		limit := MaxYAML
		if strings.HasPrefix(name, "presets/assets/") {
			limit = media.MaxBytes
		}
		if len(body) > limit || len(body) > MaxExpanded-total {
			return nil, ErrPackage
		}
		total += len(body)
		names = append(names, name)
	}
	sort.Strings(names)
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0600)
		entry, e := w.CreateHeader(header)
		if e != nil {
			return nil, e
		}
		if _, e = entry.Write(files[name]); e != nil {
			return nil, e
		}
	}
	if e := w.Close(); e != nil {
		return nil, e
	}
	if out.Len() > MaxUpload {
		return nil, fmt.Errorf("%w: export exceeds upload limit", ErrPackage)
	}
	return out.Bytes(), nil
}

func entityBound(documents []Document) bool {
	n := 0
	for _, d := range documents {
		if d.Kind == "Taxonomy" {
			var spec presets.TaxonomySpec
			if strict(d.Spec, &spec) != nil {
				return false
			}
			n += len(spec.Categories) + len(spec.Tags)
		} else {
			n++
		}
	}
	return len(documents) > 0 && n <= MaxEntities
}
