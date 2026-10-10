package httpcache

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
)

// Serve returns errors only before writing response headers. It reserves shared
// reader capacity for the entire downstream response, including HEAD and 304.
func (s *Service) Serve(w http.ResponseWriter, r *http.Request, entry application.Entry, relativePath string) (resultErr error) {
	workCtx, finish, err := s.db.ApplicationWork(r.Context(), entry.StorageID())
	if err != nil {
		return err
	}
	defer finish()
	r = r.WithContext(workCtx)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return errors.New("HTTP cache supports GET and HEAD only")
	}
	if entry.Upstream == nil {
		return ErrUpstream
	}
	if _, err := entry.Upstream.RelativeURL(relativePath); err != nil {
		return err
	}
	if err := s.begin(entry); err != nil {
		return err
	}
	defer s.wg.Done()
	if err := s.db.AddFor(entry.MetricsID(), "artifact_requests", 1); err != nil {
		return err
	}
	mw := &metricWriter{ResponseWriter: w, s: s, app: entry.MetricsID()}
	w = mw
	defer func() {
		if recovered := recover(); recovered != nil {
			s.finishMetrics(mw, entry, relativePath, ErrUpstream)
			panic(recovered)
		}
		s.finishMetrics(mw, entry, relativePath, resultErr)
	}()
	release, err := s.budget.AcquireHTTPReader()
	if err != nil {
		return err
	}
	readerKey := entry.StorageID() + "\x00" + relativePath
	s.mu.Lock()
	s.readers[readerKey]++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.readers[readerKey]--
		if s.readers[readerKey] == 0 {
			delete(s.readers, readerKey)
		}
		s.mu.Unlock()
		release()
	}()
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(s.ctx, cancel)
	defer cancel()
	defer stop()
	policy, err := s.readPolicy(entry)
	if err != nil {
		return err
	}
	f := fill{entry: entry, path: relativePath, policy: policy}
	r = r.WithContext(ctx)
	// Shared freshness belongs to administrator rules and the source response.
	// Request no-store, no-cache, max-age and Pragma are ignored so an anonymous
	// client can never force a private upstream transfer or a per-request
	// revalidation; misses still coalesce through sharedFetch.
	_, onlyCached := directives(r.Header)["only-if-cached"]
	for tries := 0; ; tries++ {
		if tries == fetchAgainLimit {
			return ErrFetchContended
		}
		old, err := s.lookup(entry.StorageID(), relativePath)
		if err != nil {
			return err
		}
		// A previously explicit override is not an implicit permission after the
		// rule is removed or stops matching. Keep the body for ordinary cleanup,
		// but do not serve it or use it as a failure fallback under the new policy.
		cacheEligible := old != nil && f.eligible(old.headers)
		if cacheEligible && s.now().Before(f.freshness(old.headers, old.ValidatedAt)) {
			if err = s.db.AddFor(entry.MetricsID(), "cache_hit_requests", 1); err != nil {
				s.unpin(old.GenerationID)
				return err
			}
			defer s.unpin(old.GenerationID)
			return s.serveStored(w, r, old)
		}
		if onlyCached {
			if old != nil {
				s.unpin(old.GenerationID)
			}
			s.serveStatus(w, http.StatusGatewayTimeout)
			return nil
		}
		if r.Method == http.MethodHead {
			if old != nil {
				defer s.unpin(old.GenerationID)
			}
			return s.head(w, r, f, old)
		}
		result, fetchErr := s.sharedFetch(ctx, f, old)
		if old != nil {
			s.unpin(old.GenerationID)
		}
		if errors.Is(fetchErr, errUncacheableFlight) {
			// The upstream response, never a request header, made this path
			// uncacheable. Each follower transfers concurrently instead of
			// retrying one at a time.
			if err = s.db.AddFor(entry.MetricsID(), "miss_requests", 1); err != nil {
				return err
			}
			result, fetchErr = s.fetch(ctx, f, nil, false, nil)
		}
		if errors.Is(fetchErr, ErrFetchAgain) {
			continue
		}
		if fetchErr != nil {
			return fetchErr
		}
		return s.serveResult(w, r, result, relativePath)
	}
}

