package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// HTTPCacheEntry is one stored body of the HTTP cache: a complete upstream
// representation of one application path within one source epoch. At most one
// entry per path is current; retired entries wait until no reader pins them.
type HTTPCacheEntry struct {
	// RowNo orders entries by insertion; previews scan by it.
	RowNo     int64
	ID        string
	StorageID string
	Path      string
	SourceURL string
	SHA256    string
	SizeBytes int64
	// Headers is the JSON object of the stored representation headers.
	Headers     []byte
	FetchedAt   time.Time
	ValidatedAt time.Time
	FreshUntil  time.Time
	// AccessBucket is the start of the last minute a client used the entry, or 0.
	AccessBucket int64
	Current      bool
}

const httpEntryColumns = `row_no,id,storage_id,path,source_url,sha256,size_bytes,headers_json,fetched_at_s,validated_at_s,fresh_until_s,last_access_bucket_s,is_current`

// Preview pages scan the current entries of one source by row through the
// http_cache_scan index; see TestHTTPMaintenancePagesUseSourceRowRangeIndex.
const (
	httpHighWaterQuery = `SELECT COALESCE(MAX(row_no),0) FROM http_cache_generations WHERE storage_id=? AND is_current=1`
	httpPageCondition  = `WHERE storage_id=? AND is_current=1 AND row_no>? AND row_no<=? ORDER BY row_no LIMIT ?`
)

func scanHTTPCacheEntry(row scanner) (HTTPCacheEntry, error) {
	var e HTTPCacheEntry
	var fetched, validated, fresh int64
	err := row.Scan(&e.RowNo, &e.ID, &e.StorageID, &e.Path, &e.SourceURL, &e.SHA256, &e.SizeBytes, &e.Headers, &fetched, &validated, &fresh, &e.AccessBucket, &e.Current)
	e.FetchedAt = time.Unix(fetched, 0).UTC()
	e.ValidatedAt = time.Unix(validated, 0).UTC()
	e.FreshUntil = time.Unix(fresh, 0).UTC()
	return e, err
}

