package spool

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tempBody(t *testing.T) *Body {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(t.TempDir(), "body.part"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBody(f, 0)
	t.Cleanup(func() { b.CloseFile() })
	return b
}

// chunks is an upstream body that hands out one chunk per receive, so the
// test decides exactly how far the writer has progressed.
type chunks struct {
	next chan []byte
	err  error
}

func (c *chunks) Read(p []byte) (int, error) {
	chunk, ok := <-c.next
	if !ok {
		if c.err != nil {
			return 0, c.err
		}
		return 0, io.EOF
	}
	return copy(p, chunk), nil
}
func (c *chunks) Close() error { return nil }

func TestReaderFollowsWriterAndSeesOnlyFinalOutcome(t *testing.T) {
	b := tempBody(t)
	upstream := &chunks{next: make(chan []byte)}
	fill := Fill{Body: b, Limit: 100, Retry: Retry{Attempts: 1}}
	done := make(chan error, 1)
	go func() { done <- fill.Run(context.Background(), &Segment{Body: upstream, Total: 10}) }()
	r := b.NewReader(context.Background())
	upstream.next <- []byte("hello")
	buf := make([]byte, 10)
	n, err := r.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatal("reader did not stream the written prefix", n, err)
	}
	// Seeking from the end works with a declared length before the bytes exist.
	if pos, err := r.Seek(-2, io.SeekEnd); err != nil || pos != 8 {
		t.Fatal(pos, err)
	}
	upstream.next <- []byte("world")
	close(upstream.next)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	tail := make([]byte, 2)
	if _, err := io.ReadFull(r, tail); err != nil || string(tail) != "ld" {
		t.Fatal("tail after seek", string(tail), err)
	}
	// Every byte is written, but the owner has not finished the body yet.
	waiting, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	pending := b.NewReader(waiting)
	pending.Seek(10, io.SeekStart)
	if _, err := pending.Read(tail); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("reader reached EOF before the owner finished the body", err)
	}
	b.Finish(nil)
	if _, err := r.Read(tail); err != io.EOF {
		t.Fatal("finished body", err)
	}
	if b.Total() != 10 {
		t.Fatal("total", b.Total())
	}
}

func TestFailedBodyIsNeverASuccessfulEOF(t *testing.T) {
	b := tempBody(t)
	fill := Fill{Body: b, Limit: 100, Retry: Retry{Attempts: 1}}
	if err := fill.Run(context.Background(), &Segment{Body: io.NopCloser(strings.NewReader("complete")), Total: -1}); err != nil {
		t.Fatal(err)
	}
	b.Finish(errors.New("verification failed"))
	if _, err := io.ReadAll(b.NewReader(context.Background())); !errors.Is(err, ErrIncomplete) {
		t.Fatal("failed body ended as EOF", err)
	}
	if err := b.Wait(context.Background()); err == nil {
		t.Fatal("wait hid the failure")
	}
}

