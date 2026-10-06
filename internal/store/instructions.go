package store

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"unicode"
	"unicode/utf8"
)

// Instructions is portable application configuration, separate from generated
// installer commands and source/cache identity. Rendering supports Markdown and trusted administrator-authored HTML documents.
type Instructions struct {
	LocalizedText
	Revision int64 `json:"revision"`
}

func (s *Store) Instructions(uid string) (Instructions, error) {
	var out Instructions
	err := s.DB.QueryRow(`SELECT en,zh_cn,revision FROM application_instructions WHERE app_uid=?`, uid).Scan(&out.En, &out.ZhCN, &out.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
		var key, provider string
		if e := s.DB.QueryRow(`SELECT v.id||'/'||a.id,a.provider FROM applications a JOIN vendors v ON v.uid=a.vendor_uid WHERE a.uid=?`, uid).Scan(&key, &provider); e == nil {
			if template, ok := BuiltinApplicationTemplate(key); ok && template.Application.Provider == provider {
				out.LocalizedText = template.Instructions
			}
		}
	}
	return out, err
}
func (s *Store) SaveInstructions(key string, expected int64, value LocalizedText) (Instructions, error) {
	for _, text := range []string{value.En, value.ZhCN} {
		if !utf8.ValidString(text) || utf8.RuneCountInString(text) > 12000 {
			return Instructions{}, ErrInvalidDirectory
		}
		for _, r := range text {
			if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
				return Instructions{}, ErrInvalidDirectory
			}
		}
	}
	if expected < 0 {
		return Instructions{}, ErrInvalidDirectory
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Instructions{}, err
	}
	defer tx.Rollback()
	app, err := readApplication(tx, key)
	if err != nil {
		return Instructions{}, err
	}
	if app.DeletedAt != nil {
		return Instructions{}, ErrDirectoryDeleted
	}
	var result sql.Result
	if expected == 0 {
		result, err = tx.Exec(`INSERT INTO application_instructions(app_uid,revision,en,zh_cn) VALUES(?,1,?,?) ON CONFLICT(app_uid) DO NOTHING`, app.UID, value.En, value.ZhCN)
	} else {
		result, err = tx.Exec(`UPDATE application_instructions SET revision=revision+1,en=?,zh_cn=? WHERE app_uid=? AND revision=?`, value.En, value.ZhCN, app.UID, expected)
	}
	if err != nil {
		return Instructions{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Instructions{}, err
	}
	if n != 1 {
		return Instructions{}, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return Instructions{}, err
	}
	return Instructions{LocalizedText: value, Revision: expected + 1}, nil
}

//go:embed entity_templates_v073.json
var instructionsV073 []byte

//go:embed entity_templates_v074.json
var instructionsV074 []byte

// Upgrade only exact previous defaults, independently by language. Empty and
// customized documents remain authoritative. Revision changes invalidate editors.
func upgradeBuiltinInstructions(db *sql.DB) error {
	var old []EntityTemplate
	for _, snapshot := range [][]byte{instructionsV073, instructionsV074} {
		var templates []EntityTemplate
		if err := json.Unmarshal(snapshot, &templates); err != nil {
			return err
		}
		old = append(old, templates...)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, previous := range old {
		key := previous.Vendor.ID + "/" + previous.Application.ID
		current, ok := BuiltinApplicationTemplate(key)
		if !ok {
			continue
		}
		_, err = tx.Exec(`UPDATE application_instructions SET
   en=CASE WHEN en=? THEN ? ELSE en END,
   zh_cn=CASE WHEN zh_cn=? THEN ? ELSE zh_cn END,
   revision=revision+1
   WHERE (en=? OR zh_cn=?) AND app_uid IN (
    SELECT a.uid FROM applications a JOIN vendors v ON v.uid=a.vendor_uid
    WHERE v.id||'/'||a.id=? AND a.provider=?)`,
			previous.Instructions.En, current.Instructions.En, previous.Instructions.ZhCN, current.Instructions.ZhCN,
			previous.Instructions.En, previous.Instructions.ZhCN, key, previous.Application.Provider)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
