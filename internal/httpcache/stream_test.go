package httpcache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/distributor"
)

// reopen replaces the fixture's service with one built with options, as a
// restart of the same data directory would.
func (f *fixture) reopen(t *testing.T, options ...Option) {
	t.Helper()
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	options = append([]Option{WithClock(func() time.Time { return time.Unix(f.clock.Load(), 0).UTC() }), WithTransferPolicy(distributor.DefaultIdleTimeout, fastRetry)}, options...)
	s, err := New(f.dir, f.db, f.budget, options...)
	if err != nil {
		t.Fatal(err)
	}
	f.s = s
}

// streamWriter is a concurrency-safe response writer that reports when the
// first body bytes arrive and whether the handler aborted the response.
type streamWriter struct {
	mu      sync.Mutex
	header  http.Header
	status  int
	body    bytes.Buffer
	first   chan struct{}
	once    sync.Once
	aborted bool
}

func newStreamWriter() *streamWriter {
	return &streamWriter{header: http.Header{}, first: make(chan struct{})}
}
func (w *streamWriter) Header() http.Header { return w.header }
func (w *streamWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = status
	}
}
func (w *streamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.body.Write(p)
	w.mu.Unlock()
	if n > 0 {
		w.once.Do(func() { close(w.first) })
	}
	return n, err
}
func (w *streamWriter) result() (int, string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status, w.body.String(), w.aborted
}

// serveInto serves one request into w and reports the handler's error,
// recording an aborted response instead of propagating the abort panic.
func (f *fixture) serveInto(w *streamWriter, headers http.Header) (err error) {
	defer func() {
		if caught := recover(); caught != nil {
			if caught != http.ErrAbortHandler {
				panic(caught)
			}
			w.mu.Lock()
			w.aborted = true
			w.mu.Unlock()
		}
	}()
	r := httptest.NewRequest(http.MethodGet, "http://redapp.example/vendor/app/file", nil)
	r.Header = headers
	return f.s.Serve(w, r, f.entry, "file")
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (f *fixture) noPartialFiles(t *testing.T) {
	t.Helper()
	files, err := os.ReadDir(f.s.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".body") {
			t.Fatal("partial cache file left behind", file.Name())
		}
	}
}

func TestConcurrentColdReadersStreamBeforeTheUpstreamCompletes(t *testing.T) {
	data := []byte(strings.Repeat("first half|", 50) + strings.Repeat("second half|", 50))
	half := 550
	release := make(chan struct{})
	var calls atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Write(data[:half])
		w.(http.Flusher).Flush()
		select {
		case <-release:
			w.Write(data[half:])
		case <-r.Context().Done():
		}
	}), 300)
	f.budget.limit = 1 << 20
	const readers = 3
	writers := make([]*streamWriter, readers)
	done := make(chan error, readers)
	for i := range writers {
		writers[i] = newStreamWriter()
		go func(w *streamWriter) { done <- f.serveInto(w, http.Header{}) }(writers[i])
	}
	// Every reader receives bytes while the single upstream response is
	// still incomplete.
	for _, w := range writers {
		<-w.first
	}
	if calls.Load() != 1 || len(f.rows(t)) != 0 {
		t.Fatal("readers did not share one incomplete upstream response", calls.Load())
	}
	close(release)
	for i := 0; i < readers; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range writers {
		if status, body, aborted := w.result(); status != http.StatusOK || body != string(data) || aborted || w.header.Get("ETag") != `"v1"` {
			t.Fatal("streamed response", status, len(body), aborted, w.header)
		}
	}
	rows := f.rows(t)
	if len(rows) != 1 || rows[0].SHA256 != digestOf(data) || rows[0].SizeBytes != int64(len(data)) || rows[0].LastAccessAt == nil {
		t.Fatal("complete stream was not published intact", rows)
	}
	if w, err := f.serve(t, "GET", http.Header{}); err != nil || w.Body.String() != string(data) || calls.Load() != 1 {
		t.Fatal("published stream not served from the cache", err, calls.Load())
	}
	if f.budget.readers.Load() != 0 || f.budget.writers.Load() != 0 {
		t.Fatal("streaming leaked capacity")
	}
}

