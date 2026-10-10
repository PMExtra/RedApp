package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/presets"
	"time"
)

type RetentionChannel struct {
	Version   string    `json:"version"`
	FetchedAt time.Time `json:"fetched_at"`
	ExpiresAt time.Time `json:"expires_at"`
}
type RetentionVersion struct {
	Version  string   `json:"version"`
	Reasons  []string `json:"reasons"`
	Bytes    int64    `json:"bytes"`
	Selected bool     `json:"selected"`
}
type RetentionGuard struct {
	SourceFence
	Automatic bool                        `json:"automatic"`
	Hash      string                      `json:"hash"`
	Channels  map[string]RetentionChannel `json:"channels"`
	Versions  []RetentionVersion          `json:"versions"`
}
type RetentionReceipt struct {
	Selection       []CleanupSelection `json:"selection"`
	Skipped         map[string]string  `json:"skipped"`
	RetiredVersions int                `json:"retired_versions"`
	LogicalBytes    int64              `json:"logical_bytes"`
}

func RetentionHash(r presets.Retention) string {
	raw, _ := json.Marshal(r)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func (s *Store) Retention(key string) (presets.Retention, string, error) {
	c, err := s.ApplicationConfiguration(key)
	if err != nil {
		return presets.Retention{}, "", err
	}
	raw, ok := c.Effective["retention"]
	if !ok {
		return presets.Retention{}, "", ErrInvalidDirectory
	}
	var r presets.Retention
	if err = strict(object(raw), &r); err != nil {
		return r, "", err
	}
	return r, RetentionHash(r), r.Validate()
}
func checkRetention(tx *sql.Tx, app string, guard *RetentionGuard, at time.Time) error {
	if guard == nil {
		return ErrConflict
	}
	uid, _, ok := identity.ParseStorageID(app)
	if !ok {
		return ErrInvalidDirectory
	}
	w := newConfigSet(tx)
	entry, err := w.app(uid)
	if err != nil {
		return err
	}
	if entry == nil {
		return ErrSourceInactive
	}
	effective, err := w.effective("App", entry.config)
	if err != nil {
		return err
	}
	var r presets.Retention
	if err = strict(object(effective["retention"]), &r); err != nil {
		return err
	}
	if guard.Automatic && !r.Enabled {
		return ErrConflict
	}
	if RetentionHash(r) != guard.Hash {
		return ErrConflict
	}
	// Active current epoch is required in addition to the preview's runtime fence.
	var source SourceRecord
	source, err = scanSource(tx.QueryRow(`SELECT `+sourceColumns+sourceJoin+` WHERE src.app_uid=? AND src.epoch=(SELECT source_epoch FROM applications WHERE uid=?)`, uid, uid))
	if err != nil {
		return err
	}
	if source.StorageID() != app || !source.Active {
		return ErrSourceInactive
	}
	var spec presets.AppSpec
	if err = strict(effective, &spec); err != nil {
		return err
	}
	for name, frozen := range guard.Channels {
		var version string
		var fetched, expires int64
		if err = tx.QueryRow(`SELECT version,fetched_at_s,expires_at_s FROM channels WHERE app_id=? AND channel=?`, app, name).Scan(&version, &fetched, &expires); err != nil {
			return err
		}
		expiry := time.Unix(expires, 0)
		ttlExpiry := time.Unix(fetched, 0).Add(time.Duration(spec.CacheTTLSeconds) * time.Second)
		if ttlExpiry.Before(expiry) {
			expiry = ttlExpiry
		}
		if version != frozen.Version || fetched != frozen.FetchedAt.Unix() || expires != frozen.ExpiresAt.Unix() || time.Unix(fetched, 0).After(at) || !at.Before(expiry) {
			return ErrConflict
		}
	}
	return nil
}
func (s *Store) RetentionStatus(uid string) (json.RawMessage, error) {
	var raw []byte
	err := s.DB.QueryRow(`SELECT payload FROM retention_status WHERE app_uid=?`, uid).Scan(&raw)
	if err == sql.ErrNoRows {
		return json.RawMessage(`{}`), nil
	}
	return raw, err
}
func (s *Store) SaveRetentionStatus(uid string, raw json.RawMessage) error {
	if !identity.ValidUID(uid) || !json.Valid(raw) {
		return ErrInvalidDirectory
	}
	_, err := s.DB.Exec(`INSERT INTO retention_status(app_uid,payload) VALUES(?,?) ON CONFLICT(app_uid) DO UPDATE SET payload=excluded.payload`, uid, []byte(raw))
	return err
}
