package store

import (
	"database/sql"
	"errors"
	"time"
)

func (s *Store) VersionCount(app string) (int64, error) {
	if err := requireApp(app); err != nil {
		return 0, err
	}
	var n int64
	err := s.DB.QueryRow("SELECT COUNT(*) FROM app_versions WHERE app_id=?", app).Scan(&n)
	return n, err
}

// VersionPage uses keyset pagination within one application. HTTP asks for one
// extra row to determine whether a next page exists; no whole history is loaded.
func (s *Store) VersionPage(app, after string, limit int) ([]VersionStats, error) {
	if requireApp(app) != nil || limit < 1 || limit > 101 {
		return nil, errors.New("Invalid version page")
	}
	rows, err := s.DB.Query("SELECT version,first_seen_s,artifact_requests,downstream_bytes FROM app_versions WHERE app_id=? AND version>? ORDER BY version ASC LIMIT ?", app, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]VersionStats, 0)
	for rows.Next() {
		var v VersionStats
		var at int64
		if err = rows.Scan(&v.Version, &at, &v.ArtifactRequests, &v.DownstreamBytes); err != nil {
			return nil, err
		}
		v.FirstSeen = time.Unix(at, 0).UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

type ListedEvent struct {
	ID           int64     `json:"id"`
	Time         time.Time `json:"time"`
	AppID        string    `json:"app_id"`
	Version      string    `json:"version"`
	ResourceKey  string    `json:"resource_key"`
	GenerationID string    `json:"generation_id"`
	Resource     string    `json:"resource"`
	Category     string    `json:"category"`
	Code         string    `json:"code"`
	Message      string    `json:"message"`
	StatusCode   int       `json:"status_code"`
}

// EventPage returns newest IDs first, with an optional application restriction.
// Retention remains the existing write-side 1,000-event / 30-day policy. The read
// also excludes expired rows when no new event has triggered write-side pruning.
func (s *Store) EventPage(app string, beforeID int64, limit int) ([]ListedEvent, error) {
	if app != "" && requireApp(app) != nil || beforeID < 0 || limit < 1 || limit > 101 {
		return nil, errors.New("Invalid event page")
	}
	query := "SELECT id,time_s,app_id,version,resource_key,generation_id,category,code,message,upstream_status FROM events WHERE time_s>=?"
	args := []any{time.Now().Add(-30 * 24 * time.Hour).Unix()}
	if app != "" {
		query += " AND app_id=?"
		args = append(args, app)
	}
	if beforeID > 0 {
		query += " AND id<?"
		args = append(args, beforeID)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ListedEvent, 0)
	for rows.Next() {
		var event ListedEvent
		var at int64
		var appID, version, key, generation sql.NullString
		var status sql.NullInt64
		if err = rows.Scan(&event.ID, &at, &appID, &version, &key, &generation, &event.Category, &event.Code, &event.Message, &status); err != nil {
			return nil, err
		}
		event.Time = time.Unix(at, 0).UTC()
		event.AppID = appID.String
		event.Version = version.String
		event.ResourceKey = key.String
		event.Resource = key.String
		event.GenerationID = generation.String
		event.StatusCode = int(status.Int64)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Store) VersionNumberPage(app string, page, limit int) (Page[VersionStats], error) {
	if requireApp(app) != nil || page < 1 || page > 1000000000 || limit < 1 || limit > 100 {
		return Page[VersionStats]{}, errors.New("Invalid version page")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Page[VersionStats]{}, err
	}
	defer tx.Rollback()
	var total int64
	if err = tx.QueryRow(`SELECT COUNT(*) FROM app_versions WHERE app_id=?`, app).Scan(&total); err != nil {
		return Page[VersionStats]{}, err
	}
	result := NewPage[VersionStats](page, limit, total)
	rows, err := tx.Query(`SELECT version,first_seen_s,artifact_requests,downstream_bytes FROM app_versions WHERE app_id=? ORDER BY version ASC LIMIT ? OFFSET ?`, app, limit, (result.Page-1)*limit)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var v VersionStats
		var at int64
		if err = rows.Scan(&v.Version, &at, &v.ArtifactRequests, &v.DownstreamBytes); err != nil {
			rows.Close()
			return result, err
		}
		v.FirstSeen = time.Unix(at, 0).UTC()
		result.Items = append(result.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}