// A slow upstream is bounded only by its idle gaps: the transfer lasts many
// idle timeouts, the equivalent of the former total limit at this scale.
func TestSlowUpstreamHasNoTotalTimeLimit(t *testing.T) {
	const idle = 150 * time.Millisecond
	const chunks = 30
	data := bytes.Repeat([]byte("0123456789abcdef"), 512)
	var calls atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		size := len(data) / chunks
		for offset := 0; offset < len(data); offset += size {
			w.Write(data[offset:min(offset+size, len(data))])
			w.(http.Flusher).Flush()
			time.Sleep(idle / 4) // upstream pacing, not test synchronization
		}
	}), 300)
	f.budget.limit = 1 << 20
	f.reopen(t, WithTransferPolicy(idle, fastRetry))
	start := time.Now()
	w, err := f.serve(t, "GET", http.Header{})
	if err != nil || !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatal("slow transfer failed", err, w.Body.Len())
	}
	if elapsed := time.Since(start); elapsed < 4*idle || calls.Load() != 1 {
		t.Fatal("transfer did not outlast several idle timeouts in one request", elapsed, calls.Load())
	}
	if rows := f.rows(t); len(rows) != 1 || rows[0].SHA256 != digestOf(data) {
		t.Fatal(rows)
	}
}

// stallingSource serves half of data and then stalls the first response; with
// a validator it answers range requests from the requested offset.
type stallingSource struct {
	data      []byte
	validator string // ETag, or "" for none
	resumed   http.Header
	mu        sync.Mutex
	requests  []string
}

func (u *stallingSource) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.requests = append(u.requests, r.Header.Get("Range")+" "+r.Header.Get("If-Range"))
	first := len(u.requests) == 1
	u.mu.Unlock()
	if u.validator != "" {
		w.Header().Set("ETag", u.validator)
		w.Header().Set("Accept-Ranges", "bytes")
	}
	if spec := r.Header.Get("Range"); spec != "" && !first {
		var start int
		if _, err := fmt.Sscanf(spec, "bytes=%d-", &start); err != nil || r.Header.Get("If-Range") != u.validator {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for key, values := range u.resumed {
			w.Header()[key] = values
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(u.data)-1, len(u.data)))
		w.Header().Set("Content-Length", fmt.Sprint(len(u.data)-start))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(u.data[start:])
		return
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(u.data)))
	w.Write(u.data[:len(u.data)/2])
	w.(http.Flusher).Flush()
	if first {
		<-r.Context().Done() // stall until the idle timeout gives up
		return
	}
	w.Write(u.data[len(u.data)/2:])
}

func (u *stallingSource) seen() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.requests...)
}

func TestStalledUpstreamResumesTheSameRepresentation(t *testing.T) {
	source := &stallingSource{data: bytes.Repeat([]byte("resumable "), 400), validator: `"v1"`}
	f := newFixture(t, source, 300)
	f.budget.limit = 1 << 20
	f.reopen(t, WithTransferPolicy(100*time.Millisecond, fastRetry))
	w := newStreamWriter()
	if err := f.serveInto(w, http.Header{}); err != nil {
		t.Fatal(err)
	}
	if status, body, aborted := w.result(); status != http.StatusOK || body != string(source.data) || aborted {
		t.Fatal("resumed stream", status, len(body), aborted)
	}
	half := len(source.data) / 2
	if seen := source.seen(); len(seen) != 2 || seen[1] != fmt.Sprintf(`bytes=%d- "v1"`, half) {
		t.Fatal("resume did not continue from the stalled offset with If-Range", seen)
	}
	if rows := f.rows(t); len(rows) != 1 || rows[0].SHA256 != digestOf(source.data) {
		t.Fatal("resumed body was not published intact", rows)
	}
	f.noPartialFiles(t)
}