func safeHeaders(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(name)}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Cache-Control", "no-store")
}
func (s *Service) serveStatus(w http.ResponseWriter, status int) {
	if status < 400 || status > 599 {
		status = http.StatusBadGateway
	}
	http.Error(w, http.StatusText(status), status)
}
func requestRange(r *http.Request) *http.Request {
	if r.Method == http.MethodHead || strings.Contains(r.Header.Get("Range"), ",") {
		copy := r.Clone(r.Context())
		copy.Header.Del("Range")
		return copy
	}
	return r
}
func (s *Service) serveStored(w http.ResponseWriter, r *http.Request, row *Row) error {
	f, size, err := s.bodies().Open(row.GenerationID)
	if err != nil {
		return err
	}
	defer f.Close()
	if size != row.SizeBytes {
		return ErrUpstream
	}
	safeHeaders(w, row.Path)
	w.Header().Set("ETag", row.ETag)
	modified, _ := http.ParseTime(row.headers.Get("Last-Modified"))
	gate := &accessWriter{ResponseWriter: w, touch: func() error { return s.touch(row) }}
	reader := &contextReadSeeker{ctx: r.Context(), ReadSeekCloser: f}
	http.ServeContent(gate, requestRange(r), path.Base(row.Path), modified, reader)
	if reader.err != nil && !errors.Is(reader.err, io.EOF) {
		panic(http.ErrAbortHandler)
	}
	return nil
}

type accessWriter struct {
	http.ResponseWriter
	touch           func() error
	started, failed bool
}

func (w *accessWriter) WriteHeader(status int) {
	if w.started {
		return
	}
	w.started = true
	if status == http.StatusOK || status == http.StatusPartialContent || status == http.StatusNotModified {
		if err := w.touch(); err != nil {
			w.failed = true
			w.Header().Del("Content-Length")
			w.Header().Del("Content-Range")
			http.Error(w.ResponseWriter, "Failed to record cache access", http.StatusServiceUnavailable)
			return
		}
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *accessWriter) Write(p []byte) (int, error) {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}
	if w.failed {
		return 0, errors.New("Cache access persistence failed")
	}
	return w.ResponseWriter.Write(p)
}
func (s *Service) head(w http.ResponseWriter, r *http.Request, f fill, old *Row) error {
	entry, path := f.entry, f.path
	if err := s.db.AddFor(entry.MetricsID(), "miss_requests", 1); err != nil {
		return err
	}
	release, err := s.budget.AcquireHTTPWriter()
	if err != nil {
		return err
	}
	defer release()
	_, finish, err := s.startTransfer(entry, path)
	if err != nil {
		return err
	}
	defer finish()
	ctx, cancel := context.WithTimeout(r.Context(), upstreamOperationTimeout)
	defer cancel()
	attempts, err := s.sourceAttempts(entry)
	if err != nil {
		return err
	}
	var lastErr error = ErrUpstream
	for _, attempt := range attempts {
		if r.Context().Err() != nil {
			return r.Context().Err()
		}
		if ctx.Err() != nil {
			lastErr = ctx.Err()
			break
		}
		source, err := attempt.Client.RelativeURL(path)
		if err != nil {
			return err
		}
		headers := sourceValidators(old, source)
		resp, err := attempt.Client.Head(ctx, source, headers)
		if err != nil {
			if e := s.upstreamFailure(entry, path, 0); e != nil {
				return e
			}
			if !errors.Is(err, distributor.ErrConnection) && ctx.Err() == nil {
				return err
			}
			lastErr = err
			continue
		}
		if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
			resp.Body.Close()
			if e := s.upstreamFailure(entry, path, resp.StatusCode); e != nil {
				return e
			}
			lastErr = ErrUpstream
			continue
		}
		err = s.headResponse(w, r, f, old, attempt, headers, resp)
		resp.Body.Close()
		return err
	}
	result, err := s.fallback(r.Context(), f, old, lastErr)
	if err != nil {
		return err
	}
	defer s.unpin(result.row.GenerationID)
	return s.serveStored(w, r, result.row)
}

