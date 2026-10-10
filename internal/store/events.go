package store

import (
	"database/sql"
	"errors"
	"time"
)

type Event struct {
	Time                                                               time.Time
	AppID, Version, ResourceKey, GenerationID, Category, Code, Message string
	UpstreamStatus                                                     *int
}

func (s *Store) RecordEvent(e Event) error {
	if e.AppID != "" && !ValidAppID(e.AppID) || e.Category == "" || e.Code == "" || e.Message == "" || e.UpstreamStatus != nil && (*e.UpstreamStatus < 100 || *e.UpstreamStatus > 599) {
		return errors.New("Invalid structured event")
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if e.AppID != "" {
		exists, err := applicationNamespaceExists(tx, e.AppID)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
	}
	var app any
	if e.AppID != "" {
		app = e.AppID
	}
	if _, err = tx.Exec("INSERT INTO events(time_s,app_id,version,resource_key,generation_id,category,code,message,upstream_status) VALUES(?,?,?,?,?,?,?,?,?)", e.Time.Unix(), app, e.Version, e.ResourceKey, e.GenerationID, e.Category, e.Code, e.Message, e.UpstreamStatus); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM events WHERE id NOT IN (SELECT id FROM events ORDER BY id DESC LIMIT 1000) OR time_s < ?", time.Now().Add(-30*24*time.Hour).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Events() ([]map[string]any, error) { return s.events("") }
func (s *Store) EventsFor(app string) ([]map[string]any, error) {
	if err := requireApp(app); err != nil {
		return nil, err
	}
	return s.events(app)
}
func (s *Store) events(app string) ([]map[string]any, error) {
	query := "SELECT time_s,app_id,version,resource_key,generation_id,category,code,message,upstream_status FROM events"
	args := []any{}
	if app != "" {
		query += " WHERE app_id=?"
		args = append(args, app)
	}
	query += " ORDER BY id DESC LIMIT 100"
	rows, err := s.read.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var at int64
		var a, v, r, g sql.NullString
		var c, code, m string
		var status sql.NullInt64
		if err = rows.Scan(&at, &a, &v, &r, &g, &c, &code, &m, &status); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"time": time.Unix(at, 0).UTC().Format(time.RFC3339), "app_id": a.String, "version": v.String, "resource_key": r.String, "generation_id": g.String, "resource": r.String, "category": c, "code": code, "message": m, "status_code": int(status.Int64)})
	}
	return out, rows.Err()
}
