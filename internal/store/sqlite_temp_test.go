package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/instance"
)

// SQLite caches the environment during its first initialization. Each scenario
// must start a real new process, including when the parent already used SQLite.
func TestSQLiteTemporaryStartup(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{0, 15, 16, SchemaVersion} {
		dir := t.TempDir()
		if version != 0 {
			loadSchemaFixture(t, dir, fixturePath(version))
		}
		command := exec.Command(executable, "-test.run=^TestSQLiteTemporaryStartupChild$", "-test.v")
		command.Env = tempChildEnvironment(dir)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("schema %d: %v\n%s", version, err, output)
		}
	}
}

func tempChildEnvironment(dir string) []string {
	env := []string{}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "SQLITE_TMPDIR=") && !strings.HasPrefix(value, "REDAPP_SQLITE_TEST_DIR=") {
			env = append(env, value)
		}
	}
	return append(env, "REDAPP_SQLITE_TEST_DIR="+dir)
}

func TestSQLiteTemporaryStartupChild(t *testing.T) {
	dir := os.Getenv("REDAPP_SQLITE_TEST_DIR")
	if dir == "" {
		return
	}
	if err := Preflight(dir); err != nil {
		t.Fatal(err)
	}
	guard, err := instance.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	path, err := PrepareSQLiteTemporaryDirectory(guard.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "tmp", "sqlite") {
		t.Fatal(path)
	}
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = PrepareSQLiteTemporaryDirectory(t.TempDir()); err == nil {
		t.Fatal("process-wide path was reconfigured")
	}
	// A second store does not replace the live process's temporary directory.
	other, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
	fixture, err := os.ReadFile("testdata/metric_spill.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.Exec(string(fixture)); err != nil {
		t.Fatal(err)
	}
	var at int64
	if err = db.read.QueryRow("SELECT aggregated_before_s+3600 FROM metric_history_state").Scan(&at); err != nil {
		t.Fatal(err)
	}
	if err = db.MaintainMetrics(time.Unix(at, 0), []MetricDefinition{{Key: "counters.artifact_requests", Kind: MetricCounter}}); err != nil {
		t.Fatal(err)
	}
	var watermark int64
	if err = db.read.QueryRow("SELECT aggregated_before_s FROM metric_history_state").Scan(&watermark); err != nil || watermark != at {
		t.Fatal(watermark, err)
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatal("temporary files leaked", entries, err)
	}
}

func TestFreshSQLiteTemporarySkeleton(t *testing.T) {
	for _, bad := range []string{"", "unexpected", "sqlite/leftover"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "tmp", "sqlite")
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if bad != "" {
			if err := os.WriteFile(filepath.Join(dir, "tmp", bad), []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		err := Preflight(dir)
		if (err == nil) != (bad == "") {
			t.Fatalf("%q: %v", bad, err)
		}
	}
}
