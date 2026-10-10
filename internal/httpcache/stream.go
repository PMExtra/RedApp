package httpcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/spool"
	"github.com/PMExtra/RedApp/internal/store"
)

// stream is one cacheable upstream response being written to a part file
// while readers follow it. It always comes from a single source: an attempt
// that fails is resumed from the same source with a byte range validated
// against the original response, never continued from another source. Only
// a complete body is published, as the entry with the same ID.
type stream struct {
	id     string
	fill   fill
	key    string // flight key; the flight stays joinable while the fill runs
	body   *spool.Body
	header http.Header // representation headers of the upstream response
	source string      // URL that produced the response
	// validator is the strong validator that lets a range request continue
	// exactly this representation, or empty when it cannot be resumed.
	validator string
	cancel    context.CancelFunc
	done      chan struct{} // closed once the fill ended and publication was attempted
	// Guarded by Service.mu. The flight holds one reader slot until it
	// resolves and hands one to each waiter.
	readers   int
	public    bool   // a public client read it; publication records the access
	finished  bool   // the fill ended
	published string // entry ID, once published
}

// streamError maps the failure of a body that has not been sent yet.
func streamError(err error) error {
	switch {
	case errors.Is(err, spool.ErrLength):
		return download.ErrArtifactLimit
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, store.ErrSourceInactive):
		return err
	}
	return fmt.Errorf("%w: %w", ErrUpstream, err)
}

// resumeValidator returns the strong validator of a representation: a strong
// ETag, or a Last-Modified at least one second older than Date (RFC 9110
// 13.1.5 and 8.8.2.2). Without one, a failed transfer cannot be resumed.
func resumeValidator(h http.Header) string {
	if tag := h.Get("ETag"); tag != "" && !strings.HasPrefix(tag, "W/") {
		return tag
	}
	modified, err := http.ParseTime(h.Get("Last-Modified"))
	date, dateErr := http.ParseTime(h.Get("Date"))
	if err == nil && dateErr == nil && date.Sub(modified) >= time.Second {
		return h.Get("Last-Modified")
	}
	return ""
}

// sameRepresentation reports whether a resumed response carries the same
// validators as the response it continues.
func sameRepresentation(original, resumed http.Header) bool {
	for _, key := range []string{"ETag", "Last-Modified"} {
		if value := original.Get(key); value != "" && resumed.Get(key) != value {
			return false
		}
	}
	return true
}

// startStream turns a cacheable 200 response into a stream. It takes over the
// run's writer lease and flight work; the caller receives the flight's hold.
func (s *Service) startStream(ctx context.Context, run *fetchRun, old *Row, resp *http.Response, source string, client *distributor.Client) (fetchResult, error) {
	if resp.ContentLength > s.budget.MaxArtifactBytes() {
		return fetchResult{}, download.ErrArtifactLimit
	}
	id, err := fsutil.RandomID()
	if err != nil {
		return fetchResult{}, err
	}
	file, err := os.OpenFile(s.partPath(id), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return fetchResult{}, err
	}
	st := &stream{id: id, fill: run.fill, key: run.key, body: spool.NewBody(file, 0), header: representationHeaders(resp.Header), source: responseSourceURL(resp, source),
		validator: resumeValidator(resp.Header), cancel: run.cancel, done: make(chan struct{}), readers: 1}
	// Readers see the declared length before the first byte, so a ranged
	// response can be framed while the body is still arriving.
	st.body.SetTotal(resp.ContentLength)
	replaces := ""
	if old != nil {
		replaces = old.GenerationID
	}
	release, finish := run.release, run.finish
	run.release, run.finish = nil, nil
	s.mu.Lock()
	s.streams[id] = st
	s.wg.Add(1)
	s.mu.Unlock()
	go s.runStream(ctx, st, resp, client, replaces, release, finish)
	return fetchResult{stream: st, status: http.StatusOK}, nil
}

// streamFillFailed is nil in production. Tests set it to run when a fill has
// failed or was stopped, before the stream records its end.
var streamFillFailed func(*stream)

func (s *Service) runStream(ctx context.Context, st *stream, resp *http.Response, client *distributor.Client, replaces string, release, finish func()) {
	defer s.wg.Done()
	defer finish()
	defer release()
	sum := sha256.New()
	fill := spool.Fill{
		Body:  st.body,
		Limit: s.budget.MaxArtifactBytes(),
		Retry: s.retry,
		Open: func(ctx context.Context, offset int64) (spool.Segment, error) {
			return s.resume(ctx, st, client, offset)
		},
		Observe: func(p []byte) error {
			sum.Write(p)
			// Buffered; accounting never fails a transfer.
			_ = s.db.AddFor(st.fill.entry.MetricsID(), "upstream_bytes", int64(len(p)))
			return nil
		},
		Resumable: func() bool { return st.validator != "" },
		Retrying: func(error) {
			_ = s.upstreamFailure(st.fill.entry, st.fill.path, 0)
		},
	}
	err := fill.Run(ctx, &spool.Segment{Body: resp.Body, Total: resp.ContentLength})
	if err != nil {
		if streamFillFailed != nil {
			streamFillFailed(st)
		}
		if ctx.Err() == nil && !errors.Is(err, spool.ErrLength) {
			var status spool.StatusError
			errors.As(err, &status)
			_ = s.upstreamFailure(st.fill.entry, st.fill.path, int(status))
		}
		s.endStream(st, err, "")
		return
	}
	// Readers may finish as soon as every byte is written; the entry is
	// published only after the body is durable and still admitted.
	st.body.Finish(nil)
	published, err := s.publishStream(st, hex.EncodeToString(sum.Sum(nil)), replaces)
	if err == nil {
		// Accounting events never fail a transfer that already completed.
		_ = s.overrideWarning(st.fill, st.header)
	}
	s.endStream(st, nil, published)
}

