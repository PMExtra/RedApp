package httpcache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/logging"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
)

var ErrInvalidPreview = errors.New("invalid maintenance preview")
var ErrPreviewBusy = errors.New("maintenance preview capacity reached")

const PreviewBuilderLimit = 8

// PreviewCriteria are the frozen inputs of an HTTP cache preview.
type PreviewCriteria struct {
	Match     pathmatch.Spec `json:"match"`
	Basis     string         `json:"basis,omitempty"`
	Before    time.Time      `json:"before,omitempty"`
	Path      string         `json:"path,omitempty"`
	Automatic bool           `json:"automatic,omitempty"`
}

// MaintenancePreview is an HTTP cache refresh or cleanup preview: the shared
// frozen preview of the store with its HTTP cache criteria.
type MaintenancePreview struct {
	ID             string
	Kind           string // cleanup or refresh
	State          store.PreviewState
	SourceEpoch    int64
	Match          pathmatch.Spec
	Basis          string
	Before         time.Time
	CreatedAt      time.Time
	ExpiresAt      time.Time
	ScannedFiles   int
	SelectedFiles  int
	SelectedBytes  int64
	ActiveFiles    int
	CompletedFiles int
	FailedFiles    int
	Result         json.RawMessage
	row            store.Preview
	criteria       PreviewCriteria
	lastScanned    int64
}

type buildOptions struct {
	after                  int64
	scanLimit, selectLimit int
	policy                 *cachepolicy.Policy
}

// previewKind is the store kind of an HTTP cache preview kind.
func previewKind(kind string) (store.PreviewKind, error) {
	switch kind {
	case "cleanup":
		return store.PreviewCacheCleanup, nil
	case "refresh":
		return store.PreviewCacheRefresh, nil
	}
	return "", ErrInvalidPreview
}

// Maintenance presents a stored HTTP cache preview.
func Maintenance(row store.Preview) (MaintenancePreview, error) {
	p := MaintenancePreview{ID: row.ID, State: row.State, SourceEpoch: row.SourceEpoch, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt, ScannedFiles: row.ScannedItems, SelectedFiles: row.SelectedItems, SelectedBytes: row.SelectedBytes, ActiveFiles: row.ActiveItems, CompletedFiles: row.CompletedItems, FailedFiles: row.FailedItems, Result: row.Result, row: row}
	switch row.Kind {
	case store.PreviewCacheCleanup:
		p.Kind = "cleanup"
	case store.PreviewCacheRefresh:
		p.Kind = "refresh"
	default:
		return p, ErrInvalidPreview
	}
	if err := json.Unmarshal(row.Criteria, &p.criteria); err != nil {
		return p, err
	}
	p.Match, p.Basis, p.Before = p.criteria.Match, p.criteria.Basis, p.criteria.Before
	return p, nil
}

// LookupPreview reads a readable preview of kind built for storageID's source
// epoch. Unknown previews and those of another source, application or kind
// are store.ErrNotFound; expired ones store.ErrExpired.
func (s *Service) LookupPreview(storageID, kind, id string) (MaintenancePreview, error) {
	k, err := previewKind(kind)
	if err != nil {
		return MaintenancePreview{}, err
	}
	uid, epoch, ok := identity.ParseStorageID(storageID)
	if !ok {
		return MaintenancePreview{}, store.ErrNotFound
	}
	row, err := s.db.Preview(uid, k, id, s.now())
	if err == nil && row.SourceEpoch != epoch {
		err = store.ErrNotFound
	}
	if err != nil {
		return MaintenancePreview{}, err
	}
	return Maintenance(row)
}

