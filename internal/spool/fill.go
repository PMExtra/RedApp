package spool

import (
	"context"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrLength reports a body longer than the limit or than its declared length.
	ErrLength = errors.New("upstream length exceeds the limit or the declared length")
	// ErrTruncated reports a body that ended before its declared length.
	ErrTruncated error = transientError("upstream body ended before its declared length")
	// ErrUnsafeResume reports a resumption that could not prove it continues
	// the same representation at the same offset.
	ErrUnsafeResume = errors.New("upstream cannot safely resume the representation")
)

type transientError string

func (e transientError) Error() string   { return string(e) }
func (e transientError) Transient() bool { return true }

// StatusError is an upstream HTTP status where a body was expected.
type StatusError int

func (e StatusError) Error() string { return fmt.Sprintf("upstream HTTP %d", int(e)) }

// Transient reports statuses a later attempt may resolve.
func (e StatusError) Transient() bool {
	return e >= 500 || e == http.StatusRequestTimeout || e == http.StatusTooManyRequests
}

// ReadError is a failed read of an upstream body, for example an idle timeout.
type ReadError struct{ Err error }

func (e *ReadError) Error() string   { return "upstream body read failed" }
func (e *ReadError) Unwrap() error   { return e.Err }
func (e *ReadError) Transient() bool { return true }

// WriteError is a failed local write; it is never retried.
type WriteError struct{ Err error }

func (e *WriteError) Error() string { return "local file write failed" }
func (e *WriteError) Unwrap() error { return e.Err }

// Retryable reports failures a later attempt may resolve: errors in the chain
// that declare themselves transient, such as connection failures, interrupted
// reads, truncation and 5xx, 408 or 429 statuses. Integrity, length, encoding,
// disk and other client errors are final.
func Retryable(err error) bool {
	var t interface{ Transient() bool }
	return errors.As(err, &t) && t.Transient()
}

// Retry bounds the attempts of one fill. Delays grow exponentially from Base
// to at most Max, with jitter.
type Retry struct {
	Attempts  int
	Base, Max time.Duration
}

// DefaultRetry allows six attempts with backoff from 1 to 30 seconds.
func DefaultRetry() Retry { return Retry{Attempts: 6, Base: time.Second, Max: 30 * time.Second} }

// Delay is the wait before attempt+1, uniformly jittered in [d/2, d].
func (r Retry) Delay(attempt int) time.Duration {
	d := r.Max
	if attempt < 30 && r.Base<<attempt < r.Max {
		d = r.Base << attempt
	}
	if d <= 1 {
		return d
	}
	return d/2 + mathrand.N(d/2+1)
}

// Segment is one upstream response body that continues the file at the
// offset it was opened for.
type Segment struct {
	// Body is nil when the upstream proved that the file already holds the
	// whole representation.
	Body io.ReadCloser
	// Total is the length of the whole representation, or -1 when unknown.
	Total int64
}

// Fill copies upstream segments into a Body until it holds the whole
// representation. There is no overall deadline: an attempt fails only when the
// upstream fails, stops sending bytes for the client's idle timeout, or sends
// bytes that cannot be accepted.
type Fill struct {
	Body  *Body
	Limit int64
	Retry Retry
	// Open starts an attempt at offset, which is the current body size. A
	// non-zero offset must resume the same representation; Open validates that
	// with CheckResume or equivalent owner rules.
	Open func(ctx context.Context, offset int64) (Segment, error)
	// Observe sees every chunk read from upstream before it is validated or
	// written, so that accounting includes rejected bytes. An error ends the fill.
	Observe func(p []byte) error
	// Progress runs after each chunk is written with the new body size.
	Progress func(size int64) error
	// Retrying runs before the wait that precedes another attempt.
	Retrying func(err error)
	// Resumable, when set, reports whether the bytes already written can be
	// continued; a failed attempt is not retried when it reports false.
	Resumable func() bool
}

