// Package fsutil holds the durable-file primitives shared by the storage
// packages: directory creation, regular-file opening, staged publication by
// rename, removal and random identifiers.
//
// Paths are ordinary absolute paths below the locked data directory. Every
// directory is checked to be a real directory when it is created or opened,
// and every file open refuses symlinks, non-regular files and a file replaced
// between inspection and open. The data directory itself is owned by the
// instance lock and has mode 0700, so no other process is expected to race
// these checks; they reject accidental or stale links rather than defend
// against a hostile local user.
package fsutil

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var (
	// ErrNotDirectory reports a storage directory that is a symlink or a file.
	ErrNotDirectory = errors.New("path is not a real directory")
	// ErrNotRegular reports a storage file that is a symlink, directory or device.
	ErrNotRegular = errors.New("path is not a regular file")
	// ErrChanged reports a file replaced between inspection and open.
	ErrChanged = errors.New("file changed while opening")
)

// EnsureDir creates path with mode 0700 if it is missing and rejects anything
// other than a real directory. A newly created entry is made durable in its
// parent, so callers create each level in order from the top.
func EnsureDir(path string) error {
	created := false
	err := os.Mkdir(path, 0o700)
	switch {
	case err == nil:
		created = true
	case !errors.Is(err, fs.ErrExist):
		return fmt.Errorf("create directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: %w", filepath.Base(path), ErrNotDirectory)
	}
	if created {
		return SyncDir(filepath.Dir(path))
	}
	return nil
}

// SyncDir makes creations, renames and removals in a directory durable.
func SyncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer f.Close()
	if err = f.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}

// OpenRegular opens an existing regular file read-only.
func OpenRegular(path string) (*os.File, error) { return openRegular(path, os.O_RDONLY) }

// OpenRegularWritable opens an existing regular file for reading and writing,
// for example to resume a partially downloaded file in place.
func OpenRegularWritable(path string) (*os.File, error) { return openRegular(path, os.O_RDWR) }

func openRegular(path string, flag int) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), ErrNotRegular)
	}
	f, err := os.OpenFile(path, flag, 0)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		f.Close()
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), ErrChanged)
	}
	return f, nil
}

// Remove deletes a file and reports whether it existed. A missing file is
// not an error. The caller syncs the directory when the removal must be durable
// before a following step.
func Remove(path string) (bool, error) {
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("remove file: %w", err)
	}
	return true, nil
}

// RandomID returns 32 lowercase hexadecimal characters from crypto/rand.
func RandomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate random identifier: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Staged is a new file that becomes visible under its final name only when it
// is published. It is created in the destination directory so that the final
// rename never crosses filesystems.
type Staged struct {
	file      *os.File
	path      string
	published bool
}

// CreateStaged exclusively creates path with mode 0600. The caller chooses the
// name so that its own recovery can recognize abandoned staging files.
func CreateStaged(path string) (*Staged, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create staging file: %w", err)
	}
	return &Staged{file: f, path: path}, nil
}

// Path is the staging file's current name.
func (s *Staged) Path() string { return s.path }

func (s *Staged) Write(p []byte) (int, error) {
	if s.file == nil {
		return 0, os.ErrClosed
	}
	return s.file.Write(p)
}

// Seal flushes the content to stable storage and closes the file. Publishing
// seals implicitly; sealing early lets a caller finish I/O before taking a lock.
func (s *Staged) Seal() error {
	if s.file == nil {
		return nil
	}
	f := s.file
	s.file = nil
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync staging file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close staging file: %w", err)
	}
	return nil
}

// Publish seals the file, renames it to target and makes the rename durable.
// After a successful Publish, Discard no longer removes anything.
func (s *Staged) Publish(target string) error {
	if s.published {
		return errors.New("staging file already published")
	}
	if err := s.Seal(); err != nil {
		return err
	}
	if err := os.Rename(s.path, target); err != nil {
		return fmt.Errorf("publish staging file: %w", err)
	}
	s.published = true
	source := s.path
	s.path = target
	if err := SyncDir(filepath.Dir(target)); err != nil {
		return err
	}
	if filepath.Dir(source) != filepath.Dir(target) {
		return SyncDir(filepath.Dir(source))
	}
	return nil
}

// Discard closes and removes an unpublished staging file. It is safe to call
// more than once and after Publish, so callers can defer it unconditionally.
func (s *Staged) Discard() error {
	if s.file != nil {
		s.file.Close()
		s.file = nil
	}
	if s.published {
		return nil
	}
	_, err := Remove(s.path)
	return err
}

// WriteFile atomically creates or replaces path with data: a staging file in
// the same directory is written, synced, renamed over path, and the directory
// is synced. Readers observe either the old file or the complete new one.
func WriteFile(path string, data []byte) error {
	id, err := RandomID()
	if err != nil {
		return err
	}
	staged, err := CreateStaged(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-"+id))
	if err != nil {
		return err
	}
	defer staged.Discard()
	if _, err = staged.Write(data); err != nil {
		return fmt.Errorf("write staging file: %w", err)
	}
	return staged.Publish(path)
}