// resume continues a stream at offset from the source that produced it. The
// response must be the exact remaining range of the same representation and
// must still be cacheable; anything else ends the stream without an entry.
func (s *Service) resume(ctx context.Context, st *stream, client *distributor.Client, offset int64) (spool.Segment, error) {
	resp, err := client.Send(ctx, distributor.Request{Method: http.MethodGet, URL: st.source, Header: spool.RangeHeader(offset, st.validator), IdleTimeout: s.idleTimeout})
	if err != nil {
		return spool.Segment{}, err
	}
	if status := spool.StatusError(resp.StatusCode); status.Transient() {
		resp.Body.Close()
		return spool.Segment{}, status
	}
	total, err := spool.CheckResume(resp, offset, st.body.Total())
	if err == nil && (!sameRepresentation(st.header, resp.Header) || !st.fill.eligible(resp.Header)) {
		err = spool.ErrUnsafeResume
	}
	if err != nil {
		resp.Body.Close()
		return spool.Segment{}, err
	}
	return spool.Segment{Body: resp.Body, Total: total}, nil
}

// publishStream makes a complete stream the current entry of its path. With
// replaces set, that entry must still be current; the source fence must still
// admit the fill's application snapshot.
func (s *Service) publishStream(st *stream, digest, replaces string) (string, error) {
	part, body := s.partPath(st.id), s.bodyPath(st.id)
	if err := st.body.File().Sync(); err != nil {
		fsutil.Remove(part)
		return "", err
	}
	if err := fsutil.Rename(part, body); err != nil {
		fsutil.Remove(part)
		return "", err
	}
	headers, err := json.Marshal(st.header)
	if err != nil {
		fsutil.Remove(body)
		return "", err
	}
	now := s.now().UTC()
	entry := store.HTTPCacheEntry{ID: st.id, StorageID: st.fill.entry.StorageID(), Path: st.fill.path, SourceURL: st.source, SHA256: digest, SizeBytes: st.body.Size(), Headers: headers, FetchedAt: now, ValidatedAt: now, FreshUntil: st.fill.freshness(st.header, now)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.public {
		entry.AccessBucket = now.Unix() / 60 * 60
	}
	if err = s.db.PublishHTTPCacheEntry(entry, fence(st.fill.entry), replaces, now); err != nil {
		fsutil.Remove(body)
		return "", err
	}
	s.verified[st.id] = true
	// From now on requests find the entry; none may join the stream, so a
	// cleanup that retires the entry also keeps new readers away from it.
	st.published = st.id
	s.leaveFlightMapLocked(st)
	return st.id, nil
}

// endStream records the end of a fill. A failed stream leaves its flight
// before readers learn the failure, so new requests start a new fill; its
// part file is removed and nothing is published.
func (s *Service) endStream(st *stream, failure error, published string) {
	s.mu.Lock()
	s.leaveFlightMapLocked(st)
	st.finished = true
	st.published = published
	closeFile := st.readers == 0
	if closeFile {
		delete(s.streams, st.id)
	}
	s.mu.Unlock()
	if failure != nil {
		st.body.Finish(failure)
		fsutil.Remove(s.partPath(st.id))
	}
	if closeFile {
		st.body.CloseFile()
	}
	close(st.done)
}

// leaveFlightMapLocked stops new requests from joining st. A flight that has
// not resolved yet is removed when it resolves.
func (s *Service) leaveFlightMapLocked(st *stream) {
	if f := s.flights[st.key]; f != nil && f.resolved && f.result.stream == st {
		delete(s.flights, st.key)
	}
}

// dropReaderLocked releases one reader slot. When the last slot of an
// unfinished stream goes, the fill stops: nobody waits for the body any more.
// The stopping stream leaves the flight map at once, so a request arriving
// before the fill has ended starts a new fetch instead of joining it.
func (s *Service) dropReaderLocked(st *stream) (stop, closeFile bool) {
	st.readers--
	if st.readers > 0 {
		return false, false
	}
	if !st.finished {
		s.leaveFlightMapLocked(st)
		return true, false
	}
	delete(s.streams, st.id)
	return false, true
}

func (st *stream) drop(stop, closeFile bool) {
	if stop {
		st.cancel()
	}
	if closeFile {
		st.body.CloseFile()
	}
}

func (s *Service) leaveStream(st *stream) {
	s.mu.Lock()
	stop, closeFile := s.dropReaderLocked(st)
	s.mu.Unlock()
	st.drop(stop, closeFile)
}

// awaitPublication waits until the stream ended and returns its entry ID, or
// "" when nothing was published.
func (s *Service) awaitPublication(ctx context.Context, st *stream) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-st.done:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return st.published, nil
}
