package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"

	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/identity"
)

func policyApplication(q directoryQuerier, key string, allowDeleted bool) (Application, error) {
	a, err := readApplication(q, key)
	if err != nil {
		return a, err
	}
	if a.DeletedAt != nil && !allowDeleted {
		return a, ErrDirectoryDeleted
	}
	if a.Provider != "http-cache" {
		return a, invalidf("HTTP policy requires the GeneralHttp provider")
	}
	return a, nil
}

// ReadHTTPPolicy returns a complete empty policy when none has been saved. Its
// revision always belongs to the application, including unrelated directory edits.
func (s *Store) ReadHTTPPolicy(key string) (cachepolicy.Config, int64, error) {
	config := cachepolicy.Empty()
	tx, err := s.read.Begin()
	if err != nil {
		return config, 0, err
	}
	defer tx.Rollback()
	app, err := policyApplication(tx, key, true)
	if err != nil {
		return config, app.Revision, err
	}
	var raw []byte
	err = tx.QueryRow(`SELECT payload FROM application_http_policies WHERE app_uid=?`, app.UID).Scan(&raw)
	if err == sql.ErrNoRows {
		return config, app.Revision, tx.Commit()
	}
	if err != nil {
		return config, app.Revision, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&config); err != nil {
		return cachepolicy.Empty(), app.Revision, err
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return cachepolicy.Empty(), app.Revision, fmt.Errorf("Invalid stored HTTP policy trailing data")
	}
	if config, err = cachepolicy.Normalize(config); err != nil {
		return cachepolicy.Empty(), app.Revision, err
	}
	if config.Rules == nil {
		config.Rules = []cachepolicy.CacheRule{}
	}
	if config.AutoCleanup == nil {
		config.AutoCleanup = []cachepolicy.CleanupRule{}
	}
	return config, app.Revision, tx.Commit()
}

// SaveHTTPPolicy shares directory CAS and changes no source epoch. Configuration
// and the admission revision commit together so older background work is fenced.
func (s *Store) SaveHTTPPolicy(key string, revision int64, config cachepolicy.Config) (Application, error) {
	config, err := cachepolicy.Normalize(config)
	if err != nil {
		return Application{}, err
	}
	a, err := s.Application(key)
	if err != nil {
		return Application{}, err
	}
	if a.Provider != "http-cache" {
		return Application{}, ErrInvalidDirectory
	}
	for i := range config.Rules {
		if config.Rules[i].ID == "" {
			config.Rules[i].ID, err = identity.NewUID()
			if err != nil {
				return Application{}, err
			}
		}
	}
	patch := ConfigurationPatch{Revision: revision, Set: map[string]json.RawMessage{"http_policy.rules": encode(config.Rules), "http_policy.auto_cleanup": encode(config.AutoCleanup), "http_policy.stale_fallback": encode(config.StaleFallback)}}
	if err = s.patchConfiguration("App", key, patch, nil, nil); err != nil {
		return Application{}, err
	}
	return s.Application(key)
}
