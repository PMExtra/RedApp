package store

import (
	"database/sql"
	"errors"
	"unicode"
	"unicode/utf8"
)

// Instructions is portable application configuration, separate from generated
// installer commands and source/cache identity. Text is rendered as plain text.
type Instructions struct {
	LocalizedText
	Revision int64 `json:"revision"`
}

func (s *Store) Instructions(uid string) (Instructions, error) {
	var out Instructions
	err := s.DB.QueryRow(`SELECT en,zh_cn,revision FROM application_instructions WHERE app_uid=?`, uid).Scan(&out.En, &out.ZhCN, &out.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
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
