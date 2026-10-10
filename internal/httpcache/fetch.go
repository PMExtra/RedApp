package httpcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/store"
)

type fetchResult struct {
	row            *Row           // A pinned stored generation.
	staged         *fsutil.Staged // A sealed body not yet published.
	status         int
	response       *http.Response
	stale          bool
	oldUnavailable bool
	blockReason    string
}

const upstreamOperationTimeout = 9 * time.Minute

// fetchRun is one upstream transfer: the cache operation it serves, the
// transfer row shown to administrators and, for a shared flight, the waiters
// charged for its bytes.
type fetchRun struct {
	fill
	transferID string
	flight     *flight // nil for an unshared transfer
}

// fetch transfers path from the configured sources. Without allowStore it never
// publishes: it is an unconditional transfer (old must be nil) streamed only to
// its caller. shared is the flight the transfer runs for, if any.
func (s *Service) fetch(ctx context.Context, f fill, old *Row, allowStore bool, shared *flight) (out fetchResult, err error) {
	release, err := s.budget.AcquireHTTPWriter()
	if err != nil {
		return fetchResult{}, err
	}
	transferID, finish, err := s.startTransfer(f.entry, f.path)
	if err != nil {
		release()
		return fetchResult{}, err
	}
	run := &fetchRun{fill: f, transferID: transferID, flight: shared}
	// Internal budget exhaustion is an upstream failure; caller cancellation or
	// shutdown is not. Keep the outer context for the final fallback decision.
	upstreamCtx, cancel := context.WithTimeout(ctx, upstreamOperationTimeout)
	releaseBudget := release
	release = func() { cancel(); finish(); releaseBudget() }
	defer func() {
		if out.response == nil {
			release()
		} else {
			out.response.Body = &leasedBody{ReadCloser: out.response.Body, release: release}
		}
	}()
	attempts := f.attempts
	if attempts == nil {
		if attempts, err = s.sourceAttempts(f.entry); err != nil {
			return fetchResult{}, err
		}
	}
	var lastErr error = ErrUpstream
	for _, attempt := range attempts {
		if ctx.Err() != nil {
			return fetchResult{}, ctx.Err()
		}
		if upstreamCtx.Err() != nil {
			lastErr = upstreamCtx.Err()
			break
		}
		result, retry, fetchErr := s.fetchAttempt(upstreamCtx, run, old, allowStore, attempt)
		if result.oldUnavailable {
			old = nil
		}
		if !retry {
			return result, fetchErr
		}
		lastErr = fetchErr
	}
	return s.fallback(ctx, f, old, lastErr)
}

// Validators are source-specific. Even equal ETags from different configured
// sources do not establish that their bodies are identical.
func sourceValidators(old *Row, source string) http.Header {
	headers := http.Header{}
	if old != nil && old.SourceURL == source {
		if tag := old.headers.Get("ETag"); tag != "" {
			headers.Set("If-None-Match", tag)
		} else if modified := old.headers.Get("Last-Modified"); modified != "" {
			headers.Set("If-Modified-Since", modified)
		}
	}
	return headers
}

func responseSourceURL(resp *http.Response, initial string) string {
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String()
	}
	return initial // Direct injected fixture transports may omit Request.
}
func validSourceNotModified(old *Row, resp *http.Response, initial string, sent http.Header) bool {
	if old == nil || len(sent) == 0 || responseSourceURL(resp, initial) != old.SourceURL || !validNotModified(old, resp.Header) {
		return false
	}
	// Redirects clear validators. An unsolicited 304 after a redirect must not
	// reuse an old body, even if a redirect chain returns to the initial URL.
	return resp.Request == nil || resp.Request.Header.Get("If-None-Match") != "" || resp.Request.Header.Get("If-Modified-Since") != ""
}

