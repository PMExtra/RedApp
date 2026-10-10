package httpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"strings"

	"github.com/PMExtra/RedApp/internal/configexchange"
	"github.com/PMExtra/RedApp/internal/media"
	"github.com/PMExtra/RedApp/presets"
)

// Controlled images of configuration packages: exported icons become
// assets/<sha256>.<ext> entries; imported ones are validated in isolation.

type exchangeIconField struct {
	value string
	set   func(string)
}

func iconFields(d *configexchange.Document) []exchangeIconField {
	object := d.Spec
	if d.Template != nil {
		object = d.Overrides
	}
	if object == nil {
		return nil
	}
	out := []exchangeIconField{}
	if value, ok := object["icon"].(string); ok {
		out = append(out, exchangeIconField{value, func(v string) { object["icon"] = v }})
	}
	if icons, ok := object["localized_icons"].(map[string]any); ok {
		for _, lang := range []string{"en", "zh-CN"} {
			locale := lang
			if value, ok := icons[lang].(string); ok {
				out = append(out, exchangeIconField{value, func(v string) { icons[locale] = v }})
			}
		}
	}
	return out
}
func (s *Server) exportImages(p *configexchange.Package) error {
	for i := range p.Documents {
		for _, field := range iconFields(&p.Documents[i]) {
			if field.value == "" {
				continue
			}
			var body []byte
			var extension string
			if strings.HasPrefix(field.value, media.PublicPrefix) {
				file, mime, e := s.icons.Open(field.value)
				if e != nil {
					return e
				}
				body, e = io.ReadAll(io.LimitReader(file, media.MaxStoredBytes+1))
				file.Close()
				if e != nil {
					return e
				}
				switch mime {
				case "image/png":
					extension = ".png"
				case "image/jpeg":
					extension = ".jpg"
				case "image/svg+xml":
					extension = ".svg"
				default:
					return media.ErrInvalidIcon
				}
			} else {
				image, ok := presets.Embedded().Image(field.value)
				if !ok {
					return media.ErrInvalidIcon
				}
				body = image.Body
				extension = path.Ext(field.value)
			}
			if _, e := media.ValidateStaticImage(body, extension); e != nil {
				return e
			}
			hash := sha256.Sum256(body)
			name := "assets/" + hex.EncodeToString(hash[:]) + extension
			p.Assets[name] = body
			field.set(name)
		}
	}
	return nil
}
func (s *Server) prepareImportImages(p *configexchange.Package) (map[string][]byte, error) {
	// Validation writes only into an isolated directory, never into published icon storage.
	temp, e := os.MkdirTemp(s.dataDir, ".configuration-images-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(temp)
	staged, e := media.New(temp)
	if e != nil {
		return nil, e
	}
	defer staged.Close()
	images := map[string][]byte{}
	used := map[string]bool{}
	for i := range p.Documents {
		for _, field := range iconFields(&p.Documents[i]) {
			if field.value == "" {
				continue
			}
			if strings.HasPrefix(field.value, "assets/") {
				raw, ok := p.Assets[field.value]
				if !ok {
					return nil, media.ErrInvalidIcon
				}
				public, e := staged.PutStatic(raw, path.Ext(field.value))
				if e != nil {
					return nil, e
				}
				used[field.value] = true
				images[public] = raw
				field.set(public)
			} else if s.validateIcon("icon", field.value) != nil {
				return nil, media.ErrInvalidIcon
			}
		}
	}
	for name := range p.Assets {
		if !used[name] {
			return nil, media.ErrInvalidIcon
		}
	}
	return images, nil
}
