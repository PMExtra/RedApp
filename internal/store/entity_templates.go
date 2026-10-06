package store

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
)

// Entity configuration is separate from the provider's immutable release protocol.
//
//go:embed entity_templates.json
var entityTemplatesJSON []byte

type EntityTemplate struct {
	Vendor       VendorInput      `json:"vendor"`
	Application  ApplicationInput `json:"application"`
	Instructions LocalizedText    `json:"instructions"`
}

func EntityTemplates() []EntityTemplate {
	var result []EntityTemplate
	if err := json.Unmarshal(entityTemplatesJSON, &result); err != nil {
		panic("Invalid embedded entity templates")
	}
	return result
}
func BuiltinApplicationTemplate(key string) (EntityTemplate, bool) {
	for _, v := range EntityTemplates() {
		if v.Vendor.ID+"/"+v.Application.ID == key {
			return v, true
		}
	}
	return EntityTemplate{}, false
}

var ErrBuiltinTemplate = errors.New("Applications matching a built-in template cannot be deleted; disable them instead")

func (s *Store) EnsureEntityTemplates() error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, template := range EntityTemplates() {
		v, e := readVendor(tx, template.Vendor.ID)
		if errors.Is(e, sql.ErrNoRows) {
			template.Vendor.Enabled = false
			v, e = createVendor(tx, template.Vendor)
		}
		if e != nil {
			return e
		}
		var exists int
		if e = tx.QueryRow(`SELECT count(*) FROM applications WHERE vendor_uid=? AND id=?`, v.UID, template.Application.ID).Scan(&exists); e != nil {
			return e
		}
		// A deleted legacy parent or any existing full-key record remains untouched.
		if exists != 0 || v.DeletedAt != nil {
			continue
		}
		template.Application.Enabled = false
		a, e := createApplication(tx, v.ID, template.Application)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(`INSERT INTO application_instructions VALUES(?,1,?,?)`, a.UID, template.Instructions.En, template.Instructions.ZhCN); e != nil {
			return e
		}
	}
	if _, err = tx.Exec(`UPDATE directory_state SET seeded=1 WHERE id=1`); err != nil {
		return err
	}
	return tx.Commit()
}

type TemplateReset struct {
	Revision             int64    `json:"revision"`
	InstructionsRevision int64    `json:"instructions_revision"`
	Groups               []string `json:"groups"`
}

func (s *Store) ResetApplicationTemplate(key string, in TemplateReset) (Application, error) {
	template, ok := BuiltinApplicationTemplate(key)
	if !ok {
		return Application{}, sql.ErrNoRows
	}
	if len(in.Groups) == 0 {
		return Application{}, ErrInvalidDirectory
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Application{}, err
	}
	defer tx.Rollback()
	a, err := readApplication(tx, key)
	if err != nil {
		return a, err
	}
	if a.Revision != in.Revision {
		return a, ErrConflict
	}
	changes := ApplicationChanges{Name: a.Name, Description: a.Description, Icon: a.Icon, BaseURL: a.BaseURL, BaseURLs: a.BaseURLs, SourceStrategy: a.SourceStrategy, CacheTTLSeconds: a.CacheTTLSeconds, Enabled: a.Enabled}
	var instructions Instructions
	err = tx.QueryRow(`SELECT revision,en,zh_cn FROM application_instructions WHERE app_uid=?`, a.UID).Scan(&instructions.Revision, &instructions.En, &instructions.ZhCN)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return a, err
	}
	if instructions.Revision == 0 && a.Provider == template.Application.Provider {
		instructions.LocalizedText = template.Instructions
	}
	writeInstructions := false
	seen := map[string]bool{}
	for _, group := range in.Groups {
		if seen[group] {
			return a, ErrInvalidDirectory
		}
		seen[group] = true
		switch group {
		case "metadata":
			changes.Name = template.Application.Name
			changes.Description = template.Application.Description
		case "icon":
			changes.Icon = template.Application.Icon
		case "instructions_en":
			instructions.En = template.Instructions.En
			writeInstructions = true
		case "instructions_zh":
			instructions.ZhCN = template.Instructions.ZhCN
			writeInstructions = true
		case "source":
			if a.Provider != template.Application.Provider {
				return a, ErrInvalidDirectory
			}
			changes.BaseURL = template.Application.BaseURL
			changes.BaseURLs = template.Application.BaseURLs
			changes.SourceStrategy = template.Application.SourceStrategy
		case "cache":
			if a.Provider != template.Application.Provider {
				return a, ErrInvalidDirectory
			}
			changes.CacheTTLSeconds = template.Application.CacheTTLSeconds
		default:
			return a, ErrInvalidDirectory
		}
	}
	if writeInstructions {
		if instructions.Revision != in.InstructionsRevision {
			return a, ErrConflict
		}
		if _, err = tx.Exec(`INSERT INTO application_instructions(app_uid,revision,en,zh_cn) VALUES(?,?,?,?) ON CONFLICT(app_uid) DO UPDATE SET revision=excluded.revision,en=excluded.en,zh_cn=excluded.zh_cn`, a.UID, instructions.Revision+1, instructions.En, instructions.ZhCN); err != nil {
			return a, err
		}
	}
	a, err = updateApplication(tx, key, in.Revision, changes)
	if err != nil {
		return a, err
	}
	return a, tx.Commit()
}

func BuiltinVendorTemplate(id string) (VendorInput, bool) {
	for _, value := range EntityTemplates() {
		if value.Vendor.ID == id {
			return value.Vendor, true
		}
	}
	return VendorInput{}, false
}
func (s *Store) ResetVendorTemplate(id string, in TemplateReset) (Vendor, error) {
	template, ok := BuiltinVendorTemplate(id)
	if !ok {
		return Vendor{}, sql.ErrNoRows
	}
	if len(in.Groups) == 0 {
		return Vendor{}, ErrInvalidDirectory
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Vendor{}, err
	}
	defer tx.Rollback()
	v, err := readVendor(tx, id)
	if err != nil {
		return v, err
	}
	changes := VendorChanges{Name: v.Name, Description: v.Description, Icon: v.Icon, LocalizedIcons: v.LocalizedIcons, Enabled: v.Enabled}
	seen := map[string]bool{}
	for _, group := range in.Groups {
		if seen[group] {
			return v, ErrInvalidDirectory
		}
		seen[group] = true
		switch group {
		case "metadata":
			changes.Name = template.Name
			changes.Description = template.Description
		case "icon":
			changes.Icon = template.Icon
			changes.LocalizedIcons = template.LocalizedIcons
		default:
			return v, ErrInvalidDirectory
		}
	}
	v, err = updateVendor(tx, id, in.Revision, changes)
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}
