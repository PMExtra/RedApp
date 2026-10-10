package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/PMExtra/RedApp/internal/identity"
)

// PreviewKind names what a frozen preview selects and how it executes.
type PreviewKind string

const (
	PreviewVersionCleanup PreviewKind = "version_cleanup"
	PreviewRetention      PreviewKind = "retention"
	PreviewCacheRefresh   PreviewKind = "cache_refresh"
	PreviewCacheCleanup   PreviewKind = "cache_cleanup"
)

// PreviewState is the lifecycle position of a preview. Expiry is not a state:
// it follows from the times (see Preview.Readable).
type PreviewState string

const (
	PreviewBuilding PreviewState = "building"
	PreviewReady    PreviewState = "ready"
	PreviewRunning  PreviewState = "running"
	PreviewDone     PreviewState = "done"
	PreviewFailed   PreviewState = "failed"
)

const (
	// PreviewLifetime is how long a preview may be executed after creation.
	PreviewLifetime = 10 * time.Minute
	// ReceiptLifetime is how long the receipt of an execution stays readable.
	ReceiptLifetime = 24 * time.Hour
)

// ErrPreviewStale means the source, the policy or a frozen object changed
// since the preview: nothing was changed and a new preview is required.
var ErrPreviewStale = errors.New("source, policy or frozen objects changed since the preview")

// ErrPreviewRunning means the preview is still being built or executed.
var ErrPreviewRunning = errors.New("preview is being built or executed")

// Preview is a frozen selection of one application source epoch. The fence
// and kind guard are checked whenever the preview is created, built or
// executed; Criteria and Summary are owned by the kind.
type Preview struct {
	ID          string
	Kind        PreviewKind
	AppUID      string
	SourceEpoch int64
	// Fence is the source fence the selection was frozen under.
	Fence SourceFence
	// RequireActive also requires the source to serve, as retention, refresh
	// and automatic cleanup do; explicit cleanup works on disabled and old sources.
	RequireActive bool
	State         PreviewState
	CreatedAt     time.Time
	ExpiresAt     time.Time
	ExecutedAt    *time.Time
	// Criteria are the frozen inputs and guards of the kind.
	Criteria json.RawMessage
	// Summary holds the kind's frozen estimates shown with the preview.
	Summary json.RawMessage
	// HighWater is the last HTTP cache row an HTTP cache selection may contain.
	HighWater int64
	// ScannedItems counts the evaluated candidates, ActiveItems the selected
	// ones in use when they were frozen.
	ScannedItems   int
	SelectedItems  int
	SelectedBytes  int64
	ActiveItems    int
	CompletedItems int
	FailedItems    int
	// Result is the receipt of a finished execution.
	Result json.RawMessage
}

// StorageID is the storage namespace of the previewed source epoch.
func (p Preview) StorageID() string { return identity.StorageID(p.AppUID, p.SourceEpoch) }

// Finished reports whether the preview has a receipt.
func (p Preview) Finished() bool { return p.State == PreviewDone || p.State == PreviewFailed }

// Readable returns ErrExpired once a preview may no longer be read or
// executed: an unexecuted preview at ExpiresAt and a receipt ReceiptLifetime
// after execution. A running execution never expires.
func (p Preview) Readable(now time.Time) error {
	switch {
	case p.Finished() && (p.ExecutedAt == nil || !now.Before(p.ExecutedAt.Add(ReceiptLifetime))):
		return ErrExpired
	case (p.State == PreviewReady || p.State == PreviewBuilding) && !now.Before(p.ExpiresAt):
		return ErrExpired
	}
	return nil
}

// PreviewItem is one frozen candidate of a preview and its execution outcome.
// Ordinals are positive and increase in display order.
type PreviewItem struct {
	Ordinal int64
	// Ref identifies the frozen object (an HTTP cache entry), Label is what the
	// administrator sees (a path or a version).
	Ref       string
	Label     string
	SizeBytes int64
	// Selected items are executed; the others are listed for review only.
	Selected bool
	// Detail is the kind's frozen detail of the item.
	Detail json.RawMessage
	// Status is "pending" until executed and "kept" for unselected items.
	Status    string
	ErrorCode string
}

// PreviewOutcome is the execution result of one item.
type PreviewOutcome struct {
	Ordinal   int64
	Status    string
	ErrorCode string
}