func queryHTTPCacheEntries(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, query string, args ...any) ([]HTTPCacheEntry, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+httpEntryColumns+` FROM http_cache_generations `+query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HTTPCacheEntry{}
	for rows.Next() {
		e, err := scanHTTPCacheEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// HTTPCacheEntries lists the current entries of a source epoch in path order.
func (s *Store) HTTPCacheEntries(storageID string) ([]HTTPCacheEntry, error) {
	return queryHTTPCacheEntries(context.Background(), s.DB, `WHERE storage_id=? AND is_current=1 ORDER BY path`, storageID)
}

// HTTPCacheEntriesAfter lists up to limit current entries in path order whose
// path sorts after path, or at or after it when inclusive.
func (s *Store) HTTPCacheEntriesAfter(storageID, path string, inclusive bool, limit int) ([]HTTPCacheEntry, error) {
	condition := `path>?`
	if inclusive {
		condition = `path>=?`
	}
	return queryHTTPCacheEntries(context.Background(), s.DB, `WHERE storage_id=? AND is_current=1 AND `+condition+` ORDER BY path LIMIT ?`, storageID, path, limit)
}

// AllHTTPCacheEntries lists every entry, current or retired, of every application.
func (s *Store) AllHTTPCacheEntries() ([]HTTPCacheEntry, error) {
	return queryHTTPCacheEntries(context.Background(), s.DB, `ORDER BY row_no`)
}

// CurrentHTTPCacheEntry returns the current entry of a path, or sql.ErrNoRows.
func (s *Store) CurrentHTTPCacheEntry(storageID, path string) (HTTPCacheEntry, error) {
	return scanHTTPCacheEntry(s.DB.QueryRow(`SELECT `+httpEntryColumns+` FROM http_cache_generations WHERE storage_id=? AND path=? AND is_current=1`, storageID, path))
}

// HTTPCacheEntry returns an entry by ID, or sql.ErrNoRows.
func (s *Store) HTTPCacheEntry(id string) (HTTPCacheEntry, error) {
	return scanHTTPCacheEntry(s.DB.QueryRow(`SELECT `+httpEntryColumns+` FROM http_cache_generations WHERE id=?`, id))
}

// RetireHTTPCacheEntry makes an entry non-current. Retiring twice keeps the
// first retirement time.
func (s *Store) RetireHTTPCacheEntry(id string, at time.Time) error {
	_, err := s.DB.Exec(`UPDATE http_cache_generations SET is_current=0,retired_at_s=COALESCE(retired_at_s,?) WHERE id=?`, at.Unix(), id)
	return err
}

// DeleteRetiredHTTPCacheEntry removes a retired entry's row after its body was
// deleted. It reports whether a row was removed; current entries are kept.
func (s *Store) DeleteRetiredHTTPCacheEntry(id string) (bool, error) {
	result, err := s.DB.Exec(`DELETE FROM http_cache_generations WHERE id=? AND is_current=0`, id)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// TouchHTTPCacheEntry advances an entry's access bucket; buckets never move
// back. It returns sql.ErrNoRows when the entry no longer exists.
func (s *Store) TouchHTTPCacheEntry(id string, bucket int64) error {
	result, err := s.DB.Exec(`UPDATE http_cache_generations SET last_access_bucket_s=MAX(last_access_bucket_s,?) WHERE id=?`, bucket, id)
	return affected(result, err)
}

// PublishHTTPCacheEntry makes e the current entry of its path while the source
// fence is still active. With replaces set, the path's current entry must
// still be that entry (ErrConflict otherwise); it is retired at the same time.
// The entry's body file must already exist under its final name.
func (s *Store) PublishHTTPCacheEntry(e HTTPCacheEntry, fence SourceFence, replaces string, at time.Time) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.RequireSourceActive(tx, e.StorageID, fence); err != nil {
		return err
	}
	if replaces != "" {
		var current string
		err = tx.QueryRow(`SELECT id FROM http_cache_generations WHERE storage_id=? AND path=? AND is_current=1`, e.StorageID, e.Path).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) || err == nil && current != replaces {
			return ErrConflict
		}
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`UPDATE http_cache_generations SET is_current=0,retired_at_s=COALESCE(retired_at_s,?) WHERE storage_id=? AND path=? AND is_current=1`, at.Unix(), e.StorageID, e.Path); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO http_cache_generations(id,storage_id,path,source_url,sha256,size_bytes,headers_json,fetched_at_s,validated_at_s,fresh_until_s,last_access_bucket_s,is_current) VALUES(?,?,?,?,?,?,?,?,?,?,?,1)`,
		e.ID, e.StorageID, e.Path, e.SourceURL, e.SHA256, e.SizeBytes, e.Headers, e.FetchedAt.Unix(), e.ValidatedAt.Unix(), e.FreshUntil.Unix(), e.AccessBucket)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RevalidateHTTPCacheEntry records a successful validation of the current
// entry id while the source fence is active. It returns ErrConflict when the
// entry is no longer current.
func (s *Store) RevalidateHTTPCacheEntry(id, storageID string, fence SourceFence, validated, fresh time.Time, headers []byte) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.RequireSourceActive(tx, storageID, fence); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE http_cache_generations SET validated_at_s=?,fresh_until_s=?,headers_json=? WHERE id=? AND storage_id=? AND is_current=1`, validated.Unix(), fresh.Unix(), headers, id, storageID)
	if err = affected(result, err); errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	} else if err != nil {
		return err
	}
	return tx.Commit()
}

// HTTPCacheEntryCurrent reports, in one snapshot with the source fence check,
// whether entry id is still current.
func (s *Store) HTTPCacheEntryCurrent(storageID string, fence SourceFence, id string) (bool, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err = s.RequireSourceActive(tx, storageID, fence); err != nil {
		return false, err
	}
	var current bool
	if err = tx.QueryRow(`SELECT is_current FROM http_cache_generations WHERE id=? AND storage_id=?`, id, storageID).Scan(&current); err != nil {
		return false, err
	}
	return current, tx.Commit()
}

// HTTPPreview is a frozen refresh or cleanup selection of HTTP cache entries.
// Criteria and Result are owned by the HTTP cache and stored as JSON.
type HTTPPreview struct {
	ID        string
	StorageID string
	Kind      string // cleanup or refresh
	State     string // building, ready, running, done or failed
	// Fence is the source fence the selection was frozen under.
	Fence          SourceFence
	CreatedAt      time.Time
	ExpiresAt      time.Time
	Criteria       []byte
	ExecutedAt     *time.Time
	Result         []byte
	ScannedFiles   int
	SelectedFiles  int
	SelectedBytes  int64
	CompletedFiles int
	FailedFiles    int
	// HighWater is the last entry row the selection may contain.
	HighWater int64
}

