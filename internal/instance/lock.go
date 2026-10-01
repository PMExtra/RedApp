// Package instance owns the data-directory kernel lock for the process lifetime.
package instance

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

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
	f, err := os.OpenFile(filepath.Join(real, "instance.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("Instance lock must be a regular file")
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("Data directory is already owned by another instance: %w", err)
	}
	return &Guard{f, real}, nil
}
func (g *Guard) Close() error { return g.File.Close() } // Never unlink the lock inode.