const (
	itemPending = "pending"
	itemKept    = "kept"
)

// previewRules is what differs between kinds inside the shared transactions.
type previewRules struct {
	// cache selections scan HTTP cache rows up to a high water.
	cache bool
	// guard checks conditions beyond the source fence when the preview is
	// created and before it executes.
	guard func(tx *sql.Tx, p Preview, at time.Time) error
	// admit validates the items frozen at creation.
	admit func(tx *sql.Tx, p Preview, items []PreviewItem) error
}

var previewKinds = map[PreviewKind]previewRules{
	PreviewVersionCleanup: {admit: admitReleaseItems},
	PreviewRetention:      {guard: guardRetention, admit: admitReleaseItems},
	PreviewCacheRefresh:   {cache: true},
	PreviewCacheCleanup:   {cache: true},
}

const previewColumns = `id,app_uid,source_epoch,kind,state,app_revision,vendor_revision,require_active,created_at_s,expires_at_s,executed_at_s,criteria_json,summary_json,high_water,scanned_count,selected_count,selected_bytes,active_count,completed_count,failed_count,result_json`

func scanPreview(row scanner) (Preview, error) {
	var p Preview
	var created, expires int64
	var executed sql.NullInt64
	var criteria, summary, result []byte
	err := row.Scan(&p.ID, &p.AppUID, &p.SourceEpoch, &p.Kind, &p.State, &p.Fence.AppRuntimeRevision, &p.Fence.VendorRuntimeRevision, &p.RequireActive, &created, &expires, &executed, &criteria, &summary, &p.HighWater, &p.ScannedItems, &p.SelectedItems, &p.SelectedBytes, &p.ActiveItems, &p.CompletedItems, &p.FailedItems, &result)
	p.CreatedAt = time.Unix(created, 0).UTC()
	p.ExpiresAt = time.Unix(expires, 0).UTC()
	p.ExecutedAt = timePointer(executed)
	p.Criteria, p.Summary = criteria, summary
	if result != nil {
		p.Result = result
	}
	return p, err
}

const previewItemColumns = `ordinal,ref,label,size_bytes,selected,detail_json,result_status,error_code`

func scanPreviewItem(row scanner) (PreviewItem, error) {
	var item PreviewItem
	var detail []byte
	err := row.Scan(&item.Ordinal, &item.Ref, &item.Label, &item.SizeBytes, &item.Selected, &detail, &item.Status, &item.ErrorCode)
	item.Detail = detail
	return item, err
}

// loadPreview reads preview id of kind owned by uid while it is readable.
func loadPreview(ctx context.Context, q rowQuerier, uid string, kind PreviewKind, id string, now time.Time) (Preview, error) {
	p, err := scanPreview(q.QueryRowContext(ctx, `SELECT `+previewColumns+` FROM previews WHERE id=? AND app_uid=? AND kind=?`, id, uid, kind))
	if err != nil {
		return p, err
	}
	return p, p.Readable(now)
}

// checkPreview checks the frozen source fence and the kind's guard.
func checkPreview(tx *sql.Tx, p Preview, at time.Time) error {
	source, err := scanSource(tx.QueryRow(`SELECT `+sourceColumns+sourceJoin+` WHERE src.app_uid=? AND src.epoch=?`, p.AppUID, p.SourceEpoch))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPreviewStale
	}
	if err != nil {
		return err
	}
	if source.Fence() != p.Fence || p.RequireActive && !source.Active {
		return ErrPreviewStale
	}
	if guard := previewKinds[p.Kind].guard; guard != nil {
		return guard(tx, p, at)
	}
	return nil
}

func validNewPreview(p Preview) error {
	rules, ok := previewKinds[p.Kind]
	switch {
	case !ok, !identity.ValidUID(p.ID), !identity.ValidUID(p.AppUID), p.SourceEpoch < 1:
		return errors.New("invalid preview identity")
	case p.State != PreviewReady && (p.State != PreviewBuilding || !rules.cache):
		return errors.New("invalid preview state")
	case p.CreatedAt.IsZero(), !p.ExpiresAt.After(p.CreatedAt), p.ExpiresAt.Sub(p.CreatedAt) > PreviewLifetime:
		return errors.New("invalid preview lifetime")
	case p.ScannedItems < 0, p.ActiveItems < 0:
		return errors.New("invalid preview counters")
	}
	return nil
}