// HTTPPreviewItem is one frozen entry of a preview and its execution outcome.
type HTTPPreviewItem struct {
	Ordinal      int64
	GenerationID string
	Path         string
	SizeBytes    int64
	AccessBucket int64
	Basis        string
	Before       time.Time
	// Match is the JSON path pattern that selected the entry.
	Match        []byte
	RuleIndex    int
	ResultStatus string
	ErrorCode    string
}

const httpPreviewColumns = `id,storage_id,kind,state,app_revision,vendor_revision,created_at_s,expires_at_s,selection_json,executed_at_s,result_json,scanned_count,selected_count,selected_bytes,completed_count,failed_count,high_water`

func scanHTTPPreview(row scanner) (HTTPPreview, error) {
	var p HTTPPreview
	var created, expires int64
	var executed sql.NullInt64
	err := row.Scan(&p.ID, &p.StorageID, &p.Kind, &p.State, &p.Fence.AppRuntimeRevision, &p.Fence.VendorRuntimeRevision, &created, &expires, &p.Criteria, &executed, &p.Result, &p.ScannedFiles, &p.SelectedFiles, &p.SelectedBytes, &p.CompletedFiles, &p.FailedFiles, &p.HighWater)
	p.CreatedAt = time.Unix(created, 0).UTC()
	p.ExpiresAt = time.Unix(expires, 0).UTC()
	p.ExecutedAt = timePointer(executed)
	return p, err
}

const httpPreviewItemColumns = `ordinal,generation_id,path,size_bytes,access_bucket_s,basis,before_s,match_json,rule_index,result_status,error_code`

func scanHTTPPreviewItem(row scanner) (HTTPPreviewItem, error) {
	var item HTTPPreviewItem
	var before int64
	err := row.Scan(&item.Ordinal, &item.GenerationID, &item.Path, &item.SizeBytes, &item.AccessBucket, &item.Basis, &before, &item.Match, &item.RuleIndex, &item.ResultStatus, &item.ErrorCode)
	if before != 0 {
		item.Before = time.Unix(before, 0).UTC()
	}
	return item, err
}

// PreviewFence is the source check an HTTP cache preview operation makes in
// its own transaction.
type PreviewFence uint8

const (
	// NoPreviewFence checks nothing beyond the preview row itself.
	NoPreviewFence PreviewFence = iota
	// CleanupPreviewFence requires the frozen fence but not an active source,
	// so manual cleanup may select disabled and historical sources.
	CleanupPreviewFence
	// ActivePreviewFence requires the frozen fence of a source that still
	// serves, as refresh and automatic cleanup do.
	ActivePreviewFence
)

func (s *Store) requireHTTPPreviewFence(tx *sql.Tx, storageID string, fence SourceFence, mode PreviewFence) error {
	switch mode {
	case ActivePreviewFence:
		return s.RequireSourceActive(tx, storageID, fence)
	case CleanupPreviewFence:
		return s.RequireCleanupFence(tx, storageID, fence)
	}
	return nil
}

