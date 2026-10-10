package httpcache

import (
	"net/http"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
)

func (s *Service) readPolicy(entry application.Entry) (*cachepolicy.Policy, error) {
	config := cachepolicy.Empty()
	if entry.UID != "" {
		var revision int64
		var err error
		config, revision, err = s.db.ReadHTTPPolicy(entry.Descriptor.ID)
		if err != nil {
			return nil, err
		}
		if revision != entry.Revision {
			if err := s.db.CheckSourceActive(entry.StorageID(), fence(entry)); err != nil {
				return nil, store.ErrSourceInactive
			}
		}
	}
	return cachepolicy.Compile(config)
}

func evaluatedFreshness(policy *cachepolicy.Policy, entry application.Entry, path string, headers http.Header, validatedAt time.Time) time.Time {
	decision := resolveCacheDecision(policy, entry, path)
	if decision.Explicit {
		return validatedAt.Add(time.Duration(decision.TTLSeconds) * time.Second)
	}
	return freshness(headers, decision.TTLSeconds, validatedAt)
}

// fill holds the inputs of one cache operation on one path: the application
// snapshot admitted for the request, the policy compiled for that snapshot
// and, for maintenance warm-ups, the budget callbacks and source order.
type fill struct {
	entry  application.Entry
	path   string // application-relative, without the leading slash
	policy *cachepolicy.Policy
	// warm marks a maintenance read, which never counts as a public request.
	warm bool
	// attempts, when set, replaces the configured source order; a warm-up
	// continues from the source whose HEAD response asked for a GET.
	attempts []sourceAttempt
	// observe is charged for each body chunk and check is given the declared
	// length; either may fail this caller's wait without failing the flight.
	observe func(int64) error
	check   func(int64) error
}

func (f fill) decision() CacheDecision { return resolveCacheDecision(f.policy, f.entry, f.path) }

// eligible reports whether a representation with these headers may be stored
// and served under the current policy.
func (f fill) eligible(h http.Header) bool { return cacheBlockReason(h, f.decision()) == "" }

func (f fill) blockReason(h http.Header) string { return cacheBlockReason(h, f.decision()) }

func (f fill) freshness(h http.Header, validatedAt time.Time) time.Time {
	return evaluatedFreshness(f.policy, f.entry, f.path, h, validatedAt)
}

func (f fill) staleFallback() bool { return f.policy == nil || f.policy.StaleFallback() }

// ListEntry evaluates the same current policy used for serving; changing a rule
// does not require rewriting all previously stored body metadata.
func (s *Service) ListEntry(entry application.Entry) ([]Row, error) {
	policy, err := s.readPolicy(entry)
	if err != nil {
		return nil, err
	}
	rows, err := s.listRows(entry.StorageID())
	if err != nil {
		return nil, err
	}
	evaluateFreshness(policy, entry, rows)
	return rows, nil
}

// EntryAfter positions a path-ordered page. Path is the last listed path; when
// Prefix is set it is only a prefix of that path and the first Skip current
// rows starting with it were already listed. Prefixes keep continuation tokens
// bounded for paths of up to 4096 bytes.
type EntryAfter struct {
	Path   string
	Prefix bool
	Skip   int
}

// ListEntryPage returns up to limit current files of entry's source epoch in
// path order after the given position, with freshness evaluated like ListEntry.
func (s *Service) ListEntryPage(entry application.Entry, after EntryAfter, limit int) ([]Row, error) {
	policy, err := s.readPolicy(entry)
	if err != nil {
		return nil, err
	}
	condition, fetch := "path>?", limit
	if after.Prefix {
		condition, fetch = "path>=?", limit+after.Skip
	}
	rows, err := s.queryRows(`SELECT `+columns+` FROM http_cache_generations WHERE storage_id=? AND is_current=1 AND `+condition+` ORDER BY path LIMIT ?`, entry.StorageID(), after.Path, fetch)
	if err != nil {
		return nil, err
	}
	if after.Prefix {
		// Rows sharing a prefix sort contiguously before every other row >= prefix.
		skipped := 0
		for skipped < after.Skip && skipped < len(rows) && strings.HasPrefix(rows[skipped].Path, after.Path) {
			skipped++
		}
		rows = rows[skipped:]
		if len(rows) > limit {
			rows = rows[:limit]
		}
	}
	evaluateFreshness(policy, entry, rows)
	return rows, nil
}

// evaluateFreshness applies the current policy to listed rows: a blocked
// response is never fresh.
func evaluateFreshness(policy *cachepolicy.Policy, entry application.Entry, rows []Row) {
	for i := range rows {
		rows[i].FreshUntil = evaluatedFreshness(policy, entry, rows[i].Path, rows[i].headers, rows[i].ValidatedAt)
		if cacheBlockReason(rows[i].headers, resolveCacheDecision(policy, entry, rows[i].Path)) != "" {
			rows[i].FreshUntil = rows[i].ValidatedAt
		}
	}
}

func (s *Service) List(storageID string) ([]Row, error) {
	uid, epoch, dynamic := identity.ParseStorageID(storageID)
	if !dynamic {
		return s.listRows(storageID)
	}
	var entry application.Entry
	entry.UID = uid
	entry.SourceEpoch = epoch
	err := s.db.DB.QueryRow(`SELECT v.id||'/'||a.id,a.cache_ttl_seconds,a.revision FROM applications a JOIN vendors v ON v.uid=a.vendor_uid WHERE a.uid=?`, uid).Scan(&entry.Descriptor.ID, &entry.Descriptor.DefaultChannelTTLSeconds, &entry.Revision)
	if err != nil {
		return nil, err
	}
	return s.ListEntry(entry)
}

// CacheDecision is fixed by the request's captured application revision. Source
// response headers are deliberately not consulted when an administrator has
// selected an explicit path rule.
type CacheDecision struct {
	TTLSeconds int
	Explicit   bool
	RuleID     string
	RuleIndex  int
	Revision   int64
}

func resolveCacheDecision(policy *cachepolicy.Policy, entry application.Entry, path string) CacheDecision {
	decision := CacheDecision{TTLSeconds: entry.Descriptor.DefaultChannelTTLSeconds, RuleIndex: -1, Revision: entry.Revision}
	if policy != nil {
		if rule, index, ok := policy.CacheRule("/" + path); ok {
			decision.TTLSeconds = rule.TTLSeconds
			decision.Explicit = true
			decision.RuleID = rule.ID
			decision.RuleIndex = index
		}
	}
	return decision
}
