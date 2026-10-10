package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-sqlite3"

	"github.com/PMExtra/RedApp/internal/fsutil"
)

// MinimumMigratableVersion is the oldest schema version that Open upgrades in
// place. Zero disables migrations: before 1.0 every other version is refused
// unchanged (ADR 0001). Releasing 1.0 sets it to that release's SchemaVersion;
// see "Schema migrations" in docs/dev/development.md.
const MinimumMigratableVersion = 0

// migrations are the production steps in order: migrations[i] upgrades schema
// MinimumMigratableVersion+i to the next version. Empty before 1.0.
var migrations = []migrationStep{}

// migrationStep upgrades schema From to From+1 inside the migration
// transaction. Foreign key enforcement is off, so a step may rebuild tables
// (create new, copy, drop old, rename); foreign_key_check runs after the last
// step. Steps must not commit, change PRAGMA user_version or touch files.
type migrationStep struct {
	From int
	Name string
	Up   func(tx *sql.Tx) error
}

// migrationPlan is everything Open needs to judge and upgrade an existing
// database. Production uses productionPlan; tests build plans with synthetic
// steps and schemas.
type migrationPlan struct {
	target  int // version written by the build, SchemaVersion
	minimum int // oldest upgradable version; 0 disables migrations
	steps   []migrationStep
	schema  string // fresh schema the migrated database must equal
	// freeSpace reports the bytes available to an unprivileged writer in dir.
	freeSpace func(dir string) (uint64, error)
	now       func() time.Time
}

func productionPlan() migrationPlan {
	return migrationPlan{target: SchemaVersion, minimum: MinimumMigratableVersion, steps: migrations, schema: schema, freeSpace: availableBytes, now: time.Now}
}

func availableBytes(dir string) (uint64, error) {
	var disk syscall.Statfs_t
	if err := syscall.Statfs(dir, &disk); err != nil {
		return 0, err
	}
	return disk.Bavail * uint64(disk.Bsize), nil
}

var (
	errSchemaNewer   = errors.New("the database was written by a newer RedApp; downgrades are not supported")
	errSchemaTooOld  = errors.New("the database schema is older than the oldest version this build can migrate; upgrade through an intermediate release first")
	errMigrationDisk = errors.New("not enough free disk space in the data directory for the pre-migration backup and the migration")
)

// validate checks that the steps upgrade minimum to target one version at a time.
func (p migrationPlan) validate() error {
	if p.minimum == 0 {
		if len(p.steps) != 0 {
			return errors.New("migration steps require a minimum migratable version")
		}
		return nil
	}
	if p.minimum < 1 || p.minimum > p.target || len(p.steps) != p.target-p.minimum {
		return fmt.Errorf("migration steps must upgrade schema %d to %d one version at a time", p.minimum, p.target)
	}
	for i, step := range p.steps {
		if step.From != p.minimum+i || step.Up == nil || step.Name == "" {
			return fmt.Errorf("migration step %d is out of order or incomplete", i)
		}
	}
	return nil
}

// classify reports whether a database with these header values is current
// (false, nil), needs migration (true, nil) or must be refused.
func (p migrationPlan) classify(app, version int) (bool, error) {
	switch {
	case app != applicationID:
		return false, ErrIncompatibleDirectory
	case version == p.target:
		return false, nil
	case p.minimum == 0:
		// Before 1.0 every other version is refused with the plain error.
		return false, ErrIncompatibleDirectory
	case version > p.target:
		return false, fmt.Errorf("%w: %w (database schema %d, this build %d)", ErrIncompatibleDirectory, errSchemaNewer, version, p.target)
	case version < p.minimum:
		return false, fmt.Errorf("%w: %w (database schema %d, oldest migratable %d)", ErrIncompatibleDirectory, errSchemaTooOld, version, p.minimum)
	}
	return true, nil
}

// rowQuerier is a connection, pool or transaction that runs single-row queries.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func readHeader(ctx context.Context, db rowQuerier) (app, version int, err error) {
	if err = db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil {
		return 0, 0, fmt.Errorf("%w: %w", ErrIncompatibleDirectory, err)
	}
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, 0, fmt.Errorf("%w: %w", ErrIncompatibleDirectory, err)
	}
	return app, version, nil
}

