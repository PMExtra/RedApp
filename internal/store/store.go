// Package store owns the schema and typed persistence boundaries of RedApp.
package store

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/presets"
)

const SchemaVersion = 11

var ErrFreshDirectory = errors.New("This data directory belongs to an old or unknown database; use a new empty data directory. Configuration, cache and history are not migrated. Keep the old directory unchanged")
var ErrConflict = errors.New("Setting revision changed; reload before saving")
var ErrImmutableRelease = errors.New("Trusted release resource bindings changed")
var ErrExpired = errors.New("Cleanup preview expired")

//go:embed schema.sql
var schema string

type Store struct {
	DB                    *sql.DB
	rates                 rates
	pending               counterBuffer
	work                  applicationWork
	configMu              sync.Mutex
	validateDistributions func([]presets.Descriptor) error
	prepareConfiguration  func(DirectorySnapshot) (ConfigurationPublication, error)
	configurationFault    func(string, *sql.Tx) error // Test-only deterministic fault/synchronization seam.
}

func ValidAppID(app string) bool {
	return identity.ValidKey(app)
}
func sqliteURL(path string, query string) string {
	return (&url.URL{Scheme: "file", Path: path}).String() + "?" + query
}

// Open checks the immutable main-file schema before any writable connection.
// Only the exact current schema is accepted; old or unknown directories are refused without modification.
func Open(dir string) (*Store, error) {
	path, err := filepath.Abs(filepath.Join(dir, "state.sqlite"))
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	fresh := errors.Is(err, os.ErrNotExist)
	if err != nil && !fresh {
		return nil, err
	}
	if !fresh {
		if !info.Mode().IsRegular() {
			return nil, ErrFreshDirectory
		}
		if err = probeExisting(path); err != nil {
			return nil, err
		}
	} else {
		entries, e := os.ReadDir(dir)
		if e != nil {
			return nil, e
		}
		for _, entry := range entries {
			if entry.Name() != "instance.lock" {
				return nil, ErrFreshDirectory
			}
		}
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if e != nil {
			return nil, e
		}
		if e = f.Close(); e != nil {
			return nil, e
		}
	}
	db, err := sql.Open("sqlite3", sqliteURL(path, "mode=rw&_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_synchronous=FULL"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if fresh {
		tx, e := db.Begin()
		if e == nil {
			_, e = tx.Exec(schema)
			if e == nil {
				e = tx.Commit()
			}
			tx.Rollback()
		}
		err = e
	} else {
		err = db.Ping()
	}
	if err == nil {
		// Validate the authoritative WAL view before readiness too. This is a
		// read-only check after the main file established schema ownership.
		err = checkSchema(db)
	}
	if err == nil {
		// Persist the immutable schema to the main file before a first successful
		// startup. Later WAL transactions contain data only, so read-only preflight
		// needs no writable sidecar or full database copy.
		var busy, frames, checkpointed int
		err = db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed)
		if err == nil && busy != 0 {
			err = errors.New("Initial schema checkpoint is busy")
		}
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{DB: db, rates: rates{started: time.Now()}}
	if err = s.loadApplicationDeletionGates(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func probeExisting(path string) error {
	// immutable=1 guarantees SQLite neither creates nor updates WAL/SHM/journal
	// files. It deliberately reads the main file, whose schema is checkpointed
	// before startup, including after the supported atomic upgrade.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Lstat(path + suffix)
		if errors.Is(err, os.ErrNotExist) && suffix != "" {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return ErrFreshDirectory
		}
	}
	db, err := sql.Open("sqlite3", sqliteURL(path, "mode=ro&immutable=1&_query_only=on"))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrFreshDirectory, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if checkSchema(db) != nil {
		return ErrFreshDirectory
	}
	return checkReservedVendor(db)
}
func checkSchema(db *sql.DB) error { return checkSchemaDefinition(db, schema, SchemaVersion) }
func checkSchemaDefinition(db *sql.DB, definition string, expectedVersion int) error {
	var count, version int
	if err := db.QueryRow("SELECT COUNT(*),COALESCE(MAX(version),0) FROM schema_version").Scan(&count, &version); err != nil || count != 1 || version != expectedVersion {
		return ErrFreshDirectory
	}
	// A version marker alone is not enough to authorize opening an unknown
	// database read/write. Require the table and constraint definitions written
	// by this schema; auxiliary diagnostic triggers are intentionally ignored.
	required := map[string]string{}
	for _, match := range regexp.MustCompile(`(?s)CREATE (?:UNIQUE )?(?:TABLE|INDEX)\s+([a-z_]+)[^;]*;`).FindAllStringSubmatch(definition, -1) {
		required[match[1]] = strings.Join(strings.Fields(strings.TrimSuffix(match[0], ";")), " ")
	}
	rows, err := db.Query("SELECT type,name,sql FROM sqlite_schema WHERE type IN ('table','index') AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return ErrFreshDirectory
	}
	defer rows.Close()
	for rows.Next() {
		var kind, name, definition string
		if rows.Scan(&kind, &name, &definition) != nil {
			return ErrFreshDirectory
		}
		expected, ok := required[name]
		if !ok {
			if kind == "table" {
				return ErrFreshDirectory
			}
			continue
		}
		definition = strings.Replace(definition, `CREATE TABLE "applications"`, `CREATE TABLE applications`, 1)
		definition = strings.Replace(definition, `CREATE TABLE "application_sources"`, `CREATE TABLE application_sources`, 1)
		if strings.Join(strings.Fields(definition), " ") != expected {
			return ErrFreshDirectory
		}
		delete(required, name)
	}
	if rows.Err() != nil || len(required) != 0 {
		return ErrFreshDirectory
	}
	return nil
}
func requireApp(app string) error {
	if !ValidAppID(app) {
		return errors.New("Canonical vendor/app identity is required")
	}
	return nil
}
func unixPointer(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Unix()
}
func timePointer(t sql.NullInt64) *time.Time {
	if !t.Valid {
		return nil
	}
	v := time.Unix(t.Int64, 0).UTC()
	return &v
}

// Preflight may be called before acquiring an instance lock; it does not create
// or modify any file in the data directory. Open repeats it under that lock.
func Preflight(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	// Observe an existing kernel lock without creating the lock file. This
	// refuses a live writer's directory and preserves the useful
	// "already owned" diagnostic before the CLI's exclusive Acquire call.
	lock, lockErr := os.OpenFile(filepath.Join(dir, "instance.lock"), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if lockErr == nil {
		defer lock.Close()
		info, err := lock.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("Instance lock must be a regular file")
		}
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
			return fmt.Errorf("Data directory is already owned by another instance: %w", err)
		}
	} else if !errors.Is(lockErr, os.ErrNotExist) {
		return lockErr
	}
	hasDB := false
	for _, entry := range entries {
		if entry.Name() == "state.sqlite" {
			hasDB = true
		}
	}
	if hasDB {
		path, err := filepath.Abs(filepath.Join(dir, "state.sqlite"))
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return ErrFreshDirectory
		}
		return probeExisting(path)
	}
	for _, entry := range entries {
		if entry.Name() != "instance.lock" {
			return ErrFreshDirectory
		}
	}
	return nil
}

func checkReservedVendor(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM vendors WHERE id='all'`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return ErrFreshDirectory
	}
	return nil
}