// Run fills the body. first, when set, is the response of the first attempt,
// already opened by the owner at offset zero. Run does not finish the body:
// the owner verifies and publishes the file before choosing the outcome.
func (f *Fill) Run(ctx context.Context, first *Segment) error {
	for attempt := 0; ; attempt++ {
		err := f.attempt(ctx, first)
		first = nil
		if err == nil {
			return nil
		}
		if !Retryable(err) || ctx.Err() != nil || attempt+1 >= f.Retry.Attempts || f.Resumable != nil && !f.Resumable() {
			return err
		}
		if f.Retrying != nil {
			f.Retrying(err)
		}
		wait := time.NewTimer(f.Retry.Delay(attempt))
		select {
		case <-ctx.Done():
		case <-wait.C:
		}
		wait.Stop()
	}
}

func (f *Fill) attempt(ctx context.Context, segment *Segment) error {
	offset := f.Body.Size()
	if segment == nil {
		opened, err := f.Open(ctx, offset)
		if err != nil {
			return err
		}
		segment = &opened
	}
	if segment.Body == nil {
		if segment.Total != offset {
			return ErrUnsafeResume
		}
		f.Body.SetTotal(offset)
		return nil
	}
	defer segment.Body.Close()
	total := segment.Total
	if total > f.Limit {
		return ErrLength
	}
	if total >= 0 {
		f.Body.SetTotal(total)
	}
	buf := make([]byte, 64<<10)
	for {
		n, readErr := segment.Body.Read(buf)
		if n > 0 {
			if f.Observe != nil {
				if err := f.Observe(buf[:n]); err != nil {
					return err
				}
			}
			if offset+int64(n) > f.Limit || total >= 0 && offset+int64(n) > total {
				return ErrLength
			}
			written, err := f.Body.append(buf[:n])
			if err != nil {
				return &WriteError{err}
			}
			if written != n {
				return &WriteError{io.ErrShortWrite}
			}
			offset += int64(n)
			if f.Progress != nil {
				if err := f.Progress(offset); err != nil {
					return err
				}
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return &ReadError{readErr}
			}
			break
		}
	}
	if total >= 0 && offset != total {
		return ErrTruncated
	}
	f.Body.SetTotal(offset)
	return nil
}

// RangeHeader asks for the representation from offset on. ifRange is the
// validator the response must still match; an empty value sends none.
func RangeHeader(offset int64, ifRange string) http.Header {
	h := http.Header{}
	h.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	if ifRange != "" {
		h.Set("If-Range", ifRange)
	}
	return h
}

// CheckResume accepts only an exact 206 response continuing at offset: a
// single Content-Range from offset to the end, a matching Content-Length and
// a total equal to total when that was known. It returns the whole length.
func CheckResume(resp *http.Response, offset, total int64) (int64, error) {
	if resp.StatusCode != http.StatusPartialContent {
		return 0, ErrUnsafeResume
	}
	start, end, n, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil || start != offset || end != n-1 || total >= 0 && total != n || resp.ContentLength >= 0 && resp.ContentLength != end-start+1 {
		return 0, ErrUnsafeResume
	}
	return n, nil
}

// parseContentRange parses "bytes first-last/complete".
func parseContentRange(s string) (first, last, complete int64, err error) {
	spec, ok := strings.CutPrefix(s, "bytes ")
	if !ok {
		return 0, 0, 0, ErrUnsafeResume
	}
	bounds, length, ok := strings.Cut(spec, "/")
	if !ok {
		return 0, 0, 0, ErrUnsafeResume
	}
	a, b, ok := strings.Cut(bounds, "-")
	if !ok {
		return 0, 0, 0, ErrUnsafeResume
	}
	first, e1 := strconv.ParseInt(a, 10, 64)
	last, e2 := strconv.ParseInt(b, 10, 64)
	complete, e3 := strconv.ParseInt(length, 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || first < 0 || last < first || complete <= last {
		return 0, 0, 0, ErrUnsafeResume
	}
	return first, last, complete, nil
}
