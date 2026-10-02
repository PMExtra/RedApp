package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type CleanupSelection struct {
	GenerationID  string `json:"generation_id"`
	Version       string `json:"version"`
	ResourceKey   string `json:"resource_key"`
	SnapshotBytes int64  `json:"snapshot_bytes"`
}
type CleanupPreview struct {
	ID, AppID            string
	CreatedAt, ExpiresAt time.Time
	Selection            []CleanupSelection
	ExecutedAt           *time.Time
	Result               json.RawMessage
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
	_, err = tx.Exec("INSERT INTO cleanup_previews(id,app_id,created_at_s,expires_at_s,selection_json) VALUES(?,?,?,?,?)", p.ID, p.AppID, p.CreatedAt.Unix(), p.ExpiresAt.Unix(), raw)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func scanCleanup(row scanner) (CleanupPreview, error) {
	var p CleanupPreview
	var created, expires int64
	var executed sql.NullInt64
	var raw, result []byte
	err := row.Scan(&p.ID, &p.AppID, &created, &expires, &raw, &executed, &result)
	if err != nil {
		return p, err
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
	return scanCleanup(s.DB.QueryRow("SELECT id,app_id,created_at_s,expires_at_s,selection_json,executed_at_s,result_json FROM cleanup_previews WHERE id=? AND app_id=?", id, app))
}
func (s *Store) RetireCleanupPreview(app, id string, at time.Time) (CleanupPreview, error) {
	var zero CleanupPreview
	if err := requireApp(app); err != nil {
		return zero, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	p, err := scanCleanup(tx.QueryRow("SELECT id,app_id,created_at_s,expires_at_s,selection_json,executed_at_s,result_json FROM cleanup_previews WHERE id=? AND app_id=?", id, app))
	if err != nil {
		return p, err
	}
	if p.ExecutedAt == nil && !at.Before(p.ExpiresAt) || p.ExecutedAt != nil && !at.Before(p.ExecutedAt.Add(24*time.Hour)) {
		return p, ErrExpired
	}
	for _, item := range p.Selection {
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
		if _, err = tx.Exec("UPDATE generations SET is_current=0,retired_at_s=COALESCE(retired_at_s,?) WHERE id=? AND app_id=?", at.Unix(), item.GenerationID, app); err != nil {
			return p, err
		}
	}
	if p.ExecutedAt == nil {
		p.ExecutedAt = &at
		p.Result, _ = json.Marshal(map[string]int{"selected_generations": len(p.Selection)})
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
