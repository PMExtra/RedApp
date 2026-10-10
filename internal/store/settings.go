package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// Global settings are JSON objects owned by their typed callers (internal/site
// and internal/config). A missing setting reads as revision zero and leaves the
// destination unchanged, so callers keep their defaults.

func (s *Store) ReadSiteSettings(out any) (int64, error) { return s.readGlobalSetting("site", out) }
func (s *Store) SaveSiteSettings(expected int64, value any) (int64, error) {
	return s.saveGlobalSetting("site", expected, value)
}
func (s *Store) ReadPublicURLSetting(out any) (int64, error) {
	return s.readGlobalSetting("public_url", out)
}
func (s *Store) SavePublicURLSetting(expected int64, value any) (int64, error) {
	return s.saveGlobalSetting("public_url", expected, value)
}

func (s *Store) readGlobalSetting(key string, out any) (int64, error) {
	var raw []byte
	var revision int64
	err := s.DB.QueryRow(`SELECT revision,payload FROM settings WHERE scope='global' AND app_id='' AND key=?`, key).Scan(&revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return revision, json.Unmarshal(raw, out)
}

// saveGlobalSetting replaces a setting when its stored revision still equals
// expected; zero means the setting must not exist yet.
func (s *Store) saveGlobalSetting(key string, expected int64, value any) (int64, error) {
	if expected < 0 {
		return 0, errors.New("Invalid setting revision")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return 0, errors.New("Settings require a typed JSON object")
	}
	var result sql.Result
	if expected == 0 {
		result, err = s.DB.Exec(`INSERT INTO settings(scope,app_id,key,revision,payload) VALUES('global','',?,1,?) ON CONFLICT(scope,app_id,key) DO NOTHING`, key, raw)
	} else {
		result, err = s.DB.Exec(`UPDATE settings SET revision=revision+1,payload=? WHERE scope='global' AND app_id='' AND key=? AND revision=?`, raw, key, expected)
	}
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n != 1 {
		return 0, ErrConflict
	}
	return expected + 1, nil
}
