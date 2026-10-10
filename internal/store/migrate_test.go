package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A synthetic three-version schema. Step 1 adds a column; step 2 rebuilds a
// referenced child table (the SQLite procedure that needs foreign keys off)
// and adds an index. Only tests use these steps.
const (
	syntheticV1 = `CREATE TABLE owners(id INTEGER PRIMARY KEY, name TEXT NOT NULL);
CREATE TABLE items(id INTEGER PRIMARY KEY, owner INTEGER NOT NULL REFERENCES owners(id) ON DELETE CASCADE, label TEXT);`
	syntheticItemsV3 = `CREATE TABLE items(
  id INTEGER PRIMARY KEY,
  owner INTEGER NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
  -- Labels are never NULL from schema 3 on.
  label TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0
)`
	syntheticV3 = `CREATE TABLE owners(id INTEGER PRIMARY KEY, name TEXT NOT NULL);
` + syntheticItemsV3 + `;
CREATE INDEX items_owner ON items(owner);`
)

func execStep(statements ...string) func(*sql.Tx) error {
	return func(tx *sql.Tx) error {
		for _, statement := range statements {
			if _, err := tx.Exec(statement); err != nil {
				return err
			}
		}
		return nil
	}
}

func syntheticPlan() migrationPlan {
	return migrationPlan{
		target: 3, minimum: 1, schema: syntheticV3,
		steps: []migrationStep{
			{From: 1, Name: "item size", Up: execStep(`ALTER TABLE items ADD COLUMN size INTEGER NOT NULL DEFAULT 0`)},
			{From: 2, Name: "required labels", Up: execStep(
				strings.Replace(syntheticItemsV3, "items(", "items_new(", 1),
				`INSERT INTO items_new(id, owner, label, size) SELECT id, owner, coalesce(label, ''), size FROM items`,
				`DROP TABLE items`,
				`ALTER TABLE items_new RENAME TO items`,
				`CREATE INDEX items_owner ON items(owner)`,
			)},
		},
		freeSpace: func(string) (uint64, error) { return 1 << 40, nil },
		now:       func() time.Time { return time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC) },
	}
}

var discardLog = slog.New(slog.DiscardHandler)

