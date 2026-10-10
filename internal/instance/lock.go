// Package instance owns the data-directory kernel lock for the process lifetime.
package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockName is the lock file inside a data directory. It is the only file a
// data directory may contain before the database is created.
const LockName = "instance.lock"

var errLockNotRegular = errors.New("Instance lock must be a regular file")

type Guard struct {
	File      *os.File
	Directory string
}

func Acquire(dir string) (*Guard, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(real, LockName), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, errLockNotRegular
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("Data directory is already owned by another instance: %w", err)
	}
	return &Guard{f, real}, nil
}
func (g *Guard) Close() error { return g.File.Close() } // Never unlink the lock inode.

// Check fails when a live process holds the lock of dir. Unlike Acquire it
// neither creates the directory nor the lock file, so it can run before a
// read-only inspection of a directory that must stay unmodified.
func Check(dir string) error {
	f, err := os.OpenFile(filepath.Join(dir, LockName), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errLockNotRegular
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("Data directory is already owned by another instance: %w", err)
	}
	return nil
}
