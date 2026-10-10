// Package store owns the schema and typed persistence boundaries of RedApp.
package store

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/instance"
	"github.com/PMExtra/RedApp/presets"
)

// SchemaVersion is stored in PRAGMA user_version. Before 1.0 every schema
// change increments it and older directories are refused (ADR 0001).
const SchemaVersion = 13

// applicationID ("RdAp") is stored in PRAGMA application_id so that a foreign
// SQLite file whose user_version happens to match is still refused.
const applicationID = 0x52644170

const databaseName = "state.sqlite"

var ErrIncompatibleDirectory = errors.New("This data directory belongs to another RedApp schema version or is not a RedApp data directory; use a new empty data directory. Data is never migrated and the old directory is left unchanged")
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
	writeMu               sync.Mutex // orders configuration writes with their publication; held across one write transaction plus the in-memory prepare and publish (see writeConfiguration)
	validateDistributions func([]presets.Descriptor) error
	prepareConfiguration  func(DirectorySnapshot) (ConfigurationPublication, error)
	// beforeCommit runs inside every configuration transaction just before it
	// commits. Only openStore options set it.
	beforeCommit func(*sql.Tx) error
	// view is the runtime view of the last committed configuration, loaded on
	// first use. writeMu guards it.
	view *directoryView
}

// option configures a Store at construction; production uses none.
type option func(*Store)

func ValidAppID(app string) bool {
	return identity.ValidKey(app)
}
func sqliteURL(path string, query string) string {
	return (&url.URL{Scheme: "file", Path: path}).String() + "?" + query
}

// Open creates the schema in a new empty directory or opens a directory whose
// database has exactly SchemaVersion. Any other directory is refused without
// modification. The caller must hold the directory's instance lock.
func Open(dir string) (*Store, error) { return openStore(dir) }

func openStore(dir string, options ...option) (*Store, error) {
	path, err := filepath.Abs(filepath.Join(dir, databaseName))
	if err != nil {
		return nil, err
	}
	fresh, err := inspectDirectory(dir, path)
	if err != nil {
		return nil, err
	}
	if fresh {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		if err = f.Close(); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite3", sqliteURL(path, "mode=rw&_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_synchronous=FULL"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if fresh {
		err = createSchema(db)
	} else {
		// The read-only probe saw the main file; confirm the WAL view agrees.
		err = checkVersion(db)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{DB: db, rates: rates{started: time.Now()}}
	for _, apply := range options {
		apply(s)
	}
	if err = s.loadApplicationDeletionGates(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// createSchema writes the schema and version in one transaction and then
// checkpoints them into the main file, so the immutable read-only probe of a
// later start sees them even if the process dies with data left in the WAL.
func createSchema(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(schema); err != nil {
		return err
	}
	if _, err = tx.Exec(fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d", applicationID, SchemaVersion)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	var busy, frames, checkpointed int
	if err = db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed); err != nil {
		return err
	}
	if busy != 0 {
		return errors.New("Initial schema checkpoint is busy")
	}
	return nil
}

// Preflight checks a data directory before the instance lock is acquired. It
// creates and modifies nothing, so a refused directory stays byte-for-byte
// unchanged; Open repeats the check under the lock.
func Preflight(dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// A live owner may be writing the database, which the immutable probe
	// cannot read consistently; report the owner instead.
	if err := instance.Check(dir); err != nil {
		return err
	}
	path, err := filepath.Abs(filepath.Join(dir, databaseName))
	if err != nil {
		return err
	}
	_, err = inspectDirectory(dir, path)
	return err
}

// inspectDirectory reports whether dir is new (no database and nothing but the
// instance lock) or holds a database of this schema version.
func inspectDirectory(dir, path string) (fresh bool, err error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false, err
		}
		for _, entry := range entries {
			if entry.Name() != instance.LockName {
				return false, ErrIncompatibleDirectory
			}
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, ErrIncompatibleDirectory
	}
	return false, probeExisting(path)
}

// probeExisting reads the version of an existing database. immutable=1 makes
// SQLite read only the main file and never create or update WAL, SHM or journal
// files; the version is checkpointed into the main file at creation.
func probeExisting(path string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		info, err := os.Lstat(path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return ErrIncompatibleDirectory
		}
	}
	db, err := sql.Open("sqlite3", sqliteURL(path, "mode=ro&immutable=1&_query_only=on"))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrIncompatibleDirectory, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	return checkVersion(db)
}

func checkVersion(db *sql.DB) error {
	var app, version int
	if err := db.QueryRow("PRAGMA application_id").Scan(&app); err != nil {
		return fmt.Errorf("%w: %v", ErrIncompatibleDirectory, err)
	}
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("%w: %v", ErrIncompatibleDirectory, err)
	}
	if app != applicationID || version != SchemaVersion {
		return ErrIncompatibleDirectory
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
