package httpcache

import (
	"context"
	"errors"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/warmplan"
	"strings"
)

type warmAttemptsKey struct{}
type warmContextKey struct{}

// Warm is an admitted maintenance read, never a synthetic public GET or access touch.
func (s *Service) Warm(ctx context.Context, entry application.Entry, path string, budget *warmplan.Budget) (item warmplan.Item) {
	item = warmplan.Item{Key: path, Status: "failed", Reason: "warm_failed"}
	before := budget.Used()
	defer func() { item.Bytes = budget.Used() - before }()
	ctx, finish, err := s.db.ApplicationWork(ctx, entry.StorageID())
	if err != nil {
		return item
	}
	defer finish()
	if err = s.begin(entry); err != nil {
		return item
	}
	defer s.wg.Done()
	release, err := s.budget.AcquireHTTPReader()
	if err != nil {
		item.Reason = "capacity"
		return item
	}
	defer release()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer cancel()
	defer stop()
	if pathmatch.ValidatePath(path) != nil || path == "/" || len(path) > 4096 || entry.Provider != application.HttpCache {
		return item
	}
	relative := strings.TrimPrefix(path, "/")
	policy, err := s.readPolicy(entry)
	if err != nil {
		return item
	}
	ctx = context.WithValue(ctx, policyContextKey{}, policy)
	ctx = context.WithValue(ctx, warmContextKey{}, true)
	ctx = context.WithValue(ctx, fetchObserverKey{}, func(n int64) error { return budget.Consume(n) })
	ctx = context.WithValue(ctx, fetchLengthKey{}, func(n int64) error { return budget.CheckLength(n) })
	for tries := 0; tries < 16; tries++ {
		if ctx.Err() != nil {
			item.Reason = "cancelled"
			return item
		}
		old, err := s.lookup(entry.StorageID(), relative)
		if err != nil {
			return item
		}
		result := s.warmCurrent(ctx, entry, relative, old, budget)
		if old != nil {
			s.unpin(old.GenerationID)
		}
		if result.Reason == "generation_changed" {
			continue
		}
		result.Key = path
		return result
	}
	item.Reason = "generation_changed"
	return item
}
func (s *Service) warmCurrent(ctx context.Context, entry application.Entry, path string, old *Row, budget *warmplan.Budget) warmplan.Item {
	failed := warmplan.Item{Status: "failed", Reason: "upstream_failed"}
	attempts, err := s.sourceAttempts(entry)
	if err != nil {
		return failed
	}
	if old != nil {
		for i, a := range attempts {
			release, err := s.budget.AcquireHTTPWriter()
			if err != nil {
				return warmplan.Item{Status: "failed", Reason: "capacity"}
			}
			headers := sourceValidators(old, a.Client.URL(path))
			resp, headErr := a.Client.Head(ctx, a.Client.URL(path), headers)
			release()
			if headErr != nil || resp.StatusCode >= 500 && resp.StatusCode != 501 {
				_ = s.upstreamFailure(entry, path, 0)
				if resp != nil {
					resp.Body.Close()
				}
				if i+1 < len(attempts) {
					continue
				}
				fallback, err := s.fallback(ctx, entry, path, old, ErrUpstream)
				if err == nil && fallback.row != nil {
					s.unpin(fallback.row.GenerationID)
					return warmplan.Item{Status: "stale_fallback", Reason: "head_failed"}
				}
				return warmplan.Item{Status: "failed", Reason: "head_failed"}
			}
			resp.Body.Close()
			if resp.Uncompressed || resp.Header.Get("Content-Encoding") != "" && !strings.EqualFold(resp.Header.Get("Content-Encoding"), "identity") {
				return warmplan.Item{Status: "failed", Reason: "invalid_encoding"}
			}
			if resp.StatusCode == 404 || resp.StatusCode == 410 {
				_ = s.retire(old)
				return warmplan.Item{Status: "failed", Reason: "not_found"}
			}
			if resp.StatusCode == 304 {
				if !validSourceNotModified(old, resp, a.Client.URL(path), headers) {
					return warmplan.Item{Status: "failed", Reason: "invalid_not_modified"}
				}
				combined := mergedHeaders(old.headers, resp.Header)
				if !contextEligible(ctx, entry, path, combined) || !contextEligible(ctx, entry, path, resp.Header) {
					_ = s.retire(old)
					return warmplan.Item{Status: "not_cacheable", Reason: "source_policy"}
				}
				result, err := s.revalidate(ctx, entry, old, combined)
				if err != nil {
					return warmplan.Item{Status: "failed", Reason: "generation_changed"}
				}
				s.unpin(result.row.GenerationID)
				return warmplan.Item{Status: "not_modified"}
			}
			if resp.StatusCode == 200 {
				if !contextEligible(ctx, entry, path, resp.Header) {
					_ = s.retire(old)
					return warmplan.Item{Status: "not_cacheable", Reason: "source_policy"}
				}
				sameSource := old.SourceURL == responseSourceURL(resp, a.Client.URL(path))
				oldTag, newTag := old.headers.Get("ETag"), resp.Header.Get("ETag")
				oldModified, newModified := old.headers.Get("Last-Modified"), resp.Header.Get("Last-Modified")
				changed := sameSource && (oldTag != "" && newTag != "" && oldTag != newTag || oldModified != "" && newModified != "" && oldModified != newModified || resp.ContentLength >= 0 && resp.ContentLength != old.SizeBytes)
				if sameSource && oldTag != "" && oldTag == newTag && resp.ContentLength == old.SizeBytes {
					result, err := s.revalidate(ctx, entry, old, mergedHeaders(old.headers, resp.Header))
					if err != nil {
						return warmplan.Item{Status: "failed", Reason: "generation_changed"}
					}
					s.unpin(result.row.GenerationID)
					return warmplan.Item{Status: "not_modified"}
				}
				if !changed && s.now().Before(contextFreshness(ctx, entry, path, old.headers, old.ValidatedAt)) {
					if err := s.warmCurrentGeneration(entry, old.GenerationID); err != nil {
						return warmplan.Item{Reason: "generation_changed"}
					}
					return warmplan.Item{Status: "ttl_fallback"}
				}
			} else if resp.StatusCode != 405 && resp.StatusCode != 501 {
				return failed
			}
			ctx = context.WithValue(ctx, warmAttemptsKey{}, attempts[i:])
			break
		}
	}
	result, err := s.sharedFetch(ctx, entry, path, old)
	if errors.Is(err, ErrFetchAgain) {
		return warmplan.Item{Status: "failed", Reason: "generation_changed"}
	}
	if errors.Is(err, warmplan.ErrLimited) {
		return warmplan.Item{Status: "skipped", Reason: "read_limit"}
	}
	if err != nil {
		return failed
	}
	if result.response != nil {
		result.response.Body.Close()
		return warmplan.Item{Status: "not_cacheable", Reason: result.blockReason}
	}
	if result.row == nil {
		return failed
	}
	defer s.unpin(result.row.GenerationID)
	if result.stale {
		return warmplan.Item{Status: "stale_fallback", Reason: "get_failed"}
	}
	if old != nil && result.row.GenerationID == old.GenerationID {
		return warmplan.Item{Status: "not_modified"}
	}
	return warmplan.Item{Status: "downloaded"}
}

// TTL fallback must observe the generation and runtime fence in one DB snapshot.
// It does not move validated_at or acquire a new public access timestamp.
func (s *Service) warmCurrentGeneration(entry application.Entry, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.db.RequireSourceActive(tx, entry.StorageID(), fence(entry)); err != nil {
		return err
	}
	var current bool
	if err = tx.QueryRow(`SELECT is_current FROM http_cache_generations WHERE id=?`, id).Scan(&current); err != nil {
		return err
	}
	if !current {
		return ErrFetchAgain
	}
	return tx.Commit()
}
