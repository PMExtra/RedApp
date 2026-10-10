package store

import (
	"database/sql"
	"errors"
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
	value := AdminNotes{Revision: 1}
	err = tx.QueryRow(`SELECT text,revision FROM `+table+` WHERE entity_uid=?`, uid).Scan(&value.Text, &value.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return value, err
}

// SaveAdminNotes replaces the notes under CAS. Notes that were never saved
// have revision 1, so the first save expects 1 and stores revision 2.
func (s *Store) SaveAdminNotes(kind, key string, expected int64, text string) (AdminNotes, error) {
	if !validNotes(text) {
		return AdminNotes{}, invalidf("notes must be at most 12000 characters without control characters other than newline, CR and tab")
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
	current := AdminNotes{}
	err = tx.QueryRow(`SELECT revision FROM `+table+` WHERE entity_uid=?`, uid).Scan(&current.Revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AdminNotes{}, err
	}
	if current.current() != expected {
		return AdminNotes{}, ErrConflict
	}
	saved := AdminNotes{Text: text, Revision: expected + 1}
	if _, err = tx.Exec(`INSERT INTO `+table+`(entity_uid,revision,text) VALUES(?,?,?) ON CONFLICT(entity_uid) DO UPDATE SET text=excluded.text,revision=excluded.revision`, uid, saved.Revision, text); err != nil {
		return AdminNotes{}, err
	}
	if err = tx.Commit(); err != nil {
		return AdminNotes{}, err
	}
	return saved, nil
}

// current is the revision clients see: notes that were never saved have
// revision 1.
func (n AdminNotes) current() int64 { return max(n.Revision, 1) }
