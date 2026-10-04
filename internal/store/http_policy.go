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
	if a.Provider != "general-http" {
		return a, fmt.Errorf("%w: HTTP policy requires the GeneralHttp provider", ErrInvalidDirectory)
	}
	return a, nil
}

// ReadHTTPPolicy returns a complete empty policy when none has been saved. Its
// revision always belongs to the application, including unrelated directory edits.
func (s *Store) ReadHTTPPolicy(key string) (cachepolicy.Config, int64, error) {
	config := cachepolicy.Empty()
	tx, err := s.DB.Begin()
	if err != nil {
		return config, 0, err
	}
	defer tx.Rollback()
	app, err := policyApplication(tx, key, true)
	if err != nil {
		return config, app.Revision, err
	}
	var raw []byte
	err = tx.QueryRow(`SELECT payload FROM settings WHERE scope='app' AND app_id=? AND key='http_policy'`, app.MetricsID()).Scan(&raw)
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
func (s *Store) SaveHTTPPolicy(key string, expectedAppRevision int64, config cachepolicy.Config) (Application, error) {
	config, err := cachepolicy.Normalize(config)
	if err != nil {
		return Application{}, err
	}
	for i := range config.Rules {
		if config.Rules[i].ID == "" {
			config.Rules[i].ID, err = identity.NewUID()
			if err != nil {
				return Application{}, err
			}
		}
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return Application{}, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Application{}, err
	}
	defer tx.Rollback()
	app, err := policyApplication(tx, key, false)
	if err != nil {
		return Application{}, err
	}
	if app.Revision != expectedAppRevision {
		return Application{}, ErrConflict
	}
	if _, err = tx.Exec(`INSERT INTO settings(scope,app_id,key,revision,payload) VALUES('app',?,'http_policy',?,?) ON CONFLICT(scope,app_id,key) DO UPDATE SET revision=excluded.revision,payload=excluded.payload`, app.MetricsID(), app.Revision+1, raw); err != nil {
		return Application{}, err
	}
	result, err := tx.Exec(`UPDATE applications SET revision=revision+1 WHERE uid=? AND revision=? AND deleted_at_s IS NULL`, app.UID, expectedAppRevision)
	if err = affected(result, err); err != nil {
		return Application{}, err
	}
	app.Revision++
	if err = tx.Commit(); err != nil {
		return Application{}, err
	}
	return app, nil
}
