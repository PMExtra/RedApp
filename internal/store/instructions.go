package store

import (
	"database/sql"
	"encoding/json"
)

type Instructions struct {
	LocalizedText
	Revision int64 `json:"revision"`
}

func (s *Store) Instructions(uid string) (Instructions, error) {
	var out Instructions
	err := s.read.QueryRow(`SELECT en,zh_cn,revision FROM application_instructions WHERE app_uid=?`, uid).Scan(&out.En, &out.ZhCN, &out.Revision)
	if err == sql.ErrNoRows {
		err = nil
	}
	return out, err
}
func (s *Store) SaveInstructions(key string, expected int64, value LocalizedText) (Instructions, error) {
	if err := validateInstructions(value); err != nil {
		return Instructions{}, err
	}
	a, err := s.Application(key)
	if err != nil {
		return Instructions{}, err
	}
	patch := ConfigurationPatch{Revision: a.Revision, Set: map[string]json.RawMessage{"instructions.en": encode(value.En), "instructions.zh-CN": encode(value.ZhCN)}}
	if err = s.patchConfiguration("App", key, patch, nil, &expected); err != nil {
		return Instructions{}, err
	}
	return s.Instructions(a.UID)
}