func jsonOrEmpty(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return []byte(`{}`), nil
	}
	if !json.Valid(raw) {
		return nil, errors.New("invalid preview JSON")
	}
	return raw, nil
}

// CreatePreview stores a new preview under its fence and the kind's guard,
// together with the items frozen so far. A ready preview is complete; a
// building HTTP cache preview grows with FreezeHTTPCachePreviewPage until
// FinishPreviewBuild. The returned preview carries the stored counters.
func (s *Store) CreatePreview(ctx context.Context, p Preview, items []PreviewItem) (Preview, error) {
	if err := validNewPreview(p); err != nil {
		return p, err
	}
	criteria, err := jsonOrEmpty(p.Criteria)
	if err != nil {
		return p, err
	}
	summary, err := jsonOrEmpty(p.Summary)
	if err != nil {
		return p, err
	}
	p.Criteria, p.Summary = criteria, summary
	p.ExecutedAt, p.Result, p.HighWater = nil, nil, 0
	p.SelectedItems, p.SelectedBytes, p.CompletedItems, p.FailedItems = 0, 0, 0, 0
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if err = checkPreview(tx, p, p.CreatedAt); err != nil {
		return p, err
	}
	rules := previewKinds[p.Kind]
	if rules.admit != nil {
		if err = rules.admit(tx, p, items); err != nil {
			return p, err
		}
	}
	if rules.cache {
		if err = tx.QueryRowContext(ctx, httpHighWaterQuery, p.StorageID()).Scan(&p.HighWater); err != nil {
			return p, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO previews(id,app_uid,source_epoch,kind,state,app_revision,vendor_revision,require_active,created_at_s,expires_at_s,criteria_json,summary_json,high_water,scanned_count,active_count) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.AppUID, p.SourceEpoch, p.Kind, p.State, p.Fence.AppRuntimeRevision, p.Fence.VendorRuntimeRevision, p.RequireActive, p.CreatedAt.Unix(), p.ExpiresAt.Unix(), []byte(criteria), []byte(summary), p.HighWater, p.ScannedItems, p.ActiveItems)
	if err != nil {
		return p, err
	}
	selected, bytes, err := appendPreviewItems(ctx, tx, p.ID, items)
	if err != nil {
		return p, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE previews SET selected_count=?,selected_bytes=? WHERE id=?`, selected, bytes, p.ID); err != nil {
		return p, err
	}
	p.SelectedItems, p.SelectedBytes = selected, bytes
	return p, tx.Commit()
}

// appendPreviewItems inserts frozen items and returns how many were selected
// and their size. Counters are the caller's to update.
func appendPreviewItems(ctx context.Context, tx *sql.Tx, id string, items []PreviewItem) (int, int64, error) {
	selected, bytes := 0, int64(0)
	for _, item := range items {
		if item.Ordinal < 1 || item.SizeBytes < 0 {
			return 0, 0, errors.New("invalid preview item")
		}
		detail, err := jsonOrEmpty(item.Detail)
		if err != nil {
			return 0, 0, err
		}
		status := itemKept
		if item.Selected {
			status = itemPending
			selected++
			bytes += item.SizeBytes
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO preview_items(preview_id,ordinal,ref,label,size_bytes,selected,detail_json,result_status) VALUES(?,?,?,?,?,?,?,?)`,
			id, item.Ordinal, item.Ref, item.Label, item.SizeBytes, item.Selected, []byte(detail), status)
		if err != nil {
			return 0, 0, err
		}
	}
	return selected, bytes, nil
}

// FinishPreviewBuild makes a built preview ready while its fence holds.
func (s *Store) FinishPreviewBuild(ctx context.Context, p Preview) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkPreview(tx, p, p.CreatedAt); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE previews SET state='ready' WHERE id=? AND state='building'`, p.ID)
	if err = affected(result, err); errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	} else if err != nil {
		return err
	}
	return tx.Commit()
}

// FailPreviewBuild records a build that did not finish. Its partial selection
// is discarded later by PrunePreviews.
func (s *Store) FailPreviewBuild(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE previews SET state='failed',executed_at_s=?,result_json=? WHERE id=? AND state='building'`, at.Unix(), []byte(`{"error":"preview_build_failed"}`), id)
	return err
}

