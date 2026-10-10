// Package spool is the streaming file core shared by the download engine and
// the HTTP cache. One writer fills a local file from upstream responses, with
// bounded retries and byte-range resumption, while any number of readers
// follow the bytes already written. Owners decide file naming, verification,
// publication and persistence; this package never touches the database.
package spool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// ErrIncomplete reports a body whose writer failed. Readers never observe a
// failed body as a successful EOF, even after every written byte was read.
var ErrIncomplete = errors.New("file did not complete")

// Body is the shared progress of one file: the readable prefix written so far,
// the declared total length and the final outcome. Its own lock guards every
// field and is never held during file I/O, so readers do not contend with the
// owner's bookkeeping.
type Body struct {
	mu      sync.Mutex
	file    *os.File
	size    int64
	total   int64
	done    bool
	err     error
	changed chan struct{}
}

// NewBody starts an unfinished body whose first size bytes are already in file.
func NewBody(file *os.File, size int64) *Body {
	return &Body{file: file, size: size, total: -1, changed: make(chan struct{})}
}

// NewComplete returns a finished body of size bytes.
func NewComplete(file *os.File, size int64) *Body {
	b := NewBody(file, size)
	b.total, b.done = size, true
	return b
}

func (b *Body) signalLocked() { close(b.changed); b.changed = make(chan struct{}) }

// Size is the readable prefix length.
func (b *Body) Size() int64 { b.mu.Lock(); defer b.mu.Unlock(); return b.size }

// Total is the declared length of the whole file, or -1 while unknown.
func (b *Body) Total() int64 { b.mu.Lock(); defer b.mu.Unlock(); return b.total }

// File is the descriptor shared by the writer and readers, or nil once closed.
func (b *Body) File() *os.File { b.mu.Lock(); defer b.mu.Unlock(); return b.file }

// SetFile attaches a descriptor, for example when a finished file is opened
// again for new readers.
func (b *Body) SetFile(f *os.File) { b.mu.Lock(); b.file = f; b.mu.Unlock() }

// CloseFile closes and detaches the descriptor. The owner calls it only when
// neither the writer nor any reader still uses the body.
func (b *Body) CloseFile() error {
	b.mu.Lock()
	f := b.file
	b.file = nil
	b.mu.Unlock()
	if f == nil {
		return nil
	}
	return f.Close()
}

// SetTotal records the declared length of the whole file, or -1 while unknown.
func (b *Body) SetTotal(n int64) { b.mu.Lock(); b.total = n; b.mu.Unlock() }

// append writes p after the readable prefix and publishes it to readers. Only
// the single writer appends, so the offset cannot move during the write.
func (b *Body) append(p []byte) (int, error) {
	b.mu.Lock()
	f, offset := b.file, b.size
	b.mu.Unlock()
	if f == nil {
		return 0, os.ErrClosed
	}
	n, err := f.WriteAt(p, offset)
	if n > 0 {
		b.mu.Lock()
		b.size = offset + int64(n)
		b.signalLocked()
		b.mu.Unlock()
	}
	return n, err
}

// Finish records the final outcome and wakes every reader: nil completes the
// body, anything else fails it. A finished body may be failed later, for
// example when its published content proves invalid.
func (b *Body) Finish(err error) {
	b.mu.Lock()
	b.done, b.err = true, err
	b.signalLocked()
	b.mu.Unlock()
}

// Reopen makes a failed body writable again so that a new writer resumes it.
func (b *Body) Reopen() {
	b.mu.Lock()
	b.done, b.err = false, nil
	b.signalLocked()
	b.mu.Unlock()
}

// Wait blocks until the body finishes and returns its failure, or ctx's error.
func (b *Body) Wait(ctx context.Context) error {
	for {
		b.mu.Lock()
		done, err, changed := b.done, b.err, b.changed
		b.mu.Unlock()
		if done {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// Await blocks until at least n bytes are readable or the body finished. It
// returns the failure of a finished body even when n bytes exist, so a caller
// can still report it before sending anything.
func (b *Body) Await(ctx context.Context, n int64) error {
	for {
		b.mu.Lock()
		size, done, err, changed := b.size, b.done, b.err, b.changed
		b.mu.Unlock()
		if done {
			return err
		}
		if size >= n {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// NewReader follows the body from its start. ctx bounds every wait.
func (b *Body) NewReader(ctx context.Context) *Reader { return &Reader{body: b, ctx: ctx} }

// Reader reads a body while it is being written: it waits for bytes that do
// not exist yet and returns EOF only after the body completed successfully.
type Reader struct {
	body   *Body
	ctx    context.Context
	offset int64
}

func (r *Reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		b := r.body
		b.mu.Lock()
		available := b.size - r.offset
		done, failure, changed, f := b.done, b.err, b.changed, b.file
		b.mu.Unlock()
		if done && failure != nil {
			return 0, fmt.Errorf("%w: %w", ErrIncomplete, failure)
		}
		if available > 0 {
			if f == nil {
				return 0, os.ErrClosed
			}
			if int64(len(p)) > available {
				p = p[:available]
			}
			n, err := f.ReadAt(p, r.offset)
			r.offset += int64(n)
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return n, err
		}
		if done {
			return 0, io.EOF
		}
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		case <-changed:
		}
	}
}

// Offset is the position of the next Read.
func (r *Reader) Offset() int64 { return r.offset }

// Seek positions the next Read. Seeking relative to the end requires a known
// total length; the bytes need not have been written yet.
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.offset
	case io.SeekEnd:
		total := r.body.Total()
		if total < 0 {
			return 0, errors.New("seek relative to an unknown length")
		}
		offset += total
	default:
		return 0, errors.New("invalid seek whence")
	}
	if offset < 0 {
		return 0, errors.New("negative seek position")
	}
	r.offset = offset
	return offset, nil
}
