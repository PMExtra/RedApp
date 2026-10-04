package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

var ErrRevisionConflict = ErrConflict

func validSetting(scope, app, key string) bool {
	if scope == "global" && app == "" {
		return key == "site" || key == "upstream_proxy" || key == "public_url"
	}
	return scope == "app" && ValidAppID(app) && (key == "channel_ttl" || key == "http_policy")
}

// ReadSetting is limited to schema-owned setting kinds. Callers use
// their typed settings structs; missing settings have revision zero.
func (s *Store) ReadSetting(scope, app, key string, out any) (int64, error) {
	if !validSetting(scope, app, key) {
		return 0, errors.New("Unknown setting scope or key")
	}
	var raw []byte
	var rev int64
	err := s.DB.QueryRow("SELECT revision,payload FROM settings WHERE scope=? AND app_id=? AND key=?", scope, app, key).Scan(&rev, &raw)
	if err != nil {
		return 0, err
	}
	return rev, json.Unmarshal(raw, out)
}
func (s *Store) CompareAndSwapSetting(scope, app, key string, expected int64, payload any) (int64, error) {
	if key == "http_policy" {
		return 0, errors.New("HTTP policy writes require application revision CAS through SaveHTTPPolicy")
	}
	if !validSetting(scope, app, key) || expected < 0 {
		return 0, errors.New("Invalid setting scope, key or revision")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	if key == "channel_ttl" {
		var n int
		if json.Unmarshal(raw, &n) != nil || n < 1 || n > 86400 {
			return 0, errors.New("Channel TTL must be between 1 and 86400 seconds")
		}
	} else {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil || obj == nil {
			return 0, errors.New("Settings require a typed JSON object")
		}
	}
	var result sql.Result
	if expected == 0 {
		result, err = s.DB.Exec("INSERT INTO settings(scope,app_id,key,revision,payload) VALUES(?,?,?,1,?) ON CONFLICT(scope,app_id,key) DO NOTHING", scope, app, key, raw)
	} else {
		result, err = s.DB.Exec("UPDATE settings SET revision=revision+1,payload=? WHERE scope=? AND app_id=? AND key=? AND revision=?", raw, scope, app, key, expected)
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
func (s *Store) ChannelTTL(app string) (int, int64, error) {
	var seconds int
	revision, err := s.ReadSetting("app", app, "channel_ttl", &seconds)
	return seconds, revision, err
}
func (s *Store) SetChannelTTL(app string, expected int64, seconds int) (int64, error) {
	return s.CompareAndSwapSetting("app", app, "channel_ttl", expected, seconds)
}