// DeleteEmptyPreview removes a ready preview that selected nothing.
func (s *Store) DeleteEmptyPreview(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM previews WHERE id=? AND selected_count=0 AND state='ready'`, id)
	return err
}

// Preview returns preview id of kind owned by the application uid while it is
// readable at now. Unknown previews, and those of another application or
// kind, are ErrNotFound; expired previews and receipts are ErrExpired.
func (s *Store) Preview(uid string, kind PreviewKind, id string, now time.Time) (Preview, error) {
	return loadPreview(context.Background(), s.read, uid, kind, id, now)
}

// PreviewItems lists up to limit items of a preview with an ordinal above
// after in ordinal order, only the selected items without an outcome when
// pendingOnly is set. Items never change position, so pages are stable.
func (s *Store) PreviewItems(ctx context.Context, id string, after int64, limit int, pendingOnly bool) ([]PreviewItem, error) {
	if limit < 1 {
		return nil, errors.New("invalid preview item limit")
	}
	return queryPreviewItems(ctx, s.read, id, after, limit, pendingOnly)
}

// AllPreviewItems lists every item of a preview in ordinal order.
func (s *Store) AllPreviewItems(ctx context.Context, id string) ([]PreviewItem, error) {
	return queryPreviewItems(ctx, s.read, id, 0, -1, false)
}

func queryPreviewItems(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, id string, after int64, limit int, pendingOnly bool) ([]PreviewItem, error) {
	condition := ``
	if pendingOnly {
		condition = ` AND result_status='pending'`
	}
	rows, err := q.QueryContext(ctx, `SELECT `+previewItemColumns+` FROM preview_items WHERE preview_id=? AND ordinal>?`+condition+` ORDER BY ordinal LIMIT ?`, id, after, limit)
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

// beginExecution reads a preview for execution in tx. It returns the preview
// and false when it already has a receipt, which execution repeats instead
// of running again. A ready preview must be of storageID's source epoch and
// pass its fence and guard.
func beginExecution(ctx context.Context, tx *sql.Tx, storageID string, kind PreviewKind, id string, at time.Time) (Preview, bool, error) {
	uid, epoch, ok := identity.ParseStorageID(storageID)
	if !ok {
		return Preview{}, false, ErrNotFound
	}
	p, err := loadPreview(ctx, tx, uid, kind, id, at)
	switch {
	case err != nil:
		return p, false, err
	case p.Finished():
		return p, false, nil
	case p.State != PreviewReady:
		return p, false, ErrPreviewRunning
	case p.SourceEpoch != epoch:
		return p, false, ErrPreviewStale
	}
	return p, true, checkPreview(tx, p, at)
}

// ClaimPreview starts executing a ready preview of storageID's source epoch:
// it checks the fence and guard and moves the preview to running. A preview
// with a receipt is returned unclaimed; one being built or executed is
// ErrPreviewRunning.
func (s *Store) ClaimPreview(ctx context.Context, storageID string, kind PreviewKind, id string, now time.Time) (Preview, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Preview{}, false, err
	}
	defer tx.Rollback()
	p, start, err := beginExecution(ctx, tx, storageID, kind, id, now)
	if err != nil || !start {
		return p, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE previews SET state='running' WHERE id=? AND state='ready'`, p.ID); err != nil {
		return p, false, err
	}
	p.State = PreviewRunning
	return p, true, tx.Commit()
}

// requireRunning checks in tx that p is still running under its fence.
func requireRunning(ctx context.Context, tx *sql.Tx, p Preview) error {
	var state PreviewState
	if err := tx.QueryRowContext(ctx, `SELECT state FROM previews WHERE id=?`, p.ID).Scan(&state); err != nil {
		return err
	}
	if state != PreviewRunning {
		return ErrConflict
	}
	return checkPreview(tx, p, time.Now())
}

func recordPreviewItem(ctx context.Context, tx *sql.Tx, id string, outcome PreviewOutcome) error {
	if outcome.Status == "" || outcome.Status == itemPending || outcome.Status == itemKept {
		return errors.New("invalid preview item outcome")
	}
	result, err := tx.ExecContext(ctx, `UPDATE preview_items SET result_status=?,error_code=? WHERE preview_id=? AND ordinal=? AND result_status='pending'`, outcome.Status, outcome.ErrorCode, id, outcome.Ordinal)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return err
	}
	failed := 0
	if outcome.Status == "failed" {
		failed = 1
	}
	_, err = tx.ExecContext(ctx, `UPDATE previews SET completed_count=completed_count+1,failed_count=failed_count+? WHERE id=?`, failed, id)
	return err
}

