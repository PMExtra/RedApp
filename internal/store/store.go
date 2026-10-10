// Package store owns the schema and typed persistence boundaries of RedApp.
package store

import (
	"context"
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
const SchemaVersion = 14

// applicationID ("RdAp") is stored in PRAGMA application_id so that a foreign
// SQLite file whose user_version happens to match is still refused.
const applicationID = 0x52644170

const databaseName = "state.sqlite"

var ErrIncompatibleDirectory = errors.New("this data directory belongs to another RedApp schema version or is not a RedApp data directory; use a new empty data directory. Data is never migrated and the old directory is left unchanged")

// ErrNotFound reports that a requested row does not exist. It is
// sql.ErrNoRows, so callers outside this package match it without importing
// database/sql.
var ErrNotFound = sql.ErrNoRows
var ErrConflict = errors.New("setting revision changed; reload before saving")
var ErrImmutableRelease = errors.New("trusted release resource bindings changed")
var ErrExpired = errors.New("cleanup preview expired")

//go:embed schema.sql
var schema string

// Store owns the SQLite database. It keeps two pools over the same WAL file:
//
//   - db is the only writer: one connection (SetMaxOpenConns(1)) opened with
//     _txlock=immediate, so every write and every read-modify-write transaction
//     is serialized in the process and takes the database write lock up front.
//   - read is a pool of query_only connections for statements and snapshot
//     transactions that never write. WAL lets them run while a write
//     transaction is open; each sees the last committed state.
//
// While a transaction on db is open the writer connection is held, so code
// running inside it (including callbacks such as finalize, beforeCommit or a
// purge wrapper) must use only that transaction and must not call Store
// methods that write: they would wait for the connection the caller holds.
// Store methods that only read use the read pool and are safe there, but they
// do not observe the transaction's uncommitted changes.
type Store struct {
	db                    *sql.DB
	read                  *sql.DB
	busyTimeout           time.Duration
	rates                 rates
	pending               counterBuffer
	work                  applicationWork
	configMu              sync.Mutex
	validateDistributions func([]presets.Descriptor) error
	prepareConfiguration  func(DirectorySnapshot) (ConfigurationPublication, error)
	// beforeCommit runs inside every configuration transaction just before it
	// commits. Only test options set it.
	beforeCommit func(*sql.Tx) error
}

// Option configures a Store at construction; production uses none.
type Option func(*Store)

// WithBusyTimeout bounds how long a statement waits for a database lock held
// by another connection before failing (default 5 seconds).
func WithBusyTimeout(d time.Duration) Option {
	return func(s *Store) { s.busyTimeout = d }
}

// readPoolSize bounds concurrent read connections; each holds a WAL snapshot
// only for the duration of one statement or read transaction.
const readPoolSize = 8

func ValidAppID(app string) bool {
	return identity.ValidKey(app)
}
func sqliteURL(path string, query string) string {
	return (&url.URL{Scheme: "file", Path: path}).String() + "?" + query
}

// Open creates the schema in a new empty directory or opens a directory whose
// database has exactly SchemaVersion. Any other directory is refused without
// modification. The caller must hold the directory's instance lock.
func Open(dir string, options ...Option) (*Store, error) {
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
	s := &Store{busyTimeout: 5 * time.Second, rates: rates{started: time.Now()}}
	for _, apply := range options {
		apply(s)
	}
	busy := fmt.Sprintf("_busy_timeout=%d", s.busyTimeout.Milliseconds())
	if s.db, err = sql.Open("sqlite3", sqliteURL(path, "mode=rw&_journal_mode=WAL&_txlock=immediate&_foreign_keys=on&_synchronous=FULL&"+busy)); err != nil {
		return nil, err
	}
	s.db.SetMaxOpenConns(1)
	if fresh {
		err = createSchema(s.db)
	} else {
		// The read-only probe saw the main file; confirm the WAL view agrees.
		err = checkVersion(s.db)
	}
	if err != nil {
		s.db.Close()
		return nil, err
	}
	// The writer has created the WAL and shared-memory files, which read-only
	// connections need but cannot create.
	if s.read, err = sql.Open("sqlite3", sqliteURL(path, "mode=ro&_query_only=on&"+busy)); err != nil {
		s.db.Close()
		return nil, err
	}
	s.read.SetMaxOpenConns(readPoolSize)
	s.read.SetMaxIdleConns(readPoolSize)
	if err = s.loadApplicationDeletionGates(); err != nil {
		s.closeDatabases()
		return nil, err
	}
	return s, nil
}

func (s *Store) closeDatabases() error {
	return errors.Join(s.read.Close(), s.db.Close())
}

// Ping checks that both the writer and a reader connection are usable.
func (s *Store) Ping(ctx context.Context) error {
	return errors.Join(s.db.PingContext(ctx), s.read.PingContext(ctx))
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
		return errors.New("initial schema checkpoint is busy")
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
		return fmt.Errorf("%w: %w", ErrIncompatibleDirectory, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	return checkVersion(db)
}

func checkVersion(db *sql.DB) error {
	var app, version int
	if err := db.QueryRow("PRAGMA application_id").Scan(&app); err != nil {
		return fmt.Errorf("%w: %w", ErrIncompatibleDirectory, err)
	}
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("%w: %w", ErrIncompatibleDirectory, err)
	}
	if app != applicationID || version != SchemaVersion {
		return ErrIncompatibleDirectory
	}
	return nil
}

func requireApp(app string) error {
	if !ValidAppID(app) {
		return errors.New("canonical vendor/app identity is required")
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

// HTTPCacheDB returns the writer connection pool for internal/httpcache only.
//
// Transitional: it is removed once the HTTP cache SQL lives in this package.
// No other package may call it. The pool has a single connection, so code
// holding a transaction from it must not call Store methods that write.
func (s *Store) HTTPCacheDB() *sql.DB { return s.db }