func TestFailedStreamsLeaveNoEntry(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source *stallingSource
	}{
		// Without a validator the bytes already sent cannot be continued.
		{"no validator", &stallingSource{data: bytes.Repeat([]byte("plain "), 400)}},
		// The continuation turned uncacheable: it must not complete the entry.
		{"uncacheable resume", &stallingSource{data: bytes.Repeat([]byte("cookie "), 400), validator: `"v1"`, resumed: http.Header{"Set-Cookie": {"session=1"}}}},
		// A different representation cannot continue the bytes already sent.
		{"changed resume", &stallingSource{data: bytes.Repeat([]byte("changed "), 400), validator: `"v1"`, resumed: http.Header{"Etag": {`"v2"`}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.source, 300)
			f.budget.limit = 1 << 20
			f.reopen(t, WithTransferPolicy(100*time.Millisecond, fastRetry))
			w := newStreamWriter()
			err := f.serveInto(w, http.Header{})
			if _, body, aborted := w.result(); err == nil && !aborted || body == string(tc.source.data) {
				t.Fatal("failed stream completed", err, aborted)
			}
			if len(f.rows(t)) != 0 {
				t.Fatal("failed stream left an entry")
			}
			f.noPartialFiles(t)
			if f.budget.readers.Load() != 0 || f.budget.writers.Load() != 0 {
				t.Fatal("failed stream leaked capacity")
			}
		})
	}
}

func TestBodyExceedingTheLimitMidStreamLeavesNoEntry(t *testing.T) {
	release := make(chan struct{})
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		io.WriteString(w, "12345") // no declared length
		w.(http.Flusher).Flush()
		<-release
		io.WriteString(w, "6789X")
	}), 300)
	f.budget.limit = 8
	w := newStreamWriter()
	done := make(chan error, 1)
	go func() { done <- f.serveInto(w, http.Header{}) }()
	<-w.first
	close(release)
	err := <-done
	if _, body, aborted := w.result(); !aborted || body != "12345" || err != nil {
		t.Fatal("oversized stream was not aborted after its accepted bytes", body, aborted, err)
	}
	if len(f.rows(t)) != 0 {
		t.Fatal("oversized stream left an entry")
	}
	f.noPartialFiles(t)
}

func TestColdAndWarmRangeRequests(t *testing.T) {
	data := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	release := make(chan struct{})
	var calls atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Write(data[:10])
		w.(http.Flusher).Flush()
		select {
		case <-release:
			w.Write(data[10:])
		case <-r.Context().Done():
		}
	}), 300)
	full := newStreamWriter()
	fullDone := make(chan error, 1)
	go func() { fullDone <- f.serveInto(full, http.Header{}) }()
	<-full.first
	// A range already written is answered while the file is still arriving.
	head := newStreamWriter()
	if err := f.serveInto(head, http.Header{"Range": {"bytes=2-4"}}); err != nil {
		t.Fatal(err)
	}
	if status, body, _ := head.result(); status != http.StatusPartialContent || body != "234" || head.header.Get("Content-Range") != fmt.Sprintf("bytes 2-4/%d", len(data)) {
		t.Fatal("cold range", status, body, head.header)
	}
	// A range beyond the written bytes waits for them.
	tail := newStreamWriter()
	tailDone := make(chan error, 1)
	go func() { tailDone <- f.serveInto(tail, http.Header{"Range": {"bytes=-6"}}) }()
	close(release)
	if err := <-tailDone; err != nil {
		t.Fatal(err)
	}
	if status, body, _ := tail.result(); status != http.StatusPartialContent || body != "uvwxyz" {
		t.Fatal("cold tail range", status, body)
	}
	if err := <-fullDone; err != nil {
		t.Fatal(err)
	}
	// The complete file is cached and answers ranges without the upstream.
	warm, err := f.serve(t, "GET", http.Header{"Range": {"bytes=10-12"}})
	if err != nil || warm.Code != http.StatusPartialContent || warm.Body.String() != "abc" || calls.Load() != 1 {
		t.Fatal("warm range", warm.Code, warm.Body.String(), err, calls.Load())
	}
}