// syntheticDatabase writes a closed, checkpointed WAL database like the ones
// Open leaves behind, with an orphan-free row in each table.
func syntheticDatabase(t *testing.T, dir, schema string, version int) string {
	t.Helper()
	path := filepath.Join(dir, databaseName)
	db, err := sql.Open("sqlite3", sqliteURL(path, "mode=rwc&_journal_mode=WAL&_foreign_keys=on"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(schema + fmt.Sprintf(`; PRAGMA application_id=%d; PRAGMA user_version=%d;
INSERT INTO owners(id, name) VALUES(1, 'owner'); INSERT INTO items(id, owner) VALUES(7, 1)`, applicationID, version)); err != nil {
		t.Fatal(err)
	}
	return path
}

func headerOf(t *testing.T, path string) (app, version int) {
	t.Helper()
	db, err := sql.Open("sqlite3", sqliteURL(path, "mode=ro"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app, version, err = readHeader(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return app, version
}

func objectsOf(t *testing.T, path string) []schemaObject {
	t.Helper()
	db, err := sql.Open("sqlite3", sqliteURL(path, "mode=ro"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	objects, err := schemaObjects(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return objects
}

func backups(t *testing.T, dir string) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(dir, databaseName+".schema*.backup"))
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestMigrationUpgradesOlderDatabaseToFreshSchema(t *testing.T) {
	plan := syntheticPlan()
	for from := 1; from <= 2; from++ {
		t.Run(fmt.Sprintf("from schema %d", from), func(t *testing.T) {
			dir := t.TempDir()
			old := syntheticV1
			if from == 2 {
				old = strings.Replace(syntheticV1, "label TEXT", "label TEXT, size INTEGER NOT NULL DEFAULT 0", 1)
			}
			path := syntheticDatabase(t, dir, old, from)
			if state, err := inspectDirectory(dir, path, plan); err != nil || state != directoryMigrate {
				t.Fatalf("read-only inspection = %v, %v; want a migratable directory", state, err)
			}
			if err := plan.migrate(dir, path, discardLog); err != nil {
				t.Fatal(err)
			}
			if _, version := headerOf(t, path); version != 3 {
				t.Fatalf("user_version = %d; want 3", version)
			}
			want, err := freshSchemaObjects(context.Background(), syntheticV3)
			if err != nil {
				t.Fatal(err)
			}
			if got := objectsOf(t, path); !reflect.DeepEqual(got, want) {
				t.Fatalf("migrated schema\n%v\nwant\n%v", got, want)
			}
			db, err := sql.Open("sqlite3", sqliteURL(path, "mode=ro"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var label string
			var size int
			if err = db.QueryRow(`SELECT label, size FROM items WHERE id=7 AND owner=1`).Scan(&label, &size); err != nil || label != "" || size != 0 {
				t.Fatalf("migrated row = %q, %d, %v", label, size, err)
			}
			found := backups(t, dir)
			if len(found) != 1 || filepath.Base(found[0]) != fmt.Sprintf("state.sqlite.schema%d-20300102T030405Z.backup", from) {
				t.Fatalf("backups = %v", found)
			}
			if info, err := os.Stat(found[0]); err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("backup mode = %v, %v; want 0600", info.Mode(), err)
			}
			if _, version := headerOf(t, found[0]); version != from {
				t.Fatalf("backup user_version = %d; want %d", version, from)
			}
		})
	}
}

// Refused databases are judged by the read-only probe, and migrate itself
// repeats the check, so neither creates a backup or changes a byte.
func TestMigrationRefusesDowngradeAndTooOldSchema(t *testing.T) {
	newer := syntheticPlan()
	newer.target, newer.steps = 2, newer.steps[:1]
	tooOld := syntheticPlan()
	tooOld.minimum, tooOld.steps = 2, tooOld.steps[1:]
	for name, c := range map[string]struct {
		plan migrationPlan
		want error
	}{
		"database newer than the build": {newer, errSchemaNewer},
		"database below the minimum":    {tooOld, errSchemaTooOld},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := syntheticDatabase(t, dir, syntheticV1, 1)
			if name == "database newer than the build" {
				path = syntheticDatabase(t, t.TempDir(), syntheticV3, 3)
				dir = filepath.Dir(path)
			}
			before := snapshotFiles(t, dir)
			if _, err := inspectDirectory(dir, path, c.plan); !errors.Is(err, c.want) || !errors.Is(err, ErrIncompatibleDirectory) {
				t.Fatalf("inspection = %v; want %v", err, c.want)
			}
			if err := c.plan.migrate(dir, path, discardLog); !errors.Is(err, c.want) {
				t.Fatalf("migrate = %v; want %v", err, c.want)
			}
			if after := snapshotFiles(t, dir); !reflect.DeepEqual(before, after) {
				t.Fatal("refusal changed the data directory")
			}
		})
	}
}

// Before 1.0 the production plan has no steps and refuses every other version
// with the plain error, exactly as without a framework.
func TestPlanWithoutMinimumRefusesOtherVersions(t *testing.T) {
	plan := syntheticPlan()
	plan.minimum, plan.steps = 0, nil
	for _, version := range []int{1, 4} {
		if _, err := plan.classify(applicationID, version); err != ErrIncompatibleDirectory {
			t.Fatalf("classify(%d) = %v; want the plain ErrIncompatibleDirectory", version, err)
		}
	}
	if err := productionPlan().validate(); err != nil {
		t.Fatalf("production migration plan: %v", err)
	}
}

func TestMigrationPlanRequiresContiguousSteps(t *testing.T) {
	gap := syntheticPlan()
	gap.steps = []migrationStep{gap.steps[1], gap.steps[0]}
	missing := syntheticPlan()
	missing.steps = missing.steps[:1]
	disabled := syntheticPlan()
	disabled.minimum = 0
	for name, plan := range map[string]migrationPlan{"out of order": gap, "missing step": missing, "steps without minimum": disabled} {
		if err := plan.validate(); err == nil {
			t.Errorf("%s: plan accepted", name)
		}
	}
}

// Every failure after the backup rolls the transaction back: the database file
// stays byte-identical and keeps its version.
func TestMigrationFailureLeavesDatabaseUnchanged(t *testing.T) {
	failing := syntheticPlan()
	failing.steps[1].Up = func(tx *sql.Tx) error {
		if err := execStep(`DROP TABLE items`)(tx); err != nil {
			return err
		}
		return errors.New("injected step failure")
	}
	orphan := syntheticPlan()
	orphan.steps[0].Up = execStep(`ALTER TABLE items ADD COLUMN size INTEGER NOT NULL DEFAULT 0`, `INSERT INTO items(id, owner) VALUES(8, 99)`)
	incomplete := syntheticPlan()
	incomplete.steps[1].Up = execStep(`UPDATE items SET label='' WHERE label IS NULL`)
	full := syntheticPlan()
	full.freeSpace = func(string) (uint64, error) { return 1024, nil }
	for name, c := range map[string]struct {
		plan   migrationPlan
		backup bool
		want   error
	}{
		"step error":                {failing, true, nil},
		"foreign key violation":     {orphan, true, nil},
		"schema differs from fresh": {incomplete, true, nil},
		"not enough disk space":     {full, false, errMigrationDisk},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := syntheticDatabase(t, dir, syntheticV1, 1)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			err = c.plan.migrate(dir, path, discardLog)
			if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
				t.Fatalf("migrate = %v; want failure %v", err, c.want)
			}
			if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
				t.Fatalf("failed migration changed the database file: %v", err)
			}
			if got := len(backups(t, dir)); got != map[bool]int{false: 0, true: 1}[c.backup] {
				t.Fatalf("%d backups after failure", got)
			}
			if _, version := headerOf(t, path); version != 1 {
				t.Fatalf("user_version = %d after failure", version)
			}
		})
	}
}

func TestMigrationNeverOverwritesBackup(t *testing.T) {
	dir := t.TempDir()
	path := syntheticDatabase(t, dir, syntheticV1, 1)
	plan := syntheticPlan()
	existing := filepath.Join(dir, "state.sqlite.schema1-20300102T030405Z.backup")
	if err := os.WriteFile(existing, []byte("earlier backup"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := plan.migrate(dir, path, discardLog); err == nil {
		t.Fatal("migration overwrote an existing backup name")
	}
	if body, err := os.ReadFile(existing); err != nil || string(body) != "earlier backup" {
		t.Fatalf("existing backup changed: %q, %v", body, err)
	}
}

// TestMigrationCrashHelper is the child process of
// TestMigrationCrashKeepsOriginalAndBackup: it exits in the middle of a step.
func TestMigrationCrashHelper(t *testing.T) {
	dir := os.Getenv("REDAPP_MIGRATION_CRASH_DIR")
	if dir == "" {
		return
	}
	plan := syntheticPlan()
	plan.steps[1].Up = func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DROP TABLE items`); err != nil {
			os.Exit(2)
		}
		os.Exit(3)
		return nil
	}
	plan.migrate(dir, filepath.Join(dir, databaseName), discardLog)
	os.Exit(4)
}

func TestMigrationCrashKeepsOriginalAndBackup(t *testing.T) {
	dir := t.TempDir()
	path := syntheticDatabase(t, dir, syntheticV1, 1)
	cmd := exec.Command(os.Args[0], "-test.run=^TestMigrationCrashHelper$")
	cmd.Env = append(os.Environ(), "REDAPP_MIGRATION_CRASH_DIR="+dir)
	var exit *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("helper = %v; want exit 3 in the middle of a step", err)
	}
	if found := backups(t, dir); len(found) != 1 {
		t.Fatalf("backups after crash = %v", found)
	}
	if _, version := headerOf(t, path); version != 1 {
		t.Fatalf("user_version after crash = %d; want 1", version)
	}
	plan := syntheticPlan()
	plan.now = func() time.Time { return time.Date(2030, 1, 2, 3, 5, 0, 0, time.UTC) }
	if err := plan.migrate(dir, path, discardLog); err != nil {
		t.Fatalf("migration after crash: %v", err)
	}
	if _, version := headerOf(t, path); version != 3 {
		t.Fatalf("user_version = %d; want 3", version)
	}
}

// End to end on the real schema: a database that lacks one index of the
// current schema and carries the previous version number is migrated by a
// test-only step when the plan allows it.
func TestOpenMigratesMigratableDirectory(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveSiteSettings(1, map[string]string{"title": "kept"}); err != nil {
		t.Fatal(err)
	}
	var index, definition string
	if err = s.db.QueryRow(`SELECT name, sql FROM sqlite_schema WHERE type='index' AND sql IS NOT NULL ORDER BY name LIMIT 1`).Scan(&index, &definition); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`DROP INDEX "` + index + `"; PRAGMA user_version=` + strconv.Itoa(SchemaVersion-1)); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = Preflight(dir); !errors.Is(err, ErrIncompatibleDirectory) {
		t.Fatalf("production Preflight = %v; want refusal before 1.0", err)
	}
	plan := productionPlan()
	plan.minimum = SchemaVersion - 1
	plan.steps = []migrationStep{{From: SchemaVersion - 1, Name: "restore index", Up: execStep(definition)}}
	if err = preflight(dir, plan); err != nil {
		t.Fatalf("Preflight with the migration enabled = %v", err)
	}
	s, err = Open(dir, func(s *Store) { s.migrations = plan })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := schemaObjects(context.Background(), s.read)
	if err != nil {
		t.Fatal(err)
	}
	want, err := freshSchemaObjects(context.Background(), schema)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("migrated schema differs from a fresh one")
	}
	var site map[string]string
	if _, err = s.ReadSiteSettings(&site); err != nil || site["title"] != "kept" {
		t.Fatalf("site settings after migration = %v, %v", site, err)
	}
	if found := backups(t, dir); len(found) != 1 {
		t.Fatalf("backups = %v", found)
	}
	if err = preflight(dir, plan); err != nil {
		t.Fatalf("Preflight after migration = %v", err)
	}
}

func TestNormalizeSQLIgnoresLayoutOnly(t *testing.T) {
	for _, pair := range [][2]string{
		{"CREATE TABLE t(a INTEGER, b TEXT)", "CREATE TABLE \"t\" (\n  a INTEGER, -- first\n  b TEXT /* second */\n)"},
		{"CHECK(a IN ('x','y'))", "CHECK (a IN ( 'x' , 'y' ))"},
		{"DEFAULT 'a  -- b'", "DEFAULT 'a  -- b'"},
		{`CREATE TABLE [t](a)`, "CREATE TABLE t(a)"},
	} {
		if a, b := normalizeSQL(pair[0]), normalizeSQL(pair[1]); a != b {
			t.Errorf("%q != %q", a, b)
		}
	}
	for _, pair := range [][2]string{
		{"DEFAULT 'a b'", "DEFAULT 'a  b'"},
		{"a TEXT NOT NULL", "a TEXT"},
		{`CREATE TABLE "my table"(a)`, `CREATE TABLE my table(a)`},
	} {
		if normalizeSQL(pair[0]) == normalizeSQL(pair[1]) {
			t.Errorf("%q and %q compare equal", pair[0], pair[1])
		}
	}
}
