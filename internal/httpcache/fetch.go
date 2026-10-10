package httpcache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
)

type fetchResult struct {
	row *Row // A pinned stored generation.
	// stream is a response being cached while it is read; the receiver holds
	// one reader slot and releases it with leaveStream.
	stream         *stream
	status         int
	response       *http.Response // An unshared response for one reader.
	stale          bool
	oldUnavailable bool
	blockReason    string
}

// fetchRun is one upstream operation for a cache operation. A shared run
// serves a flight: a cacheable response becomes a stream, which takes over
// the run's writer lease, the flight's cancellation and its work lifetime.
type fetchRun struct {
	fill
	shared  bool
	key     string
	cancel  context.CancelFunc
	finish  func()
	release func() // writer lease; nil once a stream or response owns it
}

// fetch obtains path from the configured sources in strategy order, moving to
// the next source only when one fails before responding. Without run.shared
// it never stores: it is an unconditional transfer (old must be nil) streamed
// only to its caller. There is no overall deadline; an upstream body fails
// only by stalling for the idle timeout.
func (s *Service) fetch(ctx context.Context, run *fetchRun, old *Row) (out fetchResult, err error) {
	release, err := s.budget.AcquireHTTPWriter()
	if err != nil {
		return fetchResult{}, err
	}
	run.release = release
	defer func() {
		if run.release == nil {
			return
		}
		if out.response != nil {
			out.response.Body = &leasedBody{ReadCloser: out.response.Body, release: run.release}
		} else {
			run.release()
		}
		run.release = nil
	}()
	attempts := run.attempts
	if attempts == nil {
		if attempts, err = s.sourceAttempts(run.entry); err != nil {
			return fetchResult{}, err
		}
	}
	var lastErr error = ErrUpstream
	for _, attempt := range attempts {
		if ctx.Err() != nil {
			return fetchResult{}, ctx.Err()
		}
		result, retry, fetchErr := s.fetchAttempt(ctx, run, old, attempt)
		if result.oldUnavailable {
			old = nil
		}
		if !retry {
			return result, fetchErr
		}
		lastErr = fetchErr
	}
	return s.fallback(ctx, run.fill, old, lastErr)
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

func (s *Service) get(ctx context.Context, client *distributor.Client, source string, headers http.Header) (*http.Response, error) {
	return client.Send(ctx, distributor.Request{Method: http.MethodGet, URL: source, Header: headers, IdleTimeout: s.idleTimeout})
}

func (s *Service) fetchAttempt(ctx context.Context, run *fetchRun, old *Row, attempt sourceAttempt) (out fetchResult, retry bool, err error) {
	source, err := attempt.Client.RelativeURL(run.path)
	if err != nil {
		return fetchResult{}, false, err
	}
	headers := sourceValidators(old, source)
	resp, err := s.get(ctx, attempt.Client, source, headers)
	if err != nil {
		if e := s.upstreamFailure(run.entry, run.path, 0); e != nil {
			return fetchResult{}, false, e
		}
		return fetchResult{}, errors.Is(err, distributor.ErrConnection) || ctx.Err() != nil, err
	}
	keep := false
	defer func() {
		if !keep {
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
	if blockReason == "" && run.shared {
		// From here on the response is bound to this source: the stream
		// resumes only from it and never falls back to another source.
		result, err := s.startStream(ctx, run, old, resp, source, attempt.Client)
		keep = err == nil
		return result, false, err
	}
	if blockReason != "" {
		if err = s.retire(old); err != nil {
			return fetchResult{}, false, err
		}
	}
	return s.direct(run, resp, blockReason, &keep)
}

// direct hands an uncacheable response to one reader. Once its headers and
// body are sent, a failure cannot transparently change sources.
func (s *Service) direct(run *fetchRun, resp *http.Response, blockReason string, keep *bool) (fetchResult, bool, error) {
	if resp.ContentLength > s.budget.MaxArtifactBytes() {
		return fetchResult{}, false, download.ErrArtifactLimit
	}
	resp.Body = &upstreamBody{ReadCloser: resp.Body, s: s, app: run.entry.MetricsID()}
	*keep = true
	return fetchResult{response: resp, status: resp.StatusCode, blockReason: blockReason}, false, nil
}

// The caller owns the writer lease. A newly uncacheable 304 requires an
// independent complete response from the same source, with no validators.
func (s *Service) fetchUnconditional(ctx context.Context, run *fetchRun, attempt sourceAttempt) (fetchResult, bool, error) {
	source, err := attempt.Client.RelativeURL(run.path)
	if err != nil {
		return fetchResult{}, false, err
	}
	resp, err := s.get(ctx, attempt.Client, source, nil)
	if err != nil {
		if e := s.upstreamFailure(run.entry, run.path, 0); e != nil {
			return fetchResult{}, false, e
		}
		return fetchResult{}, errors.Is(err, distributor.ErrConnection) || ctx.Err() != nil, err
	}
	keep := false
	defer func() {
		if !keep {
			resp.Body.Close()
		}
	}()
	if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
		if e := s.upstreamFailure(run.entry, run.path, resp.StatusCode); e != nil {
			return fetchResult{}, false, e
		}
		return fetchResult{}, true, ErrUpstream
	}
	if resp.StatusCode != http.StatusOK {
		return fetchResult{status: resp.StatusCode}, false, nil
	}
	return s.direct(run, resp, run.blockReason(resp.Header), &keep)
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

func (s *Service) revalidate(f fill, old *Row, headers http.Header) (fetchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	fresh := f.freshness(headers, now)
	raw, _ := json.Marshal(headers)
	if err := s.db.RevalidateHTTPCacheEntry(old.GenerationID, f.entry.StorageID(), fence(f.entry), now, fresh, raw); err != nil {
		if errors.Is(err, store.ErrConflict) {
			err = ErrUpstream
		}
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