func (s *Service) headResponse(w http.ResponseWriter, r *http.Request, f fill, old *Row, attempt sourceAttempt, headers http.Header, resp *http.Response) error {
	path := f.path
	var err error
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		if err = s.retire(old); err != nil {
			return err
		}
		s.serveStatus(w, resp.StatusCode)
		return nil
	}
	if resp.StatusCode == http.StatusNotModified {
		initial, _ := attempt.Client.RelativeURL(path)
		if !validSourceNotModified(old, resp, initial, headers) {
			return ErrUpstream
		}
		combined := mergedHeaders(old.headers, resp.Header)
		if !f.eligible(resp.Header) || !f.eligible(combined) {
			if err = s.retire(old); err != nil {
				return err
			}
			return ErrUpstream
		}
		result, err := s.revalidate(f, old, combined)
		result, err = s.recordOverride(f, combined, result, err)
		if err != nil {
			return err
		}
		defer s.unpin(result.row.GenerationID)
		return s.serveStored(w, r, result.row)
	}
	if resp.StatusCode != http.StatusOK {
		s.serveStatus(w, resp.StatusCode)
		return nil
	}
	if old != nil {
		if !f.eligible(resp.Header) {
			if err = s.retire(old); err != nil {
				return err
			}
		} else {
			etag := old.headers.Get("ETag")
			initial, _ := attempt.Client.RelativeURL(path)
			same := old.SourceURL == responseSourceURL(resp, initial) && etag != "" && etag == resp.Header.Get("ETag") && resp.ContentLength == old.SizeBytes
			if same {
				combined := mergedHeaders(old.headers, resp.Header)
				result, err := s.revalidate(f, old, combined)
				result, err = s.recordOverride(f, combined, result, err)
				if err != nil {
					return err
				}
				defer s.unpin(result.row.GenerationID)
				return s.serveStored(w, r, result.row)
			}
		}
	}
	safeHeaders(w, path)
	if resp.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	for _, key := range []string{"ETag", "Last-Modified"} {
		if value := resp.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	status := preconditionStatus(r, resp.Header)
	if status != http.StatusOK {
		w.Header().Del("Content-Length")
	}
	w.WriteHeader(status)
	return nil
}

func (s *Service) serveResult(w http.ResponseWriter, r *http.Request, result fetchResult, name string) error {
	if result.response != nil {
		return s.serveDirect(w, r, result, name)
	}
	if result.row != nil {
		defer s.unpin(result.row.GenerationID)
		return s.serveStored(w, r, result.row)
	}
	s.serveStatus(w, result.status)
	return nil
}

func (s *Service) serveDirect(w http.ResponseWriter, r *http.Request, result fetchResult, name string) error {
	resp := result.response
	defer resp.Body.Close()
	safeHeaders(w, name)
	for _, key := range []string{"ETag", "Last-Modified"} {
		if value := resp.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	status := preconditionStatus(r, resp.Header)
	if status != http.StatusOK {
		w.WriteHeader(status)
		return nil
	}
	if resp.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	w.WriteHeader(http.StatusOK)
	limit := s.budget.MaxArtifactBytes()
	n, err := io.Copy(w, io.LimitReader(&contextReader{ctx: r.Context(), reader: resp.Body}, limit))
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	if n == limit {
		var extra [1]byte
		got, err := resp.Body.Read(extra[:])
		if got > 0 || err != nil && !errors.Is(err, io.EOF) {
			panic(http.ErrAbortHandler)
		}
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		panic(http.ErrAbortHandler)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type contextReadSeeker struct {
	err error
	ctx context.Context
	io.ReadSeekCloser
}

func (r *contextReadSeeker) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		r.err = err
		return 0, err
	}
	n, err := r.ReadSeekCloser.Read(p)
	if err != nil {
		r.err = err
	}
	return n, err
}