func TestStreamWithoutDeclaredLengthIgnoresRanges(t *testing.T) {
	release := make(chan struct{})
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "unknown ") // chunked: the length is unknown until the end
		w.(http.Flusher).Flush()
		select {
		case <-release:
			io.WriteString(w, "length body")
		case <-r.Context().Done():
		}
	}), 300)
	w := newStreamWriter()
	done := make(chan error, 1)
	go func() { done <- f.serveInto(w, http.Header{"Range": {"bytes=0-2"}}) }()
	<-w.first
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if status, body, _ := w.result(); status != http.StatusOK || body != "unknown length body" || w.header.Get("Content-Length") != "" {
		t.Fatal(status, body, w.header)
	}
	if rows := f.rows(t); len(rows) != 1 || rows[0].SizeBytes != int64(len("unknown length body")) {
		t.Fatal(rows)
	}
	// Once cached, the same range is answered from the complete file.
	if cached, err := f.serve(t, "GET", http.Header{"Range": {"bytes=0-2"}}); err != nil || cached.Code != http.StatusPartialContent || cached.Body.String() != "unk" {
		t.Fatal(cached.Code, cached.Body.String(), err)
	}
}

func TestRestartDiscardsInterruptedFills(t *testing.T) {
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "fresh") }), 300)
	part := f.s.partPath(testID(t))
	if err := os.WriteFile(part, []byte("half of a previous fill"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal("interrupted fill survived a restart", err)
	}
	if w, err := f.serve(t, "GET", http.Header{}); err != nil || w.Body.String() != "fresh" {
		t.Fatal(w.Body.String(), err)
	}
}

// A reader that stops before the body is complete releases the transfer only
// when it was the last one; nothing is published for an abandoned fill.
func TestLastReaderLeavingStopsTheFill(t *testing.T) {
	stopped := make(chan struct{})
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		io.WriteString(w, "partial")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(stopped)
	}), 300)
	ctx, cancel := context.WithCancel(context.Background())
	w := newStreamWriter()
	go func() {
		defer func() { recover() }() // the cancelled response is aborted
		r := httptest.NewRequest(http.MethodGet, "http://redapp.example/vendor/app/file", nil).WithContext(ctx)
		f.s.Serve(w, r, f.entry, "file")
	}()
	<-w.first
	cancel()
	<-stopped
	waitFor(t, "abandoned fill kept capacity", func() bool { return f.budget.writers.Load() == 0 && f.budget.readers.Load() == 0 })
	waitFor(t, "abandoned fill left its part file", func() bool {
		files, _ := os.ReadDir(f.s.dir)
		return len(files) == 0
	})
	if len(f.rows(t)) != 0 {
		t.Fatal("abandoned fill was published")
	}
}

// A request arriving while a fill stops because its only reader left must not
// inherit that stop: it fetches the file again instead of failing.
func TestRequestAfterLastReaderLeftFetchesAgain(t *testing.T) {
	var calls atomic.Int32
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Length", "5")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		io.WriteString(w, "fresh")
	}), 300)
	// The stopped fill pauses before it records its end, until the second
	// request has either joined it or started its own fetch.
	stopped, release := make(chan *stream, 1), make(chan struct{})
	var once, releaseOnce sync.Once
	streamFillFailed = func(st *stream) {
		once.Do(func() {
			stopped <- st
			<-release
		})
	}
	t.Cleanup(func() { streamFillFailed = nil })
	resume := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(resume) // runs before the fixture closes, so a failure cannot block Close
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		r := httptest.NewRequest(http.MethodGet, "http://redapp.example/vendor/app/file", nil).WithContext(ctx)
		first <- f.s.Serve(httptest.NewRecorder(), r, f.entry, "file")
	}()
	waitFor(t, "first request never reached the stream", func() bool {
		f.s.mu.Lock()
		defer f.s.mu.Unlock()
		return len(f.s.streams) == 1
	})
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal("first request", err)
	}
	old := <-stopped
	second := make(chan error, 1)
	w := httptest.NewRecorder()
	go func() {
		r := httptest.NewRequest(http.MethodGet, "http://redapp.example/vendor/app/file", nil)
		second <- f.s.Serve(w, r, f.entry, "file")
	}()
	waitFor(t, "second request neither joined nor fetched", func() bool {
		f.s.mu.Lock()
		defer f.s.mu.Unlock()
		return old.readers > 0 || calls.Load() == 2
	})
	resume()
	if err := <-second; err != nil || w.Code != http.StatusOK || w.Body.String() != "fresh" {
		t.Fatal(w.Code, w.Body.String(), err)
	}
}
