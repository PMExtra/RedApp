package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"path/filepath"
	"time"
)

type Store struct {
	DB    *sql.DB
	rates rates
}

func Open(dir string) (*Store, error) {
	db, err := sql.Open("sqlite3", filepath.Join(dir, "state.sqlite")+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_synchronous=FULL")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS schema_version(version INTEGER NOT NULL);
 INSERT INTO schema_version SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM schema_version);
 CREATE TABLE IF NOT EXISTS records(kind TEXT NOT NULL,id TEXT NOT NULL,body BLOB NOT NULL,PRIMARY KEY(kind,id));
 CREATE TABLE IF NOT EXISTS versions(version TEXT PRIMARY KEY,first_seen TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY,time TEXT NOT NULL,resource TEXT NOT NULL,category TEXT NOT NULL,message TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS counters(name TEXT PRIMARY KEY,value INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS admin(id INTEGER PRIMARY KEY CHECK(id=1),hash BLOB NOT NULL,revision INTEGER NOT NULL);
 `)
	if err != nil {
		db.Close()
		return nil, err
	}
	var version int
	err = db.QueryRow("SELECT version FROM schema_version").Scan(&version)
	if err != nil || (version != 1 && version != 2) {
		db.Close()
		return nil, fmt.Errorf("Unsupported database schema %d", version)
	}
	if version == 1 {
		tx, e := db.Begin()
		if e == nil {
			_, e = tx.Exec(`ALTER TABLE versions RENAME TO versions_v1;
CREATE TABLE versions(app TEXT NOT NULL,version TEXT NOT NULL,first_seen TEXT NOT NULL,PRIMARY KEY(app,version));
INSERT INTO versions SELECT 'codex',version,first_seen FROM versions_v1;
DROP TABLE versions_v1;
UPDATE schema_version SET version=2;`)
			if e == nil {
				e = tx.Commit()
			}
			tx.Rollback()
		}
		if e != nil {
			db.Close()
			return nil, fmt.Errorf("Migrate application history: %w", e)
		}
	}
	return &Store{DB: db, rates: rates{started: time.Now()}}, nil
}
func (s *Store) Put(kind, id string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO records VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body", kind, id, b)
	return e
}
func (s *Store) Get(kind, id string, v any) error {
	var b []byte
	if e := s.DB.QueryRow("SELECT body FROM records WHERE kind=? AND id=?", kind, id).Scan(&b); e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}

// PutVersion commits trusted metadata and its first-seen history together.
func (s *Store) PutVersion(kind, id string, v any, app, version string) error {
	if app == "" || version == "" {
		return fmt.Errorf("Application and version are required")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO records VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body", kind, id, b); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT OR IGNORE INTO versions VALUES(?,?,?)", app, version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Delete(kind, id string) error {
	_, e := s.DB.Exec("DELETE FROM records WHERE kind=? AND id=?", kind, id)
	return e
}
func (s *Store) List(kind string) ([]json.RawMessage, error) {
	rows, e := s.DB.Query("SELECT body FROM records WHERE kind=? ORDER BY id", kind)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		out = append(out, json.RawMessage(b))
	}
	return out, rows.Err()
}
func (s *Store) Seen(v string) error {
	return s.SeenFor("codex", v)
}
func (s *Store) SeenFor(app, v string) error {
	if app == "" || v == "" {
		return fmt.Errorf("Application and version are required")
	}
	_, e := s.DB.Exec("INSERT OR IGNORE INTO versions VALUES(?,?,?)", app, v, time.Now().UTC().Format(time.RFC3339Nano))
	return e
}
func (s *Store) Versions() (map[string]string, error) {
	return s.VersionsFor("codex")
}
func (s *Store) VersionsFor(app string) (map[string]string, error) {
	rows, e := s.DB.Query("SELECT version,first_seen FROM versions WHERE app=?", app)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var v, t string
		if e = rows.Scan(&v, &t); e != nil {
			return nil, e
		}
		out[v] = t
	}
	return out, rows.Err()
}
func (s *Store) Event(id, cat, msg string) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO events(time,resource,category,message) VALUES(?,?,?,?)", time.Now().UTC().Format(time.RFC3339Nano), id, cat, msg); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM events WHERE id NOT IN (SELECT id FROM events ORDER BY id DESC LIMIT 1000) OR time < ?", time.Now().Add(-30*24*time.Hour).UTC().Format(time.RFC3339Nano)); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Events() ([]map[string]any, error) {
	rows, e := s.DB.Query("SELECT time,resource,category,message FROM events ORDER BY id DESC LIMIT 100")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var t, r, c, m string
		if e = rows.Scan(&t, &r, &c, &m); e != nil {
			return nil, e
		}
		var status int
		fmt.Sscanf(m, "Upstream HTTP %d", &status)
		if status == 0 {
			fmt.Sscanf(m, "\u4e0a\u6e38 HTTP %d", &status)
		}
		out = append(out, map[string]any{"time": t, "resource": r, "category": c, "message": m, "status_code": status})
	}
	return out, rows.Err()
}
func (s *Store) Add(name string, n int64) error {
	s.sample(name, n)
	_, e := s.DB.Exec("INSERT INTO counters VALUES(?,?) ON CONFLICT(name) DO UPDATE SET value=value+excluded.value", name, n)
	return e
}
func (s *Store) Counters() (map[string]int64, error) {
	rows, e := s.DB.Query("SELECT name,value FROM counters")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var v int64
		if e = rows.Scan(&k, &v); e != nil {
			return nil, e
		}
		out[k] = v
	}
	return out, rows.Err()
}
