package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/PMExtra/RedApp/internal/identity"
	"strings"
	"time"
)

type CleanupSelection struct {
	GenerationID  string `json:"generation_id"`
	Version       string `json:"version"`
	ResourceKey   string `json:"resource_key"`
	SnapshotBytes int64  `json:"snapshot_bytes"`
}
type CleanupPreview struct {
	SourceFence
	Retention            *RetentionGuard
	ID, AppID            string
	CreatedAt, ExpiresAt time.Time
	Selection            []CleanupSelection
	// Frozen estimates shown with the preview and its receipt.
	ReclaimableBytes  int64
	ActiveGenerations int
	UnknownVersions   []string
	ExecutedAt        *time.Time
	Result            json.RawMessage
}

func (s *Store) SaveCleanupPreview(p CleanupPreview) error {
	if requireApp(p.AppID) != nil || p.ID == "" || p.CreatedAt.IsZero() || !p.ExpiresAt.After(p.CreatedAt) || p.ExpiresAt.Sub(p.CreatedAt) > 10*time.Minute || p.ExecutedAt != nil || p.Result != nil {
		return errors.New("Invalid cleanup preview")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkCleanupFence(tx, p.AppID, p.SourceFence); err != nil {
		return err
	}
	if p.Retention != nil {
		if err = checkRetention(tx, p.AppID, p.Retention, p.CreatedAt); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, item := range p.Selection {
		if item.GenerationID == "" || seen[item.GenerationID] || item.SnapshotBytes < 0 {
			return errors.New("Invalid cleanup selection")
		}
		seen[item.GenerationID] = true
		var app, v, k string
		var current bool
		if err = tx.QueryRow("SELECT app_id,version,resource_key,is_current FROM generations WHERE id=?", item.GenerationID).Scan(&app, &v, &k, &current); err != nil {
			return err
		}
		if app != p.AppID || v != item.Version || k != item.ResourceKey || !current {
			return errors.New("Cleanup selection ownership changed")
		}
	}
	raw, err := json.Marshal(p.Selection)
	if err != nil {
		return err
	}
	if p.ReclaimableBytes < 0 || p.ActiveGenerations < 0 {
		return errors.New("invalid cleanup preview estimate")
	}
	if p.UnknownVersions == nil {
		p.UnknownVersions = []string{}
	}
	unknown, err := json.Marshal(p.UnknownVersions)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO cleanup_previews(id,app_id,created_at_s,expires_at_s,selection_json,reclaimable_bytes,active_generations,unknown_versions_json,app_revision,vendor_revision,retention_json) VALUES(?,?,?,?,?,?,?,?,?,?,?)", p.ID, p.AppID, p.CreatedAt.Unix(), p.ExpiresAt.Unix(), raw, p.ReclaimableBytes, p.ActiveGenerations, unknown, p.AppRuntimeRevision, p.VendorRuntimeRevision, optionalRetention(p.Retention))
	if err != nil {
		return err
	}
	return tx.Commit()
}

const cleanupColumns = `id,app_id,created_at_s,expires_at_s,selection_json,reclaimable_bytes,active_generations,unknown_versions_json,executed_at_s,result_json,app_revision,vendor_revision,retention_json`

func scanCleanup(row scanner) (CleanupPreview, error) {
	var p CleanupPreview
	var created, expires int64
	var executed sql.NullInt64
	var raw, unknown, result, retention []byte
	err := row.Scan(&p.ID, &p.AppID, &created, &expires, &raw, &p.ReclaimableBytes, &p.ActiveGenerations, &unknown, &executed, &result, &p.AppRuntimeRevision, &p.VendorRuntimeRevision, &retention)
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(unknown, &p.UnknownVersions); err != nil {
		return p, err
	}
	if retention != nil {
		if err = json.Unmarshal(retention, &p.Retention); err != nil {
			return p, err
		}
	}
	p.Result = json.RawMessage(result)
	p.CreatedAt = time.Unix(created, 0).UTC()
	p.ExpiresAt = time.Unix(expires, 0).UTC()
	p.ExecutedAt = timePointer(executed)
	err = json.Unmarshal(raw, &p.Selection)
	return p, err
}
func (s *Store) CleanupPreview(app, id string) (CleanupPreview, error) {
	if err := requireApp(app); err != nil {
		return CleanupPreview{}, err
	}
	return scanCleanup(s.DB.QueryRow("SELECT "+cleanupColumns+" FROM cleanup_previews WHERE id=? AND app_id=?", id, app))
}

// ApplicationCleanupPreview reads a preview of any source epoch of the
// application uid; previews of other applications are sql.ErrNoRows.
func (s *Store) ApplicationCleanupPreview(uid, id string) (CleanupPreview, error) {
	p, err := scanCleanup(s.DB.QueryRow("SELECT "+cleanupColumns+" FROM cleanup_previews WHERE id=?", id))
	if err != nil {
		return CleanupPreview{}, err
	}
	if owner, _, ok := identity.ParseStorageID(p.AppID); !ok || owner != uid {
		return CleanupPreview{}, sql.ErrNoRows
	}
	return p, nil
}
func optionalRetention(g *RetentionGuard) any {
	if g == nil {
		return nil
	}
	raw, _ := json.Marshal(g)
	return raw
}
func (s *Store) RetireCleanupPreview(app, id string, at time.Time) (CleanupPreview, error) {
	return s.retireCleanupPreview(app, id, at, nil, false)
}
func (s *Store) RetireRetentionPreview(app, id string, at time.Time, blocked map[string]string) (CleanupPreview, error) {
	return s.retireCleanupPreview(app, id, at, blocked, true)
}
func (s *Store) retireCleanupPreview(app, id string, at time.Time, blocked map[string]string, safe bool) (CleanupPreview, error) {
	var zero CleanupPreview
	if err := requireApp(app); err != nil {
		return zero, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	p, err := scanCleanup(tx.QueryRow("SELECT "+cleanupColumns+" FROM cleanup_previews WHERE id=? AND app_id=?", id, app))
	if err != nil {
		return p, err
	}
	if p.ExecutedAt == nil && !at.Before(p.ExpiresAt) || p.ExecutedAt != nil && !at.Before(p.ExecutedAt.Add(24*time.Hour)) {
		return p, ErrExpired
	}
	if (p.Retention != nil) != safe {
		return p, ErrConflict
	}
	if safe && p.ExecutedAt != nil {
		return p, nil
	}
	if safe {
		if err = checkRetention(tx, app, p.Retention, at); err != nil {
			return p, err
		}
	}
	if p.ExecutedAt == nil {
		if err = checkCleanupFence(tx, app, p.SourceFence); err != nil {
			return p, err
		}
	}
	if safe {
		for _, item := range p.Selection {
			var current bool
			err := tx.QueryRow("SELECT is_current FROM generations WHERE id=? AND app_id=?", item.GenerationID, app).Scan(&current)
			if errors.Is(err, sql.ErrNoRows) || err == nil && !current {
				blocked[item.Version] = "generation_changed"
			} else if err != nil {
				return p, err
			}
		}
	}
	receipt := RetentionReceipt{Selection: []CleanupSelection{}, Skipped: blocked}
	versions := map[string]bool{}
	for _, item := range p.Selection {
		if safe && blocked[item.Version] != "" {
			continue
		}
		var a, v, k string
		err = tx.QueryRow("SELECT app_id,version,resource_key FROM generations WHERE id=?", item.GenerationID).Scan(&a, &v, &k)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return p, err
		}
		if a != app || v != item.Version || k != item.ResourceKey {
			return p, errors.New("Cleanup snapshot ownership mismatch")
		}
		if safe {
			receipt.Selection = append(receipt.Selection, item)
			receipt.LogicalBytes += item.SnapshotBytes
			versions[item.Version] = true
		}
		if _, err = tx.Exec("UPDATE generations SET is_current=0,retired_at_s=COALESCE(retired_at_s,?) WHERE id=? AND app_id=?", at.Unix(), item.GenerationID, app); err != nil {
			return p, err
		}
	}
	if p.ExecutedAt == nil {
		p.ExecutedAt = &at
		p.Result, _ = json.Marshal(map[string]int{"selected_generations": len(p.Selection)})
		if safe {
			receipt.RetiredVersions = len(versions)
			p.Result, _ = json.Marshal(receipt)
		}
		if _, err = tx.Exec("UPDATE cleanup_previews SET executed_at_s=?,result_json=? WHERE id=? AND app_id=?", at.Unix(), []byte(p.Result), id, app); err != nil {
			return p, err
		}
	}
	return p, tx.Commit()
}
func (s *Store) CompleteCleanupPreview(app, id string, result json.RawMessage) error {
	if requireApp(app) != nil || !json.Valid(result) {
		return errors.New("Invalid cleanup result")
	}
	r, err := s.DB.Exec("UPDATE cleanup_previews SET result_json=? WHERE id=? AND app_id=? AND executed_at_s IS NOT NULL", []byte(result), id, app)
	return affected(r, err)
}
func (s *Store) DeleteExpiredCleanupPreviews(at time.Time) error {
	_, err := s.DB.Exec("DELETE FROM cleanup_previews WHERE (executed_at_s IS NULL AND expires_at_s<=?) OR (executed_at_s IS NOT NULL AND executed_at_s<=?)", at.Unix(), at.Add(-24*time.Hour).Unix())
	return err
}

// Cleanup is an explicit management action, so disabled, tombstoned and historic
// sources may be selected. A later revision requires a fresh preview; unlike
// publication it never requires the source to be enabled.
func checkCleanupFence(tx *sql.Tx, storageID string, fence SourceFence) error {
	uid, epoch, dynamic := identity.ParseStorageID(storageID)
	if !dynamic {
		if strings.HasPrefix(storageID, "app/") || !identity.ValidKey(storageID) {
			return ErrInvalidDirectory
		}
		return nil
	}
	source, err := scanSource(tx.QueryRow(`SELECT `+sourceColumns+sourceJoin+` WHERE src.app_uid=? AND src.epoch=?`, uid, epoch))
	if err != nil {
		return err
	}
	if source.Fence() != fence {
		return ErrSourceInactive
	}
	return nil
}

// RequireCleanupFence rejects stale management previews without requiring an active upstream.
func (s *Store) RequireCleanupFence(tx *sql.Tx, storageID string, fence SourceFence) error {
	if tx == nil {
		return ErrInvalidDirectory
	}
	return checkCleanupFence(tx, storageID, fence)
}