// migrate upgrades the database at path in place. The caller holds the
// instance lock and no other connection is open. It first writes a backup
// next to the database with VACUUM INTO, then runs every step, the foreign key
// check, the schema comparison and the version update in one transaction: a
// failure or crash leaves the original database as it was, plus the backup.
func (p migrationPlan) migrate(dir, path string, log *slog.Logger) error {
	if err := p.validate(); err != nil {
		return err
	}
	ctx := context.Background()
	// _foreign_keys=off: the pragma is a no-op inside a transaction, so it
	// must be off on the connection before the migration transaction begins.
	db, err := sql.Open("sqlite3", sqliteURL(path, "mode=rw&_journal_mode=WAL&_txlock=immediate&_foreign_keys=off&_synchronous=FULL&_busy_timeout=5000"))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// The read-only probe saw the main file; the WAL view decides.
	app, from, err := readHeader(ctx, conn)
	if err != nil {
		return err
	}
	if upgrade, err := p.classify(app, from); !upgrade {
		return err
	}
	var enforced bool
	if err = conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enforced); err != nil {
		return err
	}
	if enforced {
		return errors.New("foreign key enforcement must be off while migrating")
	}
	backup, err := p.backup(ctx, conn, dir, from)
	if err != nil {
		return err
	}
	log.Info("migrating state database", "from_schema", from, "to_schema", p.target, "backup", backup)
	if err = p.apply(ctx, conn, from); err != nil {
		return fmt.Errorf("migrate state database from schema %d to %d (unchanged; backup %s): %w", from, p.target, backup, err)
	}
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		return err
	}
	// Like createSchema: the immutable probe of the next start reads only the
	// main file, so the new version must not stay in the WAL.
	var busy, frames, checkpointed int
	if err = conn.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed); err != nil {
		return err
	}
	if busy != 0 {
		return errors.New("migrated schema checkpoint is busy")
	}
	log.Info("state database migrated", "from_schema", from, "to_schema", p.target, "backup", backup)
	return nil
}

// backup writes a consistent copy of the database to a new sibling file named
// state.sqlite.schema<from>-<UTC time>.backup. It never overwrites a file and
// removes its own partial copy on failure.
func (p migrationPlan) backup(ctx context.Context, conn *sql.Conn, dir string, from int) (string, error) {
	var pages, free, size uint64
	for pragma, value := range map[string]*uint64{"page_count": &pages, "freelist_count": &free, "page_size": &size} {
		if err := conn.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(value); err != nil {
			return "", err
		}
	}
	// One copy for the backup and up to one more for the migration's WAL.
	need := 2 * (pages - min(free, pages)) * size
	if available, err := p.freeSpace(dir); err != nil {
		return "", fmt.Errorf("check free space for the pre-migration backup: %w", err)
	} else if available < need {
		return "", fmt.Errorf("%w (need %d bytes, available %d)", errMigrationDisk, need, available)
	}
	name := filepath.Join(dir, fmt.Sprintf("%s.schema%d-%s.backup", databaseName, from, p.now().UTC().Format("20060102T150405Z")))
	// VACUUM INTO accepts an empty file; creating it first sets the
	// permissions and refuses to overwrite an existing backup.
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", fmt.Errorf("create pre-migration backup: %w", err)
	}
	if err = f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	if _, err = conn.ExecContext(ctx, "VACUUM INTO ?", name); err != nil {
		os.Remove(name)
		if diskFull(err) {
			return "", fmt.Errorf("%w: %w", errMigrationDisk, err)
		}
		return "", fmt.Errorf("write pre-migration backup: %w", err)
	}
	if err = syncFile(name); err != nil {
		return "", err
	}
	if err = fsutil.SyncDir(dir); err != nil {
		return "", err
	}
	return name, nil
}

func diskFull(err error) bool {
	var e sqlite3.Error
	return errors.Is(err, syscall.ENOSPC) || (errors.As(err, &e) && e.Code == sqlite3.ErrFull)
}

func syncFile(path string) error {
	f, err := fsutil.OpenRegularWritable(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

// apply runs the steps from version from to the target in one transaction and
// commits only a database whose foreign keys hold and whose schema equals a
// freshly created one.
func (p migrationPlan) apply(ctx context.Context, conn *sql.Conn, from int) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, step := range p.steps[from-p.minimum:] {
		if err = step.Up(tx); err != nil {
			return fmt.Errorf("step %d %s: %w", step.From, step.Name, err)
		}
	}
	if err = foreignKeyCheck(ctx, tx); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", p.target)); err != nil {
		return err
	}
	got, err := schemaObjects(ctx, tx)
	if err != nil {
		return err
	}
	want, err := freshSchemaObjects(ctx, p.schema)
	if err != nil {
		return err
	}
	if err = compareSchemas(got, want); err != nil {
		return err
	}
	return tx.Commit()
}