func (s *Service) buildPreview(ctx context.Context, entry application.Entry, kind string, criteria PreviewCriteria, options buildOptions) (out MaintenancePreview, buildErr error) {
	ctx, finish, err := s.db.ApplicationWork(ctx, entry.StorageID())
	if err != nil {
		return out, err
	}
	defer finish()
	ctx, release, err := s.beginMaintenance(ctx, true)
	if err != nil {
		return out, err
	}
	defer release()
	if err = s.prunePreviews(ctx, 1); err != nil {
		return out, err
	}
	storeKind, err := previewKind(kind)
	if err != nil {
		return out, err
	}
	if criteria.Match.Type == "" && criteria.Match.Pattern == "" {
		criteria.Match = pathmatch.Spec{Type: "glob", Pattern: "/"}
	}
	matcher, err := pathmatch.Compile(criteria.Match)
	if err != nil {
		return out, fmt.Errorf("%w: %w", ErrInvalidCleanup, err)
	}
	if criteria.Path != "" && pathmatch.ValidatePath("/"+criteria.Path) != nil {
		return out, ErrInvalidPreview
	}
	if kind == "cleanup" && !criteria.Automatic && ((criteria.Basis != "fetched_at" && criteria.Basis != "last_access") || criteria.Before.IsZero() || criteria.Before.After(s.now())) {
		return out, ErrInvalidCleanup
	}
	now := s.now().UTC()
	raw, err := json.Marshal(criteria)
	if err != nil {
		return out, err
	}
	id, err := fsutil.RandomID()
	if err != nil {
		return out, err
	}
	// Refresh and automatic cleanup act on a serving source; manual cleanup
	// also on disabled or old sources.
	row, err := s.db.CreatePreview(ctx, store.Preview{
		ID: id, Kind: storeKind, AppUID: entry.UID, SourceEpoch: entry.SourceEpoch, Fence: fence(entry),
		RequireActive: kind == "refresh" || criteria.Automatic, State: store.PreviewBuilding,
		CreatedAt: now, ExpiresAt: now.Add(store.PreviewLifetime), Criteria: raw,
	}, nil)
	if err != nil {
		return out, err
	}
	defer func() {
		if buildErr != nil {
			failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// A preview left building is discarded once it expires.
			if err := errors.Join(s.db.FailPreviewBuild(failureCtx, id, s.now()), s.prunePreviews(failureCtx, 1)); err != nil {
				s.log.Warn("failed HTTP cache preview was not discarded", slog.String("app", entry.Descriptor.ID), slog.String("preview_id", id), logging.Error(err))
			}
		}
	}()
	after := options.after
	scanned, selected := 0, 0
	for done := false; !done; {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		limit := 1000
		if options.scanLimit > 0 && options.scanLimit-scanned < limit {
			limit = options.scanLimit - scanned
		}
		if limit <= 0 {
			break
		}
		page, err := s.freezePreviewPage(ctx, row, criteria, matcher, options, after, limit, options.selectLimit-selected)
		if err != nil {
			return out, err
		}
		scanned += page.Scanned
		selected += page.Selected
		if page.Scanned > 0 {
			after = page.Last
		}
		done = page.Scanned < limit || options.scanLimit > 0 && scanned >= options.scanLimit || options.selectLimit > 0 && selected >= options.selectLimit
	}
	if err = s.db.FinishPreviewBuild(ctx, row); err != nil {
		return out, err
	}
	out, err = s.LookupPreview(entry.StorageID(), kind, id)
	out.lastScanned = after
	return out, err
}

// freezePreviewPage selects one page under mu so that the active count sees
// a consistent set of reader pins.
func (s *Service) freezePreviewPage(ctx context.Context, row store.Preview, criteria PreviewCriteria, matcher *pathmatch.Matcher, options buildOptions, after int64, limit, remaining int) (store.HTTPPreviewPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	selected := 0
	var choiceErr error
	page, err := s.db.FreezeHTTPCachePreviewPage(ctx, row, after, limit, func(e store.HTTPCacheEntry) store.HTTPCacheChoice {
		r, err := rowFromEntry(e)
		if err != nil {
			choiceErr = err
			return store.HTTPCacheChoice{Stop: true}
		}
		if criteria.Path != "" && r.Path != criteria.Path || !matcher.Match("/"+r.Path) {
			return store.HTTPCacheChoice{}
		}
		basis, before, ruleIndex := criteria.Basis, criteria.Before, -1
		if criteria.Automatic {
			rule, index, ok := options.policy.CleanupRule("/" + r.Path)
			if !ok {
				return store.HTTPCacheChoice{}
			}
			basis, before, ruleIndex = rule.Basis, s.now().UTC().Add(-time.Duration(rule.AgeSeconds)*time.Second), index
		}
		if row.Kind == store.PreviewCacheCleanup {
			target := r.FetchedAt
			if basis == "last_access" && r.LastAccessAt != nil {
				target = *r.LastAccessAt
			}
			if !target.Before(before) {
				return store.HTTPCacheChoice{}
			}
		}
		selected++
		return store.HTTPCacheChoice{
			Selected: true,
			Active:   s.pins[r.GenerationID] > 0,
			Stop:     options.selectLimit > 0 && selected >= remaining,
			Detail:   store.HTTPCachePreviewDetail{AccessBucket: r.accessBucket, Basis: basis, RuleIndex: ruleIndex},
		}
	})
	if err == nil {
		err = choiceErr
	}
	return page, err
}

// claimPreview starts executing a ready preview of entry's source epoch. A
// finished preview is returned as it is.
func (s *Service) claimPreview(ctx context.Context, entry application.Entry, kind, id string) (MaintenancePreview, error) {
	k, err := previewKind(kind)
	if err != nil {
		return MaintenancePreview{}, err
	}
	row, _, err := s.db.ClaimPreview(ctx, entry.StorageID(), k, id, s.now())
	if err != nil {
		return MaintenancePreview{}, err
	}
	return Maintenance(row)
}

func (s *Service) pendingPreviewItems(ctx context.Context, preview MaintenancePreview, after int64, limit int) ([]store.PreviewItem, error) {
	return s.db.PreviewItems(ctx, preview.ID, after, limit, true)
}

func (s *Service) recordPreviewItem(ctx context.Context, preview MaintenancePreview, ordinal int64, status, code string) error {
	return s.db.RecordPreviewItems(ctx, preview.row, []store.PreviewOutcome{{Ordinal: ordinal, Status: status, ErrorCode: code}})
}

func (s *Service) finishPreview(ctx context.Context, preview MaintenancePreview, result any, failed bool) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	state := store.PreviewDone
	if failed {
		state = store.PreviewFailed
	}
	return s.db.FinishPreview(ctx, preview.row, state, raw, s.now())
}

// Row is the stored preview.
func (p MaintenancePreview) Row() store.Preview { return p.row }
