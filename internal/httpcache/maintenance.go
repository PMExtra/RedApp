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
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
)

var ErrPreviewRunning = errors.New("Maintenance preview is already running")
var ErrInvalidPreview = errors.New("Invalid maintenance preview")
var ErrPreviewBusy = errors.New("Maintenance preview capacity reached")

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

const maintenanceColumns = `id,storage_id,kind,state,app_revision,vendor_revision,created_at_s,expires_at_s,selection_json,executed_at_s,result_json,scanned_count,selected_count,selected_bytes,completed_count,failed_count`

func scanMaintenance(row scanner) (MaintenancePreview, error) {
	var p MaintenancePreview
	var created, expires int64
	var executed sql.NullInt64
	var criteria, result []byte
	err := row.Scan(&p.ID, &p.storageID, &p.Kind, &p.State, &p.fence.AppRevision, &p.fence.VendorRevision, &created, &expires, &criteria, &executed, &result, &p.ScannedFiles, &p.SelectedFiles, &p.SelectedBytes, &p.CompletedFiles, &p.FailedFiles)
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(criteria, &p.criteria); err != nil {
		return p, err
	}
	p.Match = p.criteria.Match
	p.Basis = p.criteria.Basis
	p.Before = p.criteria.Before
	p.CreatedAt = time.Unix(created, 0).UTC()
	p.ExpiresAt = time.Unix(expires, 0).UTC()
	p.result = result
	p.Result = result
	if executed.Valid {
		at := time.Unix(executed.Int64, 0).UTC()
		p.executedAt = &at
	}
	return p, nil
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
	p, err := scanMaintenance(s.db.DB.QueryRow(`SELECT `+maintenanceColumns+` FROM http_cleanup_previews WHERE id=? AND storage_id=?`, id, storageID))
	if err != nil {
		return p, err
	}
	return p, s.validatePreview(p, kind)
}

func (s *Service) BuildPreview(ctx context.Context, entry application.Entry, kind string, criteria PreviewCriteria) (MaintenancePreview, error) {
	return s.buildPreview(ctx, entry, kind, criteria, buildOptions{})
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
		return out, fmt.Errorf("%w: %v", ErrInvalidCleanup, err)
	}
	if criteria.Path != "" && pathmatch.ValidatePath("/"+criteria.Path) != nil {
		return out, ErrInvalidPreview
	}
	if kind == "cleanup" && !criteria.Automatic && ((criteria.Basis != "fetched_at" && criteria.Basis != "last_access") || criteria.Before.IsZero() || criteria.Before.After(s.now())) {
		return out, ErrInvalidCleanup
	}
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if kind == "refresh" || criteria.Automatic {
		err = s.db.RequireSourceActive(tx, entry.StorageID(), fence(entry))
	} else {
		err = s.db.RequireCleanupFence(tx, entry.StorageID(), fence(entry))
	}
	if err != nil {
		return out, err
	}
	var highWater int64
	if err = tx.QueryRow(`SELECT COALESCE(MAX(row_no),0) FROM http_cache_generations WHERE storage_id=? AND is_current=1`, entry.StorageID()).Scan(&highWater); err != nil {
		return out, err
	}
	now := s.now().UTC()
	raw, _ := json.Marshal(criteria)
	id, err := fsutil.RandomID()
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(`INSERT INTO http_cleanup_previews(id,storage_id,kind,state,app_revision,vendor_revision,created_at_s,expires_at_s,selection_json,high_water) VALUES(?,?,?,'building',?,?,?,?,?,?)`, id, entry.StorageID(), kind, entry.Revision, entry.VendorRevision, now.Unix(), now.Add(10*time.Minute).Unix(), raw, highWater)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	defer func() {
		if buildErr != nil {
			failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.DB.ExecContext(failureCtx, `UPDATE http_cleanup_previews SET state='failed',executed_at_s=?,result_json=? WHERE id=? AND state='building'`, s.now().Unix(), []byte(`{"error":"preview_build_failed"}`), id)
			_ = s.prunePreviews(failureCtx, 1)
		}
	}()
	after := options.after
	scanned, selected, active := 0, 0, 0
	done := false
	for !done {
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
		page, err := s.freezePreviewPage(ctx, entry, id, kind, criteria, matcher, options, after, highWater, limit, options.selectLimit-selected)
		if err != nil {
			return out, err
		}
		scanned += page.scanned
		selected += page.selected
		active += page.active
		if page.scanned > 0 {
			after = page.after
		}
		done = page.scanned < limit || options.scanLimit > 0 && scanned >= options.scanLimit || options.selectLimit > 0 && selected >= options.selectLimit
	}
	tx, err = s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if kind == "refresh" || criteria.Automatic {
		err = s.db.RequireSourceActive(tx, entry.StorageID(), fence(entry))
	} else {
		err = s.db.RequireCleanupFence(tx, entry.StorageID(), fence(entry))
	}
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(`UPDATE http_cleanup_previews SET state='ready' WHERE id=? AND state='building'`, id); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	out, err = s.LookupPreview(entry.StorageID(), kind, id)
	out.ActiveFiles = active
	out.lastScanned = after
	return out, err
}