func TestFillRejectsOversizedAndTruncatedBodies(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		total int64
		want  error
	}{
		{"limit", "0123456789x", -1, ErrLength},
		{"declared limit", "", 11, ErrLength},
		{"beyond declared", "0123", 3, ErrLength},
		{"truncated", "012", 5, ErrTruncated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fill := Fill{Body: tempBody(t), Limit: 10, Retry: Retry{Attempts: 1}}
			err := fill.Run(context.Background(), &Segment{Body: io.NopCloser(strings.NewReader(tc.body)), Total: tc.total})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestFillResumesAtTheWrittenOffset(t *testing.T) {
	b := tempBody(t)
	data := []byte("0123456789")
	var offsets []int64
	var retried []error
	fill := Fill{Body: b, Limit: 100, Retry: Retry{Attempts: 3, Base: time.Millisecond, Max: time.Millisecond},
		Open: func(_ context.Context, offset int64) (Segment, error) {
			offsets = append(offsets, offset)
			if len(offsets) == 1 {
				return Segment{}, StatusError(http.StatusServiceUnavailable)
			}
			return Segment{Body: io.NopCloser(bytes.NewReader(data[offset:])), Total: int64(len(data))}, nil
		},
		Retrying: func(err error) { retried = append(retried, err) },
	}
	interrupted := &chunks{next: make(chan []byte, 1), err: io.ErrUnexpectedEOF}
	interrupted.next <- data[:4]
	close(interrupted.next)
	if err := fill.Run(context.Background(), &Segment{Body: interrupted, Total: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	b.Finish(nil)
	got, err := io.ReadAll(b.NewReader(context.Background()))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal(string(got), err)
	}
	if len(offsets) != 2 || offsets[0] != 4 || offsets[1] != 4 || len(retried) != 2 {
		t.Fatal("resume offsets or retries", offsets, retried)
	}
	var read *ReadError
	if !errors.As(retried[0], &read) || !Retryable(retried[1]) {
		t.Fatal("retry causes", retried)
	}
}

func TestFillStopsOnFinalErrorsAndExhaustion(t *testing.T) {
	calls := 0
	fill := Fill{Body: tempBody(t), Limit: 100, Retry: Retry{Attempts: 2, Base: time.Millisecond, Max: time.Millisecond},
		Open: func(context.Context, int64) (Segment, error) {
			calls++
			return Segment{}, StatusError(http.StatusForbidden)
		}}
	if err := fill.Run(context.Background(), nil); !errors.Is(err, StatusError(http.StatusForbidden)) || calls != 1 {
		t.Fatal("client error retried", err, calls)
	}
	calls = 0
	fill.Open = func(context.Context, int64) (Segment, error) {
		calls++
		return Segment{}, StatusError(http.StatusBadGateway)
	}
	if err := fill.Run(context.Background(), nil); err == nil || calls != 2 {
		t.Fatal("retries not bounded", err, calls)
	}
}

func TestCheckResumeAcceptsOnlyTheExactContinuation(t *testing.T) {
	response := func(status int, contentRange string, length int64) *http.Response {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Range": {contentRange}}, ContentLength: length}
	}
	if n, err := CheckResume(response(206, "bytes 4-9/10", 6), 4, 10); err != nil || n != 10 {
		t.Fatal(n, err)
	}
	for _, resp := range []*http.Response{
		response(200, "", 10),
		response(206, "bytes 3-9/10", 7),
		response(206, "bytes 4-8/10", 5),
		response(206, "bytes 4-9/11", 6),
		response(206, "bytes 4-9/10", 5),
		response(206, "bytes */10", -1),
	} {
		if _, err := CheckResume(resp, 4, 10); !errors.Is(err, ErrUnsafeResume) {
			t.Fatal("accepted unsafe resume", resp.StatusCode, resp.Header)
		}
	}
}

func TestChecksCoalesceAndReleaseWaiters(t *testing.T) {
	var checks Checks
	first := checks.Start("key")
	if first == nil || checks.Start("key") != nil || checks.Pending("key") != first {
		t.Fatal("check was not coalesced")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := first.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("abandoned wait", err)
	}
	failure := errors.New("invalid")
	checks.Finish("key", first, failure)
	if err := first.Wait(context.Background()); err != failure || checks.Pending("key") != nil {
		t.Fatal("finished check", err)
	}
}

func TestCheckFileBindsResultToInode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blob")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	const digest = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	check, err := CheckFile(context.Background(), path, 3, digest)
	if err != nil || !check.Valid {
		t.Fatal(check, err)
	}
	st, _ := os.Lstat(path)
	if !check.Matches(st) {
		t.Fatal("same inode did not match")
	}
	os.Remove(path)
	os.WriteFile(path, []byte("abc"), 0o600)
	st, _ = os.Lstat(path)
	if check.Matches(st) {
		t.Fatal("replaced file matched an old check")
	}
	if missing, err := CheckFile(context.Background(), path+".missing", 3, digest); missing != nil || err != nil {
		t.Fatal(missing, err)
	}
}
