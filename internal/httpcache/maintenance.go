package httpcache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
)

var ErrPreviewRunning = errors.New("maintenance preview is already running")
var ErrInvalidPreview = errors.New("invalid maintenance preview")
var ErrPreviewBusy = errors.New("maintenance preview capacity reached")

const PreviewBuilderLimit = 8

type PreviewCriteria struct {
	Match     pathmatch.Spec `json:"match"`
	Basis     string         `json:"basis,omitempty"`
	Before    time.Time      `json:"before,omitempty"`
	Path      string         `json:"path,omitempty"`
	Automatic bool           `json:"automatic,omitempty"`
}
type MaintenancePreview struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	State          string          `json:"state"`
	Match          pathmatch.Spec  `json:"match"`
	Basis          string          `json:"basis,omitempty"`
	Before         time.Time       `json:"before,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	ExpiresAt      time.Time       `json:"expires_at"`
	ScannedFiles   int             `json:"scanned_files"`
	SelectedFiles  int             `json:"selected_files"`
	SelectedBytes  int64           `json:"selected_bytes"`
	ActiveFiles    int             `json:"active_files"`
	CompletedFiles int             `json:"completed_files"`
	FailedFiles    int             `json:"failed_files"`
	Result         json.RawMessage `json:"result,omitempty"`
	criteria       PreviewCriteria
	storageID      string
	fence          store.SourceFence
	executedAt     *time.Time
	result         json.RawMessage
	lastScanned    int64
}
type PreviewItem struct {
	Ordinal      int64          `json:"ordinal"`
	GenerationID string         `json:"generation_id"`
	Path         string         `json:"path"`
	SizeBytes    int64          `json:"size_bytes"`
	AccessBucket int64          `json:"access_bucket"`
	Basis        string         `json:"basis,omitempty"`
	Before       time.Time      `json:"before,omitempty"`
	Match        pathmatch.Spec `json:"match"`
	RuleIndex    int            `json:"rule_index"`
	ResultStatus string         `json:"result_status"`
	ErrorCode    string         `json:"error_code,omitempty"`
}
type PreviewPage struct {
	Items      []PreviewItem `json:"items"`
	NextCursor string        `json:"next_cursor"`
	TotalFiles int           `json:"total_files"`
	TotalBytes int64         `json:"total_bytes"`
	State      string        `json:"state"`
}
type buildOptions struct {
	after                  int64
	scanLimit, selectLimit int
	policy                 *cachepolicy.Policy
}

func previewFromStore(row store.HTTPPreview) (MaintenancePreview, error) {
	p := MaintenancePreview{ID: row.ID, Kind: row.Kind, State: row.State, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt, ScannedFiles: row.ScannedFiles, SelectedFiles: row.SelectedFiles, SelectedBytes: row.SelectedBytes, CompletedFiles: row.CompletedFiles, FailedFiles: row.FailedFiles, Result: row.Result, storageID: row.StorageID, fence: row.Fence, executedAt: row.ExecutedAt, result: row.Result}
	if err := json.Unmarshal(row.Criteria, &p.criteria); err != nil {
		return p, err
	}
	p.Match = p.criteria.Match
	p.Basis = p.criteria.Basis
	p.Before = p.criteria.Before
	return p, nil
}

// fenceMode is the source check of a preview: refresh and automatic cleanup
// act on a serving source, manual cleanup also on disabled or old sources.
func fenceMode(kind string, criteria PreviewCriteria) store.PreviewFence {
	if kind == "refresh" || criteria.Automatic {
		return store.ActivePreviewFence
	}
	return store.CleanupPreviewFence
}

func (s *Service) validatePreview(p MaintenancePreview, kind string) error {
	if kind != "cleanup" && kind != "refresh" || p.Kind != kind {
		return ErrInvalidPreview
	}
	if (p.State == "ready" || p.State == "building") && !s.now().Before(p.ExpiresAt) {
		return store.ErrExpired
	}
	if (p.State == "done" || p.State == "failed") && (p.executedAt == nil || !s.now().Before(p.executedAt.Add(24*time.Hour))) {
		return store.ErrExpired
	}
	return nil
}
func (s *Service) LookupPreview(storageID, kind, id string) (MaintenancePreview, error) {
	p, err := s.preview(id)
	if err == nil && p.storageID != storageID {
		err = sql.ErrNoRows
	}
	if err != nil {
		return MaintenancePreview{}, err
	}
	return p, s.validatePreview(p, kind)
}

func (s *Service) preview(id string) (MaintenancePreview, error) {
	row, err := s.db.HTTPPreview(id)
	if err != nil {
		return MaintenancePreview{}, err
	}
	return previewFromStore(row)
}

// LookupAppPreview finds a preview of any source epoch of the application with
// the stable uid. A preview of another application reads as sql.ErrNoRows.
func (s *Service) LookupAppPreview(uid, kind, id string) (MaintenancePreview, error) {
	p, err := s.preview(id)
	if err != nil {
		return p, err
	}
	if owner, _, ok := identity.ParseStorageID(p.storageID); !ok || owner != uid {
		return MaintenancePreview{}, sql.ErrNoRows
	}
	return p, s.validatePreview(p, kind)
}

// SourceEpoch is the source epoch the preview was built for.
func (p MaintenancePreview) SourceEpoch() int64 {
	_, epoch, _ := identity.ParseStorageID(p.storageID)
	return epoch
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
	if kind != "cleanup" && kind != "refresh" {
		return out, ErrInvalidPreview
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
	raw, _ := json.Marshal(criteria)
	id, err := fsutil.RandomID()
	if err != nil {
		return out, err
	}
	mode := fenceMode(kind, criteria)
	row, err := s.db.CreateHTTPPreview(ctx, store.HTTPPreview{ID: id, StorageID: entry.StorageID(), Kind: kind, Fence: fence(entry), CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute), Criteria: raw}, mode)
	if err != nil {
		return out, err
	}
	defer func() {
		if buildErr != nil {
			failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.db.FailHTTPPreviewBuild(failureCtx, id, s.now())
			_ = s.prunePreviews(failureCtx, 1)
		}
	}()
	after := options.after
	scanned, selected, active := 0, 0, 0
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
		page, pageActive, err := s.freezePreviewPage(ctx, row, mode, criteria, matcher, options, after, limit, options.selectLimit-selected)
		if err != nil {
			return out, err
		}
		scanned += page.Scanned
		selected += page.Selected
		active += pageActive
		if page.Scanned > 0 {
			after = page.Last
		}
		done = page.Scanned < limit || options.scanLimit > 0 && scanned >= options.scanLimit || options.selectLimit > 0 && selected >= options.selectLimit
	}
	if err = s.db.FinishHTTPPreviewBuild(ctx, row, mode); err != nil {
		return out, err
	}
	out, err = s.LookupPreview(entry.StorageID(), kind, id)
	out.ActiveFiles = active
	out.lastScanned = after
	return out, err
}

// freezePreviewPage selects one page under mu so that the active count sees
// a consistent set of reader pins.
func (s *Service) freezePreviewPage(ctx context.Context, row store.HTTPPreview, mode store.PreviewFence, criteria PreviewCriteria, matcher *pathmatch.Matcher, options buildOptions, after int64, limit, remaining int) (store.HTTPPreviewPage, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	active, selected := 0, 0
	var choiceErr error
	page, err := s.db.FreezeHTTPPreviewPage(ctx, row, mode, after, limit, func(e store.HTTPCacheEntry) (store.HTTPPreviewItem, bool, bool) {
		r, err := rowFromEntry(e)
		if err != nil {
			choiceErr = err
			return store.HTTPPreviewItem{}, false, true
		}
		if criteria.Path != "" && r.Path != criteria.Path || !matcher.Match("/"+r.Path) {
			return store.HTTPPreviewItem{}, false, false
		}
		basis, before, match, ruleIndex := criteria.Basis, criteria.Before, criteria.Match, -1
		if criteria.Automatic {
			rule, index, ok := options.policy.CleanupRule("/" + r.Path)
			if !ok {
				return store.HTTPPreviewItem{}, false, false
			}
			basis, before, match, ruleIndex = rule.Basis, s.now().UTC().Add(-time.Duration(rule.AgeSeconds)*time.Second), rule.Match, index
		}
		if row.Kind == "cleanup" {
			target := r.FetchedAt
			if basis == "last_access" && r.LastAccessAt != nil {
				target = *r.LastAccessAt
			}
			if !target.Before(before) {
				return store.HTTPPreviewItem{}, false, false
			}
		}
		rawMatch, _ := json.Marshal(match)
		selected++
		if s.pins[r.GenerationID] > 0 {
			active++
		}
		item := store.HTTPPreviewItem{GenerationID: r.GenerationID, Path: r.Path, SizeBytes: r.SizeBytes, AccessBucket: r.accessBucket, Basis: basis, Before: before, Match: rawMatch, RuleIndex: ruleIndex}
		return item, true, options.selectLimit > 0 && selected >= remaining
	})
	if err == nil {
		err = choiceErr
	}
	return page, active, err
}

func itemFromStore(row store.HTTPPreviewItem) (PreviewItem, error) {
	item := PreviewItem{Ordinal: row.Ordinal, GenerationID: row.GenerationID, Path: row.Path, SizeBytes: row.SizeBytes, AccessBucket: row.AccessBucket, Basis: row.Basis, Before: row.Before, RuleIndex: row.RuleIndex, ResultStatus: row.ResultStatus, ErrorCode: row.ErrorCode}
	return item, json.Unmarshal(row.Match, &item.Match)
}

func itemsFromStore(rows []store.HTTPPreviewItem) ([]PreviewItem, error) {
	out := make([]PreviewItem, 0, len(rows))
	for _, row := range rows {
		item, err := itemFromStore(row)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) PreviewItems(storageID, kind, id, cursor string, limit int) (PreviewPage, error) {
	var out PreviewPage
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 {
		return out, ErrInvalidPreview
	}
	after := int64(0)
	if cursor != "" {
		n, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil || n < 0 || strconv.FormatInt(n, 10) != cursor {
			return out, ErrInvalidPreview
		}
		after = n
	}
	p, err := s.LookupPreview(storageID, kind, id)
	if err != nil {
		return out, err
	}
	rows, err := s.db.HTTPPreviewItems(context.Background(), id, after, limit+1, false)
	if err != nil {
		return out, err
	}
	items, err := itemsFromStore(rows)
	if err != nil {
		return out, err
	}
	out = PreviewPage{Items: items, TotalFiles: p.SelectedFiles, TotalBytes: p.SelectedBytes, State: p.State}
	if len(items) > limit {
		out.Items = items[:limit]
		out.NextCursor = strconv.FormatInt(out.Items[limit-1].Ordinal, 10)
	}
	return out, nil
}

func (s *Service) claimPreview(ctx context.Context, entry application.Entry, kind, id string) (MaintenancePreview, error) {
	row, err := s.db.ClaimHTTPPreview(ctx, entry.StorageID(), id, func(row store.HTTPPreview) (store.HTTPPreviewDecision, error) {
		p, err := previewFromStore(row)
		if err != nil {
			return store.HTTPPreviewDecision{}, err
		}
		if err = s.validatePreview(p, kind); err != nil {
			return store.HTTPPreviewDecision{}, err
		}
		switch p.State {
		case "done", "failed":
			return store.HTTPPreviewDecision{}, nil
		case "running":
			return store.HTTPPreviewDecision{}, ErrPreviewRunning
		case "ready":
		default:
			return store.HTTPPreviewDecision{}, ErrInvalidPreview
		}
		if p.fence != fence(entry) {
			return store.HTTPPreviewDecision{}, store.ErrSourceInactive
		}
		return store.HTTPPreviewDecision{Apply: true, Fence: fenceMode(kind, p.criteria)}, nil
	})
	if err != nil {
		return MaintenancePreview{}, err
	}
	return previewFromStore(row)
}
func (s *Service) pendingPreviewItems(ctx context.Context, storageID, kind, id string, after int64, limit int) ([]PreviewItem, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidPreview
	}
	p, err := s.LookupPreview(storageID, kind, id)
	if err != nil {
		return nil, err
	}
	if p.State != "running" {
		return nil, ErrInvalidPreview
	}
	rows, err := s.db.HTTPPreviewItems(ctx, id, after, limit, true)
	if err != nil {
		return nil, err
	}
	return itemsFromStore(rows)
}

// runningPreview admits updates of a running preview of kind under entry's fence.
func (s *Service) runningPreview(entry application.Entry, kind string) func(store.HTTPPreview) (store.HTTPPreviewDecision, error) {
	return func(row store.HTTPPreview) (store.HTTPPreviewDecision, error) {
		p, err := previewFromStore(row)
		if err != nil {
			return store.HTTPPreviewDecision{}, err
		}
		if err = s.validatePreview(p, kind); err != nil {
			return store.HTTPPreviewDecision{}, err
		}
		if p.State != "running" || p.fence != fence(entry) {
			return store.HTTPPreviewDecision{}, ErrInvalidPreview
		}
		return store.HTTPPreviewDecision{Apply: true}, nil
	}
}

func (s *Service) recordPreviewItem(ctx context.Context, entry application.Entry, kind, id string, ordinal int64, status, code string) error {
	if status == "" || status == "pending" {
		return ErrInvalidPreview
	}
	return s.db.RecordHTTPPreviewItem(ctx, entry.StorageID(), id, ordinal, status, code, s.runningPreview(entry, kind))
}
func (s *Service) finishPreview(ctx context.Context, entry application.Entry, kind, id string, result any, failed bool) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	state := "done"
	if failed {
		state = "failed"
	}
	return s.db.FinishHTTPPreview(ctx, entry.StorageID(), id, state, raw, s.now(), func(row store.HTTPPreview) (store.HTTPPreviewDecision, error) {
		if row.Kind != kind || row.Fence != fence(entry) {
			return store.HTTPPreviewDecision{}, ErrInvalidPreview
		}
		switch row.State {
		case "done", "failed":
			return store.HTTPPreviewDecision{}, nil
		case "running":
			return store.HTTPPreviewDecision{Apply: true}, nil
		}
		return store.HTTPPreviewDecision{}, ErrInvalidPreview
	})
}