// CreateHTTPPreview starts building p under its fence. The selection may only
// contain entries current now; the returned preview carries that high water.
func (s *Store) CreateHTTPPreview(ctx context.Context, p HTTPPreview, mode PreviewFence) (HTTPPreview, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if err = s.requireHTTPPreviewFence(tx, p.StorageID, p.Fence, mode); err != nil {
		return p, err
	}
	if err = tx.QueryRowContext(ctx, httpHighWaterQuery, p.StorageID).Scan(&p.HighWater); err != nil {
		return p, err
	}
	p.State = "building"
	_, err = tx.ExecContext(ctx, `INSERT INTO http_cleanup_previews(id,storage_id,kind,state,app_revision,vendor_revision,created_at_s,expires_at_s,selection_json,high_water) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.StorageID, p.Kind, p.State, p.Fence.AppRuntimeRevision, p.Fence.VendorRuntimeRevision, p.CreatedAt.Unix(), p.ExpiresAt.Unix(), p.Criteria, p.HighWater)
	if err != nil {
		return p, err
	}
	return p, tx.Commit()
}

// HTTPPreviewPage reports one frozen page: how many current entries were
// scanned and selected, and the last scanned row.
type HTTPPreviewPage struct {
	Scanned, Selected int
	Last              int64
}

// FreezeHTTPPreviewPage scans up to limit current entries after row `after`
// and not beyond the preview's high water. choose decides each entry and
// returns stop to end the page after it. The selection and the preview's
// counters are stored in the same transaction.
func (s *Store) FreezeHTTPPreviewPage(ctx context.Context, p HTTPPreview, mode PreviewFence, after int64, limit int, choose func(HTTPCacheEntry) (item HTTPPreviewItem, selected, stop bool)) (HTTPPreviewPage, error) {
	var page HTTPPreviewPage
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err = s.requireHTTPPreviewFence(tx, p.StorageID, p.Fence, mode); err != nil {
		return page, err
	}
	entries, err := queryHTTPCacheEntries(ctx, tx, httpPageCondition, p.StorageID, after, p.HighWater, limit)
	if err != nil {
		return page, err
	}
	var bytes int64
	for _, e := range entries {
		page.Scanned++
		page.Last = e.RowNo
		item, selected, stop := choose(e)
		if selected {
			item.Ordinal = e.RowNo
			before := int64(0)
			if !item.Before.IsZero() {
				before = item.Before.Unix()
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO http_cleanup_preview_items(preview_id,ordinal,generation_id,path,size_bytes,access_bucket_s,basis,before_s,match_json,rule_index) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				p.ID, item.Ordinal, item.GenerationID, item.Path, item.SizeBytes, item.AccessBucket, item.Basis, before, item.Match, item.RuleIndex)
			if err != nil {
				return page, err
			}
			page.Selected++
			bytes += item.SizeBytes
		}
		if stop {
			break
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE http_cleanup_previews SET scanned_count=scanned_count+?,selected_count=selected_count+?,selected_bytes=selected_bytes+? WHERE id=? AND state='building'`, page.Scanned, page.Selected, bytes, p.ID); err != nil {
		return page, err
	}
	return page, tx.Commit()
}

// FinishHTTPPreviewBuild makes a built preview ready while its fence holds.
func (s *Store) FinishHTTPPreviewBuild(ctx context.Context, p HTTPPreview, mode PreviewFence) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.requireHTTPPreviewFence(tx, p.StorageID, p.Fence, mode); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE http_cleanup_previews SET state='ready' WHERE id=? AND state='building'`, p.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// FailHTTPPreviewBuild records a build that did not finish. Its partial
// selection is discarded later by PruneHTTPPreviews.
func (s *Store) FailHTTPPreviewBuild(ctx context.Context, id string, at time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE http_cleanup_previews SET state='failed',executed_at_s=?,result_json=? WHERE id=? AND state='building'`, at.Unix(), []byte(`{"error":"preview_build_failed"}`), id)
	return err
}