func (s *Service) fetchAttempt(ctx context.Context, run *fetchRun, old *Row, allowStore bool, attempt sourceAttempt) (out fetchResult, retry bool, err error) {
	source, err := attempt.Client.RelativeURL(run.path)
	if err != nil {
		return fetchResult{}, false, err
	}
	headers := sourceValidators(old, source)
	resp, err := attempt.Client.Get(ctx, source, headers)
	if err != nil {
		if e := s.upstreamFailure(run.entry, run.path, 0); e != nil {
			return fetchResult{}, false, e
		}
		return fetchResult{}, errors.Is(err, distributor.ErrConnection) || ctx.Err() != nil, err
	}
	resp.Body = &metricBody{ReadCloser: &sourceBody{ReadCloser: resp.Body}, s: s, app: run.entry.MetricsID(), transferID: run.transferID}
	defer func() {
		if out.response != resp {
			resp.Body.Close()
		}
	}()
	if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
		if e := s.upstreamFailure(run.entry, run.path, resp.StatusCode); e != nil {
			return fetchResult{}, false, e
		}
		return fetchResult{}, true, ErrUpstream
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		if err = s.retire(old); err != nil {
			return fetchResult{}, false, err
		}
		return fetchResult{status: resp.StatusCode}, false, nil
	}
	if resp.StatusCode == http.StatusNotModified {
		if !validSourceNotModified(old, resp, source, headers) {
			return fetchResult{}, false, ErrUpstream
		}
		combined := mergedHeaders(old.headers, resp.Header)
		if !run.eligible(resp.Header) || !run.eligible(combined) {
			if err = s.retire(old); err != nil {
				return fetchResult{}, false, err
			}
			result, retry, err := s.fetchUnconditional(ctx, run, attempt)
			result.oldUnavailable = true
			if result.blockReason == "" {
				result.blockReason = run.blockReason(resp.Header)
			}
			if result.blockReason == "" {
				result.blockReason = run.blockReason(combined)
			}
			return result, retry, err
		}
		result, err := s.revalidate(run.fill, old, combined)
		result, err = s.recordOverride(run.fill, combined, result, err)
		return result, false, err
	}
	if resp.StatusCode != http.StatusOK {
		return fetchResult{status: resp.StatusCode}, false, nil
	}
	blockReason := run.blockReason(resp.Header)
	cacheable := blockReason == ""
	if !cacheable || !allowStore {
		if !cacheable {
			if err = s.retire(old); err != nil {
				return fetchResult{}, false, err
			}
		}
		if resp.ContentLength > s.budget.MaxArtifactBytes() {
			return fetchResult{}, false, download.ErrArtifactLimit
		}
		// Uncacheable responses stream only to this reader. Once downstream
		// headers/body start, a failure cannot transparently change sources.
		return fetchResult{response: resp, status: resp.StatusCode, blockReason: blockReason}, false, nil
	}
	s.checkFetchLength(run.flight, resp.ContentLength)
	if ctx.Err() != nil {
		return fetchResult{}, false, ctx.Err()
	}
	result, err := s.spool(ctx, run, resp)
	if err != nil {
		var readErr *sourceReadError
		retry := errors.As(err, &readErr) || ctx.Err() != nil
		if retry {
			if e := s.upstreamFailure(run.entry, run.path, resp.StatusCode); e != nil {
				return fetchResult{}, false, e
			}
		}
		return fetchResult{}, retry, err
	}
	result.row.SourceURL = responseSourceURL(resp, source)
	result, err = s.publish(run.fill, old, result)
	result, err = s.recordOverride(run.fill, resp.Header, result, err)
	return result, false, err
}

// The caller owns the writer lease. A newly uncacheable 304 requires an
// independent complete response from the same source, with no validators.
func (s *Service) fetchUnconditional(ctx context.Context, run *fetchRun, attempt sourceAttempt) (fetchResult, bool, error) {
	source, err := attempt.Client.RelativeURL(run.path)
	if err != nil {
		return fetchResult{}, false, err
	}
	resp, err := attempt.Client.Get(ctx, source, nil)
	if err != nil {
		if e := s.upstreamFailure(run.entry, run.path, 0); e != nil {
			return fetchResult{}, false, e
		}
		return fetchResult{}, errors.Is(err, distributor.ErrConnection) || ctx.Err() != nil, err
	}
	resp.Body = &metricBody{ReadCloser: resp.Body, s: s, app: run.entry.MetricsID(), transferID: run.transferID}
	if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
		resp.Body.Close()
		if e := s.upstreamFailure(run.entry, run.path, resp.StatusCode); e != nil {
			return fetchResult{}, false, e
		}
		return fetchResult{}, true, ErrUpstream
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fetchResult{status: resp.StatusCode}, false, nil
	}
	if resp.ContentLength > s.budget.MaxArtifactBytes() {
		resp.Body.Close()
		return fetchResult{}, false, download.ErrArtifactLimit
	}
	return fetchResult{response: resp, status: resp.StatusCode, blockReason: run.blockReason(resp.Header)}, false, nil
}