func foreignKeyCheck(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var index int
		if err = rows.Scan(&table, &rowid, &parent, &index); err != nil {
			return err
		}
		return fmt.Errorf("foreign key check failed: a row of %s references a missing %s row", table, parent)
	}
	return rows.Err()
}

// schemaObject is one entry of sqlite_schema with its SQL normalized.
type schemaObject struct{ Type, Name, Table, SQL string }

func (o schemaObject) String() string { return o.Type + " " + o.Name }

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// schemaObjects lists the user schema in a stable order. SQLite's internal
// objects (sqlite_sequence, sqlite_stat*, automatic indexes) are excluded:
// they follow from the listed definitions or from ANALYZE.
func schemaObjects(ctx context.Context, db queryer) ([]schemaObject, error) {
	rows, err := db.QueryContext(ctx, `SELECT type, name, tbl_name, sql FROM sqlite_schema WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY type, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []schemaObject
	for rows.Next() {
		var o schemaObject
		if err = rows.Scan(&o.Type, &o.Name, &o.Table, &o.SQL); err != nil {
			return nil, err
		}
		o.SQL = normalizeSQL(o.SQL)
		out = append(out, o)
	}
	return out, rows.Err()
}

// freshSchemaObjects creates schema in a private in-memory database.
func freshSchemaObjects(ctx context.Context, schema string) ([]schemaObject, error) {
	db, err := sql.Open("sqlite3", "file::memory:?mode=memory")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, schema); err != nil {
		return nil, fmt.Errorf("create reference schema: %w", err)
	}
	return schemaObjects(ctx, db)
}

func compareSchemas(got, want []schemaObject) error {
	index := map[string]schemaObject{}
	for _, o := range got {
		index[o.String()] = o
	}
	var diff []string
	for _, w := range want {
		g, ok := index[w.String()]
		delete(index, w.String())
		if !ok {
			diff = append(diff, "missing "+w.String())
		} else if g != w {
			diff = append(diff, "different "+w.String())
		}
	}
	for name := range index {
		diff = append(diff, "unexpected "+name)
	}
	if len(diff) != 0 {
		return fmt.Errorf("migrated schema differs from a fresh schema: %s", strings.Join(diff, ", "))
	}
	return nil
}

// normalizeSQL removes comments, insignificant whitespace and quotes around
// plain identifiers outside string literals, so that equivalent definitions
// written with different layout, or rewritten by ALTER TABLE, compare equal.
func normalizeSQL(text string) string {
	var out strings.Builder
	space := false
	write := func(token string) {
		if space && out.Len() > 0 {
			last := out.String()[out.Len()-1]
			if !strings.ContainsRune("(,", rune(last)) && !strings.ContainsRune("(),", rune(token[0])) {
				out.WriteByte(' ')
			}
		}
		space = false
		out.WriteString(token)
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '-' && i+1 < len(text) && text[i+1] == '-':
			for i < len(text) && text[i] != '\n' {
				i++
			}
			space = true
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				i = len(text)
			} else {
				i += end + 3
			}
			space = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			space = true
		case c == '\'' || c == '"' || c == '`' || c == '[':
			closing := c
			if c == '[' {
				closing = ']'
			}
			j := i + 1
			for j < len(text) {
				if text[j] == closing {
					// A doubled quote is an escaped quote inside the token.
					if closing != ']' && j+1 < len(text) && text[j+1] == closing {
						j += 2
						continue
					}
					break
				}
				j++
			}
			end := min(j+1, len(text))
			token := text[i:end]
			if c != '\'' && plainIdentifier(text[i+1:end-1]) {
				token = text[i+1 : end-1]
			}
			write(token)
			i = end - 1
		default:
			write(string(c))
		}
	}
	return out.String()
}

func plainIdentifier(name string) bool {
	for i, c := range name {
		if c != '_' && !('a' <= c && c <= 'z') && !('A' <= c && c <= 'Z') && (i == 0 || !('0' <= c && c <= '9')) {
			return false
		}
	}
	return name != ""
}
