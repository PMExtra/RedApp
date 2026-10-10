package store

import (
	"encoding/json"
	"errors"
)

// Global settings are JSON objects owned by their typed callers (internal/site
// and internal/config). The schema creates every setting at revision 1 with an
// empty document, so callers keep their defaults for fields never saved.

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
	if err := s.read.QueryRow(`SELECT revision,payload FROM settings WHERE key=?`, key).Scan(&revision, &raw); err != nil {
		return 0, err
	}
	return revision, json.Unmarshal(raw, out)
}

// saveGlobalSetting replaces a setting when its stored revision still equals
// expected.
func (s *Store) saveGlobalSetting(key string, expected int64, value any) (int64, error) {
	if expected < 1 {
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
	result, err := s.db.Exec(`UPDATE settings SET revision=revision+1,payload=? WHERE key=? AND revision=?`, raw, key, expected)
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