type frozenPage struct {
	scanned, selected, active int
	after                     int64
}

func (s *Service) freezePreviewPage(ctx context.Context, entry application.Entry, id, kind string, criteria PreviewCriteria, matcher *pathmatch.Matcher, options buildOptions, after, highWater int64, limit, remaining int) (page frozenPage, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if kind == "refresh" || criteria.Automatic {
		err = s.db.RequireSourceActive(tx, entry.StorageID(), fence(entry))
	} else {
		err = s.db.RequireCleanupFence(tx, entry.StorageID(), fence(entry))
	}
	if err != nil {
		return page, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT row_no,`+columns+` FROM http_cache_generations WHERE storage_id=? AND is_current=1 AND row_no>? AND row_no<=? ORDER BY row_no LIMIT ?`, entry.StorageID(), after, highWater, limit)
	if err != nil {
		return page, err
	}
	selected := []PreviewItem{}
	for rows.Next() {
		var ordinal int64
		r, scanErr := scan(prependScanner{row: rows, first: &ordinal})
		if scanErr != nil {
			rows.Close()
			return page, scanErr
		}
		page.scanned++
		page.after = ordinal
		if criteria.Path != "" && r.Path != criteria.Path || !matcher.Match("/"+r.Path) {
			continue
		}
		item := PreviewItem{Ordinal: ordinal, GenerationID: r.GenerationID, Path: r.Path, SizeBytes: r.SizeBytes, AccessBucket: r.accessBucket, Basis: criteria.Basis, Before: criteria.Before, Match: criteria.Match, RuleIndex: -1, ResultStatus: "pending"}
		if criteria.Automatic {
			rule, index, ok := options.policy.CleanupRule("/" + r.Path)
			if !ok {
				continue
			}
			item.Basis = rule.Basis
			item.Before = s.now().UTC().Add(-time.Duration(rule.AgeSeconds) * time.Second)
			item.Match = rule.Match
			item.RuleIndex = index
		}
		if kind == "cleanup" {
			target := r.FetchedAt
			if item.Basis == "last_access" && r.LastAccessAt != nil {
				target = *r.LastAccessAt
			}
			if !target.Before(item.Before) {
				continue
			}
		}
		selected = append(selected, item)
		page.selected++
		if s.pins[r.GenerationID] > 0 {
			page.active++
		}
		if options.selectLimit > 0 && page.selected >= remaining {
			break
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	var bytes int64
	for _, item := range selected {
		raw, _ := json.Marshal(item.Match)
		before := int64(0)
		if !item.Before.IsZero() {
			before = item.Before.Unix()
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO http_cleanup_preview_items(preview_id,ordinal,generation_id,path,size_bytes,access_bucket_s,basis,before_s,match_json,rule_index) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, item.Ordinal, item.GenerationID, item.Path, item.SizeBytes, item.AccessBucket, item.Basis, before, raw, item.RuleIndex)
		if err != nil {
			return page, err
		}
		bytes += item.SizeBytes
	}
	if _, err = tx.ExecContext(ctx, `UPDATE http_cleanup_previews SET scanned_count=scanned_count+?,selected_count=selected_count+?,selected_bytes=selected_bytes+? WHERE id=? AND state='building'`, page.scanned, page.selected, bytes, id); err != nil {
		return page, err
	}
	return page, tx.Commit()
}

type prependScanner struct {
	row   scanner
	first *int64
}

func (r prependScanner) Scan(dest ...any) error {
	return r.row.Scan(append([]any{r.first}, dest...)...)
}

const itemColumns = `ordinal,generation_id,path,size_bytes,access_bucket_s,basis,before_s,match_json,rule_index,result_status,error_code`

func scanPreviewItem(row scanner) (PreviewItem, error) {
	var item PreviewItem
	var before int64
	var match []byte
	err := row.Scan(&item.Ordinal, &item.GenerationID, &item.Path, &item.SizeBytes, &item.AccessBucket, &item.Basis, &before, &match, &item.RuleIndex, &item.ResultStatus, &item.ErrorCode)
	if err != nil {
		return item, err
	}
	if before != 0 {
		item.Before = time.Unix(before, 0).UTC()
	}
	err = json.Unmarshal(match, &item.Match)
	return item, err
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
	rows, err := s.db.DB.Query(`SELECT `+itemColumns+` FROM http_cleanup_preview_items WHERE preview_id=? AND ordinal>? ORDER BY ordinal LIMIT ?`, id, after, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out = PreviewPage{Items: []PreviewItem{}, TotalFiles: p.SelectedFiles, TotalBytes: p.SelectedBytes, State: p.State}
	for rows.Next() {
		item, err := scanPreviewItem(rows)
		if err != nil {
			return out, err
		}
		if len(out.Items) == limit {
			out.NextCursor = strconv.FormatInt(out.Items[len(out.Items)-1].Ordinal, 10)
			break
		}
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}

func (s *Service) claimPreview(ctx context.Context, entry application.Entry, kind, id string) (MaintenancePreview, error) {
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return MaintenancePreview{}, err
	}
	defer tx.Rollback()
	p, err := scanMaintenance(tx.QueryRow(`SELECT `+maintenanceColumns+` FROM http_cleanup_previews WHERE id=? AND storage_id=?`, id, entry.StorageID()))
	if err != nil {
		return p, err
	}
	if err = s.validatePreview(p, kind); err != nil {
		return p, err
	}
	if p.State == "done" || p.State == "failed" {
		return p, nil
	}
	if p.State == "running" {
		return p, ErrPreviewRunning
	}
	if p.State != "ready" {
		return p, ErrInvalidPreview
	}
	if p.fence != fence(entry) {
		return p, store.ErrSourceInactive
	}
	if kind == "refresh" || p.criteria.Automatic {
		err = s.db.RequireSourceActive(tx, entry.StorageID(), p.fence)
	} else {
		err = s.db.RequireCleanupFence(tx, entry.StorageID(), p.fence)
	}
	if err != nil {
		return p, err
	}
	if _, err = tx.Exec(`UPDATE http_cleanup_previews SET state='running' WHERE id=? AND state='ready'`, id); err != nil {
		return p, err
	}
	p.State = "running"
	return p, tx.Commit()
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
	rows, err := s.db.DB.QueryContext(ctx, `SELECT `+itemColumns+` FROM http_cleanup_preview_items WHERE preview_id=? AND ordinal>? AND result_status='pending' ORDER BY ordinal LIMIT ?`, id, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PreviewItem{}
	for rows.Next() {
		item, err := scanPreviewItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func recordPreviewItemTx(tx *sql.Tx, id string, ordinal int64, status, code string) error {
	result, err := tx.Exec(`UPDATE http_cleanup_preview_items SET result_status=?,error_code=? WHERE preview_id=? AND ordinal=? AND result_status='pending'`, status, code, id, ordinal)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return err
	}
	failed := 0
	if status == "failed" {
		failed = 1
	}
	_, err = tx.Exec(`UPDATE http_cleanup_previews SET completed_count=completed_count+1,failed_count=failed_count+? WHERE id=?`, failed, id)
	return err
}
func (s *Service) recordPreviewItem(ctx context.Context, entry application.Entry, kind, id string, ordinal int64, status, code string) error {
	if status == "" || status == "pending" {
		return ErrInvalidPreview
	}
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := scanMaintenance(tx.QueryRow(`SELECT `+maintenanceColumns+` FROM http_cleanup_previews WHERE id=? AND storage_id=?`, id, entry.StorageID()))
	if err != nil {
		return err
	}
	if err = s.validatePreview(p, kind); err != nil {
		return err
	}
	if p.State != "running" || p.fence != fence(entry) {
		return ErrInvalidPreview
	}
	if err = recordPreviewItemTx(tx, id, ordinal, status, code); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) finishPreview(ctx context.Context, entry application.Entry, kind, id string, result any, failed bool) error {
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := scanMaintenance(tx.QueryRow(`SELECT `+maintenanceColumns+` FROM http_cleanup_previews WHERE id=? AND storage_id=?`, id, entry.StorageID()))
	if err != nil {
		return err
	}
	if p.Kind != kind || p.fence != fence(entry) {
		return ErrInvalidPreview
	}
	if p.State == "done" || p.State == "failed" {
		return nil
	}
	if p.State != "running" {
		return ErrInvalidPreview
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	state := "done"
	if failed {
		state = "failed"
	}
	if _, err = tx.Exec(`UPDATE http_cleanup_previews SET state=?,executed_at_s=?,result_json=? WHERE id=?`, state, s.now().Unix(), raw, id); err != nil {
		return err
	}
	return tx.Commit()
}