// DeleteEmptyHTTPPreview removes a ready preview that selected nothing.
func (s *Store) DeleteEmptyHTTPPreview(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM http_cleanup_previews WHERE id=? AND selected_count=0 AND state='ready'`, id)
	return err
}

// HTTPPreview returns a preview by ID, or sql.ErrNoRows.
func (s *Store) HTTPPreview(id string) (HTTPPreview, error) {
	return scanHTTPPreview(s.DB.QueryRow(`SELECT `+httpPreviewColumns+` FROM http_cleanup_previews WHERE id=?`, id))
}

// HTTPPreviewItems lists up to limit frozen items after ordinal `after`, only
// those without an outcome when pendingOnly is set.
func (s *Store) HTTPPreviewItems(ctx context.Context, id string, after int64, limit int, pendingOnly bool) ([]HTTPPreviewItem, error) {
	query := `SELECT ` + httpPreviewItemColumns + ` FROM http_cleanup_preview_items WHERE preview_id=? AND ordinal>? ORDER BY ordinal LIMIT ?`
	if pendingOnly {
		query = `SELECT ` + httpPreviewItemColumns + ` FROM http_cleanup_preview_items WHERE preview_id=? AND ordinal>? AND result_status='pending' ORDER BY ordinal LIMIT ?`
	}
	rows, err := s.DB.QueryContext(ctx, query, id, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HTTPPreviewItem{}
	for rows.Next() {
		item, err := scanHTTPPreviewItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// HTTPPreviewDecision is how a caller treats a preview read inside a transaction.
type HTTPPreviewDecision struct {
	// Apply performs the update; otherwise the preview is returned unchanged.
	Apply bool
	// Fence is checked against the preview's frozen fence before the update.
	Fence PreviewFence
}

func (s *Store) updateHTTPPreview(ctx context.Context, storageID, id string, decide func(HTTPPreview) (HTTPPreviewDecision, error), update func(*sql.Tx, *HTTPPreview) error) (HTTPPreview, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return HTTPPreview{}, err
	}
	defer tx.Rollback()
	p, err := scanHTTPPreview(tx.QueryRowContext(ctx, `SELECT `+httpPreviewColumns+` FROM http_cleanup_previews WHERE id=? AND storage_id=?`, id, storageID))
	if err != nil {
		return p, err
	}
	decision, err := decide(p)
	if err != nil || !decision.Apply {
		return p, err
	}
	if err = s.requireHTTPPreviewFence(tx, storageID, p.Fence, decision.Fence); err != nil {
		return p, err
	}
	if err = update(tx, &p); err != nil {
		return p, err
	}
	return p, tx.Commit()
}

// ClaimHTTPPreview moves a ready preview to running when decide applies it.
func (s *Store) ClaimHTTPPreview(ctx context.Context, storageID, id string, decide func(HTTPPreview) (HTTPPreviewDecision, error)) (HTTPPreview, error) {
	return s.updateHTTPPreview(ctx, storageID, id, decide, func(tx *sql.Tx, p *HTTPPreview) error {
		result, err := tx.ExecContext(ctx, `UPDATE http_cleanup_previews SET state='running' WHERE id=? AND state='ready'`, p.ID)
		if err = affected(result, err); errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		p.State = "running"
		return err
	})
}

func recordHTTPPreviewItem(ctx context.Context, tx *sql.Tx, id string, ordinal int64, status, code string) error {
	result, err := tx.ExecContext(ctx, `UPDATE http_cleanup_preview_items SET result_status=?,error_code=? WHERE preview_id=? AND ordinal=? AND result_status='pending'`, status, code, id, ordinal)
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
	_, err = tx.ExecContext(ctx, `UPDATE http_cleanup_previews SET completed_count=completed_count+1,failed_count=failed_count+? WHERE id=?`, failed, id)
	return err
}

// RecordHTTPPreviewItem stores the outcome of one pending item when decide
// applies it. An item records only its first outcome.
func (s *Store) RecordHTTPPreviewItem(ctx context.Context, storageID, id string, ordinal int64, status, code string, decide func(HTTPPreview) (HTTPPreviewDecision, error)) error {
	_, err := s.updateHTTPPreview(ctx, storageID, id, decide, func(tx *sql.Tx, p *HTTPPreview) error {
		return recordHTTPPreviewItem(ctx, tx, p.ID, ordinal, status, code)
	})
	return err
}

// FinishHTTPPreview stores the final state and receipt of an execution when
// decide applies it.
func (s *Store) FinishHTTPPreview(ctx context.Context, storageID, id, state string, result []byte, at time.Time, decide func(HTTPPreview) (HTTPPreviewDecision, error)) error {
	_, err := s.updateHTTPPreview(ctx, storageID, id, decide, func(tx *sql.Tx, p *HTTPPreview) error {
		_, err := tx.ExecContext(ctx, `UPDATE http_cleanup_previews SET state=?,executed_at_s=?,result_json=? WHERE id=?`, state, at.Unix(), result, p.ID)
		return err
	})
	return err
}

// HTTPRetirement is the outcome of one cleanup batch.
type HTTPRetirement struct {
	Retired         []string // retired entry IDs, whose bodies may now be collected
	RetiredBytes    int64
	SkippedAccessed int
	SkippedChanged  int
}

// RetireHTTPPreviewItems executes one batch of a running cleanup preview in a
// single transaction under its fence. An item is retired only if its entry is
// still the current entry of the same path and, for the last_access basis,
// was not accessed since the preview; every item records its outcome.
func (s *Store) RetireHTTPPreviewItems(ctx context.Context, p HTTPPreview, mode PreviewFence, items []HTTPPreviewItem, at time.Time) (HTTPRetirement, error) {
	var out HTTPRetirement
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = s.requireHTTPPreviewFence(tx, p.StorageID, p.Fence, mode); err != nil {
		return out, err
	}
	for _, item := range items {
		if err = ctx.Err(); err != nil {
			return HTTPRetirement{}, err
		}
		e, err := scanHTTPCacheEntry(tx.QueryRowContext(ctx, `SELECT `+httpEntryColumns+` FROM http_cache_generations WHERE id=? AND storage_id=?`, item.GenerationID, p.StorageID))
		status := "retired"
		switch {
		case errors.Is(err, sql.ErrNoRows):
			status = "skipped_changed"
		case err != nil:
			return HTTPRetirement{}, err
		case !e.Current || e.Path != item.Path:
			status = "skipped_changed"
		case item.Basis == "last_access" && e.AccessBucket != item.AccessBucket:
			status = "skipped_accessed"
		}
		switch status {
		case "skipped_changed":
			out.SkippedChanged++
		case "skipped_accessed":
			out.SkippedAccessed++
		default:
			if _, err = tx.ExecContext(ctx, `UPDATE http_cache_generations SET is_current=0,retired_at_s=? WHERE id=? AND is_current=1`, at.Unix(), e.ID); err != nil {
				return HTTPRetirement{}, err
			}
			out.Retired = append(out.Retired, e.ID)
			out.RetiredBytes += e.SizeBytes
		}
		if err = recordHTTPPreviewItem(ctx, tx, p.ID, item.Ordinal, status, ""); err != nil {
			return HTTPRetirement{}, err
		}
	}
	return out, tx.Commit()
}

// InterruptHTTPPreviews fails every preview a previous process left building
// or running. An interrupted execution is a receipt, never a restartable
// queue; bounded batches keep this independent of the number of items.
func (s *Store) InterruptHTTPPreviews(ctx context.Context, at time.Time) error {
	for {
		result, err := s.DB.ExecContext(ctx, `UPDATE http_cleanup_previews SET
			result_json=json_object('error',CASE WHEN state='building' THEN 'preview_build_failed' ELSE 'interrupted_by_restart' END,
			'selected_files',selected_count,'completed_files',completed_count,'failed',failed_count),
			executed_at_s=?,state='failed'
			WHERE id IN (SELECT id FROM http_cleanup_previews WHERE state IN ('building','running') LIMIT 100)`, at.Unix())
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n < 100 {
			return err
		}
	}
}

// PruneHTTPPreviews deletes expired previews and receipts older than 24 hours,
// and the selections of failed builds. Each of at most batches transactions
// removes at most 1000 items and then the header once it has none, so no
// unbounded cascade runs.
func (s *Store) PruneHTTPPreviews(ctx context.Context, now time.Time, batches int) error {
	for i := 0; i < batches; i++ {
		done, err := s.pruneHTTPPreviewBatch(ctx, now)
		if err != nil || done {
			return err
		}
	}
	return nil
}

func (s *Store) pruneHTTPPreviewBatch(ctx context.Context, now time.Time) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id string
	var expired bool
	err = tx.QueryRowContext(ctx, `SELECT id,
		(state='ready' AND expires_at_s<=?) OR (state IN ('done','failed') AND executed_at_s<=?)
		FROM http_cleanup_previews p WHERE
		(state='ready' AND expires_at_s<=?) OR
		(state IN ('done','failed') AND executed_at_s<=?) OR
		(state='failed' AND json_extract(result_json,'$.error')='preview_build_failed' AND EXISTS(SELECT 1 FROM http_cleanup_preview_items i WHERE i.preview_id=p.id))
		ORDER BY created_at_s,id LIMIT 1`, now.Unix(), now.Add(-24*time.Hour).Unix(), now.Unix(), now.Add(-24*time.Hour).Unix()).Scan(&id, &expired)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM http_cleanup_preview_items WHERE preview_id=? AND ordinal IN (SELECT ordinal FROM http_cleanup_preview_items WHERE preview_id=? ORDER BY ordinal LIMIT 1000)`, id, id); err != nil {
		return false, err
	}
	if expired {
		if _, err = tx.ExecContext(ctx, `DELETE FROM http_cleanup_previews WHERE id=? AND NOT EXISTS(SELECT 1 FROM http_cleanup_preview_items WHERE preview_id=?)`, id, id); err != nil {
			return false, err
		}
	}
	return false, tx.Commit()
}