type sourceReadError struct{ err error }

func (e *sourceReadError) Error() string { return "Upstream body did not complete" }
func (e *sourceReadError) Unwrap() error { return e.err }

type sourceBody struct{ io.ReadCloser }

func (b *sourceBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = &sourceReadError{err}
	}
	return n, err
}
func (s *Service) fallback(ctx context.Context, f fill, old *Row, cause error) (fetchResult, error) {
	if ctx.Err() != nil {
		return fetchResult{}, ctx.Err()
	}
	if old == nil || !f.staleFallback() || !f.eligible(old.headers) {
		return fetchResult{}, cause
	}
	// General HTTP deliberately favors availability after an upstream failure,
	// even when source directives require revalidation. Freshness and validators
	// still govern normal requests; fallback never advances their timestamps.
	// old is the pinned complete representation selected within this source epoch.
	row, err := s.pin(old.GenerationID)
	if err != nil {
		return fetchResult{}, err
	}
	decision := f.decision()
	message := fmt.Sprintf("Upstream failed; serving a previously cached representation (rule %q, revision %d)", decision.RuleID, decision.Revision)
	if directives := overrideDirectives(old.headers); len(directives) > 0 {
		message += "; cached source Cache-Control: " + strings.Join(directives, ", ")
	}
	if err = s.db.RecordEvent(store.Event{AppID: f.entry.MetricsID(), ResourceKey: f.path, Category: "warning", Code: "stale_cache_fallback", Message: message}); err != nil {
		s.unpin(row.GenerationID)
		return fetchResult{}, err
	}
	return fetchResult{row: row, status: http.StatusOK, stale: true}, nil
}
func (s *Service) spool(ctx context.Context, run *fetchRun, resp *http.Response) (fetchResult, error) {
	limit := s.budget.MaxArtifactBytes()
	if resp.ContentLength > limit {
		return fetchResult{}, download.ErrArtifactLimit
	}
	id, err := fsutil.RandomID()
	if err != nil {
		return fetchResult{}, err
	}
	path := filepath.Join(s.dir, id+".tmp")
	s.mu.Lock()
	if t := s.transfers[run.transferID]; t != nil {
		t.filePath = path
		t.diskBytes = 0 // A retry uses a new complete staging body, never a partial continuation.
	}
	s.mu.Unlock()
	f, err := fsutil.CreateStaged(path)
	if err != nil {
		return fetchResult{}, err
	}
	keep := false
	defer func() {
		if !keep {
			f.Discard()
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash, fetchObserver{s: s, flight: run.flight}, &spoolMeter{s: s, id: run.transferID}), io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fetchResult{}, err
	}
	if ctx.Err() != nil {
		return fetchResult{}, ctx.Err()
	}
	if n > limit {
		return fetchResult{}, download.ErrArtifactLimit
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return fetchResult{}, &sourceReadError{io.ErrUnexpectedEOF}
	}
	if err = f.Seal(); err != nil {
		return fetchResult{}, err
	}
	keep = true
	now := s.now().UTC()
	row := &Row{GenerationID: id, SizeBytes: n, SHA256: hex.EncodeToString(hash.Sum(nil)), FetchedAt: now, ValidatedAt: now, headers: representationHeaders(resp.Header)}
	return fetchResult{row: row, staged: f, status: resp.StatusCode}, nil
}
func (s *Service) publish(f fill, old *Row, result fetchResult) (fetchResult, error) {
	r := result.row
	r.storageID = f.entry.StorageID()
	r.Path = f.path
	r.current = true
	r.FreshUntil = f.freshness(r.headers, r.ValidatedAt)
	// The temporary row is not a pinned persistent generation yet.
	result.row = nil
	defer result.staged.Discard()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.HTTPCacheDB().Begin()
	if err != nil {
		return fetchResult{}, err
	}
	defer tx.Rollback()
	if err = s.db.RequireSourceActive(tx, r.storageID, fence(f.entry)); err != nil {
		return fetchResult{}, err
	}
	if old != nil {
		var currentID string
		if err = tx.QueryRow(`SELECT id FROM http_cache_generations WHERE storage_id=? AND path=? AND is_current=1`, r.storageID, f.path).Scan(&currentID); err != nil || currentID != old.GenerationID {
			return fetchResult{}, ErrUpstream
		}
	}
	published := false
	defer func() {
		if !published {
			fsutil.Remove(s.bodyPath(r.GenerationID))
		}
	}()
	if err = result.staged.Publish(s.bodyPath(r.GenerationID)); err != nil {
		return fetchResult{}, err
	}
	headers, _ := json.Marshal(r.headers)
	if _, err = tx.Exec(`UPDATE http_cache_generations SET is_current=0,retired_at_s=COALESCE(retired_at_s,?) WHERE storage_id=? AND path=? AND is_current=1`, s.now().Unix(), r.storageID, f.path); err != nil {
		return fetchResult{}, err
	}
	_, err = tx.Exec(`INSERT INTO http_cache_generations(id,storage_id,path,sha256,size_bytes,fetched_at_s,validated_at_s,last_access_bucket_s,fresh_until_s,headers_json,source_url,is_current) VALUES(?,?,?,?,?,?,?,?,?,?,?,1)`, r.GenerationID, r.storageID, f.path, r.SHA256, r.SizeBytes, r.FetchedAt.Unix(), r.ValidatedAt.Unix(), 0, r.FreshUntil.Unix(), headers, r.SourceURL)
	if err != nil {
		return fetchResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return fetchResult{}, err
	}
	published = true
	s.pins[r.GenerationID]++
	r.ETag = r.headers.Get("ETag")
	if r.ETag == "" {
		r.ETag = `"sha256-` + r.SHA256 + `"`
	}
	return fetchResult{row: r, status: http.StatusOK}, nil
}
func (s *Service) revalidate(f fill, old *Row, headers http.Header) (fetchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.HTTPCacheDB().Begin()
	if err != nil {
		return fetchResult{}, err
	}
	defer tx.Rollback()
	if err = s.db.RequireSourceActive(tx, f.entry.StorageID(), fence(f.entry)); err != nil {
		return fetchResult{}, err
	}
	now := s.now().UTC()
	fresh := f.freshness(headers, now)
	raw, _ := json.Marshal(headers)
	result, err := tx.Exec(`UPDATE http_cache_generations SET validated_at_s=?,fresh_until_s=?,headers_json=? WHERE id=? AND is_current=1`, now.Unix(), fresh.Unix(), raw, old.GenerationID)
	if err != nil {
		return fetchResult{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fetchResult{}, err
	}
	if n != 1 {
		return fetchResult{}, ErrUpstream
	}
	if err = tx.Commit(); err != nil {
		return fetchResult{}, err
	}
	row := *old
	row.headers = headers
	row.ValidatedAt = now
	row.FreshUntil = fresh
	row.ETag = headers.Get("ETag")
	if row.ETag == "" {
		row.ETag = `"sha256-` + row.SHA256 + `"`
	}
	s.pins[row.GenerationID]++
	return fetchResult{row: &row, status: http.StatusOK}, nil
}

type leasedBody struct {
	io.ReadCloser
	release func()
}

func (b *leasedBody) Close() error { err := b.ReadCloser.Close(); b.release(); return err }

type fetchObserver struct {
	s      *Service
	flight *flight
}

func (o fetchObserver) Write(p []byte) (int, error) {
	o.s.observeFetch(o.flight, int64(len(p)))
	return len(p), nil
}
