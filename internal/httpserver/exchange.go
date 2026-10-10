package httpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/PMExtra/RedApp/internal/auth"
	"github.com/PMExtra/RedApp/internal/configexchange"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/media"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/presets"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

type exchangePreview struct {
	plan            store.ImportPlan
	session, digest string
	until           time.Time
	images          map[string][]byte
}

func exchangeOwner(session auth.Session) string {
	digest := sha256.Sum256([]byte(session.CSRF))
	return hex.EncodeToString(digest[:])
}
func (s *Server) exchangeAPI(w http.ResponseWriter, r *http.Request, session auth.Session) bool {
	endpoint := strings.TrimPrefix(r.URL.Path, "/admin/api/")
	if strings.HasPrefix(endpoint, "apps/") && strings.HasSuffix(endpoint, "/copy") {
		key := strings.TrimSuffix(strings.TrimPrefix(endpoint, "apps/"), "/copy")
		if r.Method != http.MethodPost || !identity.ValidKey(key) || !queryAllowed(r) {
			fail(w, 400, "Invalid copy request")
			return true
		}
		var input store.CopyApplicationInput
		if decodeLimit(w, r, &input, 64<<10) != nil {
			fail(w, 400, "Invalid copy input")
			return true
		}
		s.directoryMu.Lock()
		defer s.directoryMu.Unlock()
		app, e := s.DB.CopyApplication(key, input)
		if e != nil {
			directoryError(w, e)
		} else {
			reply(w, 201, map[string]any{"app": app})
		}
		return true
	}
	if !strings.HasPrefix(endpoint, "configuration/") {
		return false
	}
	if r.Method != http.MethodPost || !queryAllowed(r) {
		fail(w, 400, "Invalid configuration exchange request")
		return true
	}
	s.exchangeMu.Lock()
	defer s.exchangeMu.Unlock()
	owner := exchangeOwner(session)
	if endpoint == "configuration/export" {
		var options store.ExportOptions
		if decodeLimit(w, r, &options, 256<<10) != nil {
			fail(w, 400, "Invalid export selection")
			return true
		}
		p, e := s.DB.ExportConfiguration(options)
		if e == nil {
			e = s.exportImages(&p)
		}
		var body []byte
		if e == nil {
			body, e = configexchange.ZIP(p)
		}
		if e != nil {
			fail(w, 400, "Configuration export unavailable; check selection and controlled images")
			return true
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="redapp-configuration.zip"`)
		w.WriteHeader(200)
		w.Write(body)
		return true
	}
	now := time.Now()
	if s.exchangePreviews == nil {
		s.exchangePreviews = map[string]exchangePreview{}
	}
	for id, p := range s.exchangePreviews {
		if !p.until.After(now) {
			delete(s.exchangePreviews, id)
		}
	}
	if endpoint == "configuration/import/preview" {
		if len(s.exchangePreviews) >= 8 {
			problem(w, 429, "PREVIEW_LIMIT_EXCEEDED", "Too many active configuration previews; wait for expiry")
			return true
		}
		raw, choices, e := readExchangeUpload(w, r)
		if e != nil {
			fail(w, 400, "Invalid or oversized configuration upload")
			return true
		}
		p, e := configexchange.Parse(raw)
		if e != nil {
			fail(w, 400, "Invalid configuration package")
			return true
		}
		images, e := s.prepareImportImages(&p)
		if e != nil {
			fail(w, 400, "Invalid controlled configuration images")
			return true
		}
		plan, e := s.DB.PreviewConfigurationImport(p.Documents, choices)
		if e != nil {
			directoryErrorSafe(w, e)
			return true
		}
		id, e := identity.NewUID()
		if e != nil {
			fail(w, 503, "Unable to create configuration preview")
			return true
		}
		hash := sha256.Sum256(raw)
		digest := hex.EncodeToString(hash[:])
		until := now.Add(10 * time.Minute)
		s.exchangePreviews[id] = exchangePreview{plan: plan, session: owner, digest: digest, until: until, images: images}
		reply(w, 200, map[string]any{"id": id, "digest": digest, "expires_at": until, "preview": redactImportPlan(plan)})
		return true
	}
	if strings.HasPrefix(endpoint, "configuration/import/") && strings.HasSuffix(endpoint, "/execute") {
		id := strings.TrimSuffix(strings.TrimPrefix(endpoint, "configuration/import/"), "/execute")
		if !identity.ValidUID(id) {
			fail(w, 400, "Invalid configuration preview")
			return true
		}
		var input struct {
			Confirm           bool `json:"confirm"`
			TrustInstructions bool `json:"trust_instructions"`
		}
		if decode(w, r, &input) != nil || !input.Confirm {
			fail(w, 400, "Explicit configuration confirmation required")
			return true
		}
		if result, found, e := s.DB.ImportReceipt(id); e != nil {
			directoryErrorSafe(w, e)
			return true
		} else if found {
			reply(w, 200, result)
			return true
		}
		preview, ok := s.exchangePreviews[id]
		if !ok || preview.session != owner || !preview.until.After(now) {
			fail(w, 409, "Configuration preview expired or belongs to another session")
			return true
		}
		var result store.ImportResult
		s.directoryMu.Lock()
		needed := map[string][]byte{}
		for path, body := range preview.images {
			if preview.plan.ReferencesIcon(path) {
				needed[path] = body
			}
		}
		e := s.Icons.ApplyImages(needed, s.DB.IconReferenced, func() error {
			var e error
			result, e = s.DB.ExecuteConfigurationImport(preview.plan, id, input.TrustInstructions, func() bool { active, ok := s.Auth.Session(r); return ok && exchangeOwner(active) == owner })
			return e
		})
		s.directoryMu.Unlock()
		if e != nil {
			directoryErrorSafe(w, e)
		} else {
			delete(s.exchangePreviews, id)
			reply(w, 200, result)
		}
		return true
	}
	fail(w, 404, "Configuration exchange endpoint not found")
	return true
}

// Do not echo validation errors that may contain an administrator's private URL/text.
func directoryErrorSafe(w http.ResponseWriter, e error) {
	if errors.Is(e, store.ErrExpired) {
		fail(w, 409, "Configuration preview or receipt expired")
		return
	}
	if errors.Is(e, store.ErrConflict) || errors.Is(e, store.ErrDirectoryExists) || errors.Is(e, store.ErrDirectoryDeleted) {
		directoryError(w, e)
		return
	}
	fail(w, 400, "Invalid configuration, unavailable template or unresolved import choice; use an independent copy when the template is unavailable")
}
func readExchangeUpload(w http.ResponseWriter, r *http.Request) ([]byte, []store.ImportChoice, error) {
	r.Body = http.MaxBytesReader(w, r.Body, configexchange.MaxUpload+(1<<20))
	reader, e := r.MultipartReader()
	if e != nil {
		return nil, nil, e
	}
	var raw []byte
	var choices []store.ImportChoice
	seen := map[string]bool{}
	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, nil, e
		}
		name := part.FormName()
		if seen[name] || name != "file" && name != "choices" {
			part.Close()
			return nil, nil, configexchange.ErrPackage
		}
		seen[name] = true
		limit := configexchange.MaxUpload
		if name == "choices" {
			limit = 1 << 20
		}
		body, e := io.ReadAll(io.LimitReader(part, int64(limit)+1))
		part.Close()
		if e != nil || len(body) > limit {
			return nil, nil, configexchange.ErrPackage
		}
		if name == "file" {
			raw = body
		} else {
			d := json.NewDecoder(bytes.NewReader(body))
			d.DisallowUnknownFields()
			if !utf8.Valid(body) || d.Decode(&choices) != nil || d.Decode(new(any)) != io.EOF {
				return nil, nil, configexchange.ErrPackage
			}
		}
	}
	if len(raw) == 0 {
		return nil, nil, configexchange.ErrPackage
	}
	return raw, choices, nil
}

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
				file, mime, e := s.Icons.Open(field.value)
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
	temp, e := os.MkdirTemp(s.Dir, ".configuration-images-")
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
			} else if s.validateDirectoryIcon(field.value) != nil {
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
