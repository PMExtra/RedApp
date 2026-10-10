package store

import (
	"database/sql"
	"errors"
	"unicode"
	"unicode/utf8"
)

// AdminNotes is a private payload. It is deliberately absent from Vendor,
// Application, template and public catalog structs.
type AdminNotes struct {
	Text     string `json:"text"`
	Revision int64  `json:"revision"`
}

func notesEntity(q directoryQuerier, kind, key string) (table, uid string, deleted bool, err error) {
	switch kind {
	case "vendor":
		var v Vendor
		v, err = readVendor(q, key)
		return "vendor_admin_notes", v.UID, v.DeletedAt != nil, err
	case "app":
		var a Application
		a, err = readApplication(q, key)
		return "application_admin_notes", a.UID, a.DeletedAt != nil, err
	default:
		return "", "", false, ErrInvalidDirectory
	}
}
func (s *Store) AdminNotes(kind, key string) (AdminNotes, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return AdminNotes{}, err
	}
	defer tx.Rollback()
	table, uid, _, err := notesEntity(tx, kind, key)
	if err != nil {
		return AdminNotes{}, err
	}
	var value AdminNotes
	err = tx.QueryRow(`SELECT text,revision FROM `+table+` WHERE entity_uid=?`, uid).Scan(&value.Text, &value.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return value, err
}
func (s *Store) SaveAdminNotes(kind, key string, expected int64, text string) (AdminNotes, error) {
	if expected < 0 || !utf8.ValidString(text) || utf8.RuneCountInString(text) > 12000 {
		return AdminNotes{}, ErrInvalidDirectory
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return AdminNotes{}, ErrInvalidDirectory
		}
	}
	// Notes are part of the configuration CAS state; serialize with its writers.
	s.configMu.Lock()
	defer s.configMu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return AdminNotes{}, err
	}
	defer tx.Rollback()
	table, uid, deleted, err := notesEntity(tx, kind, key)
	if err != nil {
		return AdminNotes{}, err
	}
	if deleted {
		return AdminNotes{}, ErrDirectoryDeleted
	}
	var result sql.Result
	if expected == 0 {
		result, err = tx.Exec(`INSERT INTO `+table+`(entity_uid,revision,text) VALUES(?,1,?) ON CONFLICT(entity_uid) DO NOTHING`, uid, text)
	} else {
		result, err = tx.Exec(`UPDATE `+table+` SET text=?,revision=revision+1 WHERE entity_uid=? AND revision=?`, text, uid, expected)
	}
	if err != nil {
		return AdminNotes{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return AdminNotes{}, err
	}
	if n != 1 {
		return AdminNotes{}, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return AdminNotes{}, err
	}
	return AdminNotes{Text: text, Revision: expected + 1}, nil
}
