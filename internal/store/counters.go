package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

func counterKey(name string) (string, error) {
	name = strings.TrimPrefix(name, "counters.")
	switch name {
	case "requests", "artifact_requests", "cache_hit_requests", "shared_follower_requests", "miss_requests", "download_success", "download_errors", "upstream_errors", "upstream_bytes", "downstream_bytes", "cleanup_freed_bytes":
		return "counters." + name, nil
	}
	return "", errors.New("Unknown active counter")
}
func increment(tx *sql.Tx, scope, app, key string, n int64, at int64) error {
	_, err := tx.Exec("INSERT INTO metric_counters(scope,app_id,metric,value,observed_since_s) VALUES(?,?,?,?,?) ON CONFLICT(scope,app_id,metric) DO UPDATE SET value=value+excluded.value", scope, app, key, n, at)
	return err
}
func (s *Store) add(app, name string, n int64) error {
	key, err := counterKey(name)
	if err != nil {
		return err
	}
	if n < 0 {
		return errors.New("Counter increments cannot be negative")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := time.Now().Unix()
	if err = increment(tx, "global", "", key, n, at); err != nil {
		return err
	}
	if app != "" {
		if err = increment(tx, "app", app, key, n, at); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.sample(strings.TrimPrefix(key, "counters."), n)
	return nil
}
func (s *Store) Add(name string, n int64) error { return s.add("", name, n) }

// AddFor increments global and owning-app counters in the same transaction.
func (s *Store) AddFor(app, name string, n int64) error {
	if err := requireApp(app); err != nil {
		return err
	}
	return s.add(app, name, n)
}
func (s *Store) counters(scope, app string) (map[string]int64, error) {
	rows, err := s.DB.Query("SELECT metric,value FROM metric_counters WHERE scope=? AND app_id=?", scope, app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var key string
		var value int64
		if err = rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[strings.TrimPrefix(key, "counters.")] = value
	}
	return out, rows.Err()
}
func (s *Store) Counters() (map[string]int64, error) { return s.counters("global", "") }
func (s *Store) CountersFor(app string) (map[string]int64, error) {
	if err := requireApp(app); err != nil {
		return nil, err
	}
	return s.counters("app", app)
}
