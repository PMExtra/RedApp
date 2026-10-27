package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/PMExtra/RedApp/internal/fsutil"
)

// Startup-only serialization includes directory checks and one tiny probe
// write/fsync, before workers or SQLite connections may exist.
var sqliteTemporaryStartup struct {
	sync.Mutex
	configured bool
}

// PrepareSQLiteTemporaryDirectory is process startup configuration, not a Store
// option. Call once, after Preflight and acquiring the instance lock but before
// ANY SQLite connection (including migrations). SQLite caches environment
// pointers when its VFS initializes; changing them while connections exist is
// unsafe. Multiple stores in one process share this single temporary directory.
// Config validation and health checks do not call this function.
func PrepareSQLiteTemporaryDirectory(dataDir string) (string, error) {
	sqliteTemporaryStartup.Lock()
	defer sqliteTemporaryStartup.Unlock()
	if sqliteTemporaryStartup.configured {
		return "", errors.New("SQLite temporary directory is already configured for this process")
	}
	path, explicit := os.LookupEnv("SQLITE_TMPDIR")
	if explicit {
		if path == "" || !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
			return "", errors.New("SQLITE_TMPDIR must name an existing writable absolute directory")
		}
	} else {
		path = filepath.Join(dataDir, "tmp", "sqlite")
		for _, dir := range []string{filepath.Dir(path), path} {
			if err := fsutil.EnsureDir(dir); err != nil {
				return "", fmt.Errorf("prepare SQLite temporary directory: %w", err)
			}
			info, err := os.Lstat(dir)
			if err != nil {
				return "", fmt.Errorf("inspect SQLite temporary directory: %w", err)
			}
			if info.Mode().Perm() != 0700 {
				return "", fmt.Errorf("SQLite temporary directory %q must have mode 0700", dir)
			}
		}
	}
	// Do not create or chmod an explicitly selected external directory. Resolve
	// links once, then publish the verified absolute path to SQLite.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve SQLite temporary directory: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect SQLite temporary directory: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("SQLite temporary path is not a directory")
	}
	id, err := fsutil.RandomID()
	if err != nil {
		return "", fmt.Errorf("name SQLite temporary probe: %w", err)
	}
	probe, err := fsutil.CreateStaged(filepath.Join(resolved, ".sqlite-probe-"+id))
	if err != nil {
		return "", fmt.Errorf("create SQLite temporary probe: %w", err)
	}
	// Like SQLite's Unix temporary files, unlink before use so even an abrupt
	// process exit cannot leave a probe that blocks the next fresh startup.
	if _, err = fsutil.Remove(probe.Path()); err != nil {
		return "", fmt.Errorf("unlink SQLite temporary probe: %w", errors.Join(err, probe.Discard()))
	}
	_, writeErr := probe.Write([]byte{0})
	sealErr := probe.Seal()
	discardErr := probe.Discard()
	if err = errors.Join(writeErr, sealErr, discardErr); err != nil {
		return "", fmt.Errorf("write SQLite temporary probe: %w", err)
	}
	if err = os.Setenv("SQLITE_TMPDIR", resolved); err != nil {
		return "", fmt.Errorf("configure SQLite temporary directory: %w", err)
	}
	sqliteTemporaryStartup.configured = true
	return resolved, nil
}

// A process can stop after preparing temp directories but before creating the
// database. Only this empty, private directory skeleton is safe to initialize;
// unrelated files, symlinks and nonempty temp directories still refuse startup.
func emptySQLiteTemporaryTree(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() != "sqlite" {
			return false
		}
		child := filepath.Join(path, entry.Name())
		info, err = os.Lstat(child)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			return false
		}
		contents, err := os.ReadDir(child)
		if err != nil || len(contents) != 0 {
			return false
		}
	}
	return true
}