// RecordPreviewItems stores the outcomes of pending items of a running
// preview while its fence holds. An item keeps its first outcome.
func (s *Store) RecordPreviewItems(ctx context.Context, p Preview, outcomes []PreviewOutcome) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = requireRunning(ctx, tx, p); err != nil {
		return err
	}
	for _, outcome := range outcomes {
		if err = recordPreviewItem(ctx, tx, p.ID, outcome); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FinishPreview stores the final state and receipt of a running execution.
// Finishing a finished preview keeps its first receipt.
func (s *Store) FinishPreview(ctx context.Context, p Preview, state PreviewState, result json.RawMessage, at time.Time) error {
	if state != PreviewDone && state != PreviewFailed || !json.Valid(result) {
		return errors.New("invalid preview receipt")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current PreviewState
	if err = tx.QueryRowContext(ctx, `SELECT state FROM previews WHERE id=?`, p.ID).Scan(&current); err != nil {
		return err
	}
	switch current {
	case PreviewDone, PreviewFailed:
		return nil
	case PreviewRunning:
	default:
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE previews SET state=?,executed_at_s=?,result_json=? WHERE id=?`, state, at.Unix(), []byte(result), p.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// InterruptPreviews fails every preview of kinds a previous process left
// building or running. An interrupted execution is a receipt, never a
// restartable queue; bounded batches keep this independent of the number of
// previews.
func (s *Store) InterruptPreviews(ctx context.Context, at time.Time, kinds ...PreviewKind) error {
	for _, kind := range kinds {
		for {
			result, err := s.db.ExecContext(ctx, `UPDATE previews SET
				result_json=json_object('error',CASE WHEN state='building' THEN 'preview_build_failed' ELSE 'interrupted_by_restart' END,
				'selected_items',selected_count,'completed_items',completed_count,'failed_items',failed_count),
				executed_at_s=?,state='failed'
				WHERE id IN (SELECT id FROM previews WHERE kind=? AND state IN ('building','running') LIMIT 100)`, at.Unix(), kind)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n < 100 {
				break
			}
		}
	}
	return nil
}

// PrunePreviews deletes expired previews, receipts older than ReceiptLifetime
// and the selections of failed builds. Each of at most batches transactions
// removes at most 1000 items of one preview and then the preview once it has
// none, so no unbounded cascade runs.
func (s *Store) PrunePreviews(ctx context.Context, now time.Time, batches int) error {
	for i := 0; i < batches; i++ {
		done, err := s.prunePreviewBatch(ctx, now)
		if err != nil || done {
			return err
		}
	}
	return nil
}

func (s *Store) prunePreviewBatch(ctx context.Context, now time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	expiry, receipts := now.Unix(), now.Add(-ReceiptLifetime).Unix()
	var id string
	var expired bool
	err = tx.QueryRowContext(ctx, `SELECT id,
		(state='ready' AND expires_at_s<=?) OR (state IN ('done','failed') AND executed_at_s<=?)
		FROM previews p WHERE
		(state='ready' AND expires_at_s<=?) OR
		(state IN ('done','failed') AND executed_at_s<=?) OR
		(state='failed' AND json_extract(result_json,'$.error')='preview_build_failed' AND EXISTS(SELECT 1 FROM preview_items i WHERE i.preview_id=p.id))
		ORDER BY created_at_s,id LIMIT 1`, expiry, receipts, expiry, receipts).Scan(&id, &expired)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM preview_items WHERE preview_id=? AND ordinal IN (SELECT ordinal FROM preview_items WHERE preview_id=? ORDER BY ordinal LIMIT 1000)`, id, id); err != nil {
		return false, err
	}
	if expired {
		if _, err = tx.ExecContext(ctx, `DELETE FROM previews WHERE id=? AND NOT EXISTS(SELECT 1 FROM preview_items WHERE preview_id=?)`, id, id); err != nil {
			return false, err
		}
	}
	return false, tx.Commit()
}
