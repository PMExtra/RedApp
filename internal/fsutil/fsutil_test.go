package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDirCreatesAndRejectsNonDirectories(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "objects")
	for range 2 {
		if err := EnsureDir(dir); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory not created with 0700: %v %v", info, err)
	}
	file := filepath.Join(root, "file")
	if err = os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err = os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, link} {
		if err := EnsureDir(path); !errors.Is(err, ErrNotDirectory) {
			t.Fatalf("%s accepted as directory: %v", path, err)
		}
	}
}

func TestOpenRegularIsReadOnlyAndRefusesLinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenRegular(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("x")); err == nil {
		t.Fatal("read-only open accepted a write")
	}
	f.Close()
	w, err := OpenRegularWritable(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.WriteAt([]byte("C"), 0); err != nil {
		t.Fatal(err)
	}
	w.Close()
	link := filepath.Join(dir, "link")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{link, dir} {
		if _, err = OpenRegular(p); !errors.Is(err, ErrNotRegular) {
			t.Fatalf("%s opened as a regular file: %v", p, err)
		}
	}
	if _, err = OpenRegular(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file error not preserved: %v", err)
	}
}

func TestStagedPublishAndDiscard(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "final")
	staged, err := CreateStaged(filepath.Join(dir, "final.part"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = staged.Write([]byte("complete")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("staged content visible before publication")
	}
	if err = staged.Publish(target); err != nil {
		t.Fatal(err)
	}
	if err = staged.Discard(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "complete" {
		t.Fatalf("published content %q %v", got, err)
	}
	if _, err = CreateStaged(target); err == nil {
		t.Fatal("staging file overwrote an existing file")
	}

	abandoned, err := CreateStaged(filepath.Join(dir, "abandoned.part"))
	if err != nil {
		t.Fatal(err)
	}
	if err = abandoned.Discard(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "final" {
		t.Fatalf("discard left files behind: %v %v", entries, err)
	}
}

func TestWriteFileReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings")
	for _, content := range []string{"first", "second"} {
		if err := WriteFile(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(path); err != nil || string(got) != content {
			t.Fatalf("content %q %v", got, err)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v %v", info, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("staging files left behind: %v", entries)
	}
}

func TestRemoveReportsExistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if removed, err := Remove(path); err != nil || !removed {
		t.Fatalf("existing file: %v %v", removed, err)
	}
	if removed, err := Remove(path); err != nil || removed {
		t.Fatalf("missing file: %v %v", removed, err)
	}
}

func TestRandomIDIsHex(t *testing.T) {
	a, err := RandomID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := RandomID()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 || a == b {
		t.Fatalf("identifiers %q %q", a, b)
	}
	for _, c := range a {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			t.Fatalf("non-hex identifier %q", a)
		}
	}
}
