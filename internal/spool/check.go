package spool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"

	"github.com/PMExtra/RedApp/internal/fsutil"
)

type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// HashMatches reports whether the first n bytes of f have the expected
// SHA-256. A non-nil error means the check was cancelled and proves nothing;
// unreadable or short content is reported as a mismatch.
func HashMatches(ctx context.Context, f io.ReaderAt, n int64, expected string) (bool, error) {
	h := sha256.New()
	copied, err := io.Copy(h, contextReader{ctx, io.NewSectionReader(f, 0, n)})
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	return err == nil && copied == n && hex.EncodeToString(h.Sum(nil)) == expected, nil
}

// FileCheck is a whole-file verification bound to the inode it read.
type FileCheck struct {
	info  os.FileInfo
	Valid bool
}

// CheckFile hashes a stored file without any owner lock. A nil result with a
// nil error means the path could not be opened as a regular file.
func CheckFile(ctx context.Context, path string, n int64, expected string) (*FileCheck, error) {
	f, err := fsutil.OpenRegular(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, nil
	}
	valid := false
	if st.Size() == n {
		if valid, err = HashMatches(ctx, f, n, expected); err != nil {
			return nil, err
		}
	}
	return &FileCheck{info: st, Valid: valid}, nil
}

// Matches reports whether st is still the exact inode, size and modification
// time that was hashed, so the result may be applied.
func (c *FileCheck) Matches(st os.FileInfo) bool {
	return c != nil && st != nil && os.SameFile(c.info, st) && st.Size() == c.info.Size() && st.ModTime().Equal(c.info.ModTime())
}

// Checks coalesces in-flight verifications by key. The owner's lock guards it:
// verification work runs without that lock and commits under it, so a waiter
// that gives up never cancels the check for the others.
type Checks struct{ pending map[string]*Check }

// Check is one in-flight verification; err is set before done closes.
type Check struct {
	done chan struct{}
	err  error
}

// Pending returns the in-flight check for key, if any.
func (c *Checks) Pending(key string) *Check { return c.pending[key] }

// Start registers a new check for key, or returns nil if one is in flight.
func (c *Checks) Start(key string) *Check {
	if c.pending[key] != nil {
		return nil
	}
	if c.pending == nil {
		c.pending = map[string]*Check{}
	}
	check := &Check{done: make(chan struct{})}
	c.pending[key] = check
	return check
}

// Finish removes the check for key and releases its waiters with err.
func (c *Checks) Finish(key string, check *Check, err error) {
	if c.pending[key] == check {
		delete(c.pending, key)
	}
	check.err = err
	close(check.done)
}

// Wait blocks until the check finishes and returns its error, or ctx's error.
func (c *Check) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.err
	}
}
