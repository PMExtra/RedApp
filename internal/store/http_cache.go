package store

import (
	"context"
	"database/sql"
	"encoding/json"
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
	return queryHTTPCacheEntries(context.Background(), s.read, `WHERE storage_id=? AND is_current=1 ORDER BY path`, storageID)
}

// HTTPCacheEntriesAfter lists up to limit current entries in path order whose
// path sorts after path, or at or after it when inclusive.
func (s *Store) HTTPCacheEntriesAfter(storageID, path string, inclusive bool, limit int) ([]HTTPCacheEntry, error) {
	condition := `path>?`
	if inclusive {
		condition = `path>=?`
	}
	return queryHTTPCacheEntries(context.Background(), s.read, `WHERE storage_id=? AND is_current=1 AND `+condition+` ORDER BY path LIMIT ?`, storageID, path, limit)
}

// AllHTTPCacheEntries lists every entry, current or retired, of every application.
func (s *Store) AllHTTPCacheEntries() ([]HTTPCacheEntry, error) {
	return queryHTTPCacheEntries(context.Background(), s.read, `ORDER BY row_no`)
}

// CurrentHTTPCacheEntry returns the current entry of a path, or sql.ErrNoRows.
func (s *Store) CurrentHTTPCacheEntry(storageID, path string) (HTTPCacheEntry, error) {
	return scanHTTPCacheEntry(s.read.QueryRow(`SELECT `+httpEntryColumns+` FROM http_cache_generations WHERE storage_id=? AND path=? AND is_current=1`, storageID, path))
}

// HTTPCacheEntry returns an entry by ID, or sql.ErrNoRows.
func (s *Store) HTTPCacheEntry(id string) (HTTPCacheEntry, error) {
	return scanHTTPCacheEntry(s.read.QueryRow(`SELECT `+httpEntryColumns+` FROM http_cache_generations WHERE id=?`, id))
}

// RetireHTTPCacheEntry makes an entry non-current. Retiring twice keeps the
// first retirement time.
func (s *Store) RetireHTTPCacheEntry(id string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE http_cache_generations SET is_current=0,retired_at_s=COALESCE(retired_at_s,?) WHERE id=?`, at.Unix(), id)
	return err
}

// DeleteRetiredHTTPCacheEntry removes a retired entry's row after its body was
// deleted. It reports whether a row was removed; current entries are kept.
func (s *Store) DeleteRetiredHTTPCacheEntry(id string) (bool, error) {
	result, err := s.db.Exec(`DELETE FROM http_cache_generations WHERE id=? AND is_current=0`, id)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// TouchHTTPCacheEntry advances an entry's access bucket; buckets never move
// back. It returns sql.ErrNoRows when the entry no longer exists.
func (s *Store) TouchHTTPCacheEntry(id string, bucket int64) error {
	result, err := s.db.Exec(`UPDATE http_cache_generations SET last_access_bucket_s=MAX(last_access_bucket_s,?) WHERE id=?`, bucket, id)
	return affected(result, err)
}

// PublishHTTPCacheEntry makes e the current entry of its path while the source
// fence is still active. With replaces set, the path's current entry must
// still be that entry (ErrConflict otherwise); it is retired at the same time.
// The entry's body file must already exist under its final name.
func (s *Store) PublishHTTPCacheEntry(e HTTPCacheEntry, fence SourceFence, replaces string, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.requireSourceActive(tx, e.StorageID, fence); err != nil {
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
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.requireSourceActive(tx, storageID, fence); err != nil {
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
	tx, err := s.read.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err = s.requireSourceActive(tx, storageID, fence); err != nil {
		return false, err
	}
	var current bool
	if err = tx.QueryRow(`SELECT is_current FROM http_cache_generations WHERE id=? AND storage_id=?`, id, storageID).Scan(&current); err != nil {
		return false, err
	}
	return current, tx.Commit()
}

// HTTPCachePreviewDetail is the frozen detail of one HTTP cache preview item.
type HTTPCachePreviewDetail struct {
	// AccessBucket is the entry's access bucket when it was frozen; a
	// last_access cleanup skips the entry once it moved.
	AccessBucket int64 `json:"access_bucket"`
	// Basis is the cleanup basis that selected the entry, empty for refresh.
	Basis string `json:"basis,omitempty"`
	// RuleIndex is the automatic cleanup rule that selected the entry, or -1.
	RuleIndex int `json:"rule_index"`
}

// HTTPCachePreviewDetailOf decodes the detail of an HTTP cache preview item.
func HTTPCachePreviewDetailOf(item PreviewItem) (HTTPCachePreviewDetail, error) {
	var detail HTTPCachePreviewDetail
	err := json.Unmarshal(item.Detail, &detail)
	return detail, err
}

// HTTPPreviewPage reports one frozen page: how many current entries were
// scanned and selected, and the last scanned row.
type HTTPPreviewPage struct {
	Scanned, Selected int
	Last              int64
}

// HTTPCacheChoice is how a preview treats one scanned entry.
type HTTPCacheChoice struct {
	Selected bool
	// Active marks a selected entry that is in use.
	Active bool
	// Stop ends the page after this entry.
	Stop   bool
	Detail HTTPCachePreviewDetail
}

// FreezeHTTPCachePreviewPage extends a building HTTP cache preview: it scans
// up to limit current entries of the preview's source after row `after` and
// not beyond its high water, and choose decides each entry. Items are
// ordered by entry row. The items and counters are stored in one
// transaction under the preview's fence.
func (s *Store) FreezeHTTPCachePreviewPage(ctx context.Context, p Preview, after int64, limit int, choose func(HTTPCacheEntry) HTTPCacheChoice) (HTTPPreviewPage, error) {
	var page HTTPPreviewPage
	if !previewKinds[p.Kind].cache {
		return page, errors.New("not an HTTP cache preview")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err = checkPreview(tx, p, p.CreatedAt); err != nil {
		return page, err
	}
	entries, err := queryHTTPCacheEntries(ctx, tx, httpPageCondition, p.StorageID(), after, p.HighWater, limit)
	if err != nil {
		return page, err
	}
	items := []PreviewItem{}
	active := 0
	for _, e := range entries {
		page.Scanned++
		page.Last = e.RowNo
		choice := choose(e)
		if choice.Selected {
			detail, err := json.Marshal(choice.Detail)
			if err != nil {
				return page, err
			}
			items = append(items, PreviewItem{Ordinal: e.RowNo, Ref: e.ID, Label: e.Path, SizeBytes: e.SizeBytes, Selected: true, Detail: detail})
			if choice.Active {
				active++
			}
		}
		if choice.Stop {
			break
		}
	}
	selected, bytes, err := appendPreviewItems(ctx, tx, p.ID, items)
	if err != nil {
		return page, err
	}
	page.Selected = selected
	result, err := tx.ExecContext(ctx, `UPDATE previews SET scanned_count=scanned_count+?,selected_count=selected_count+?,selected_bytes=selected_bytes+?,active_count=active_count+? WHERE id=? AND state='building'`, page.Scanned, selected, bytes, active, p.ID)
	if err = affected(result, err); errors.Is(err, sql.ErrNoRows) {
		return page, ErrConflict
	} else if err != nil {
		return page, err
	}
	return page, tx.Commit()
}

// HTTPRetirement is the outcome of one cleanup batch.
type HTTPRetirement struct {
	Retired         []string // retired entry IDs, whose bodies may now be collected
	RetiredBytes    int64
	SkippedAccessed int
	SkippedChanged  int
}

// RetireHTTPCachePreviewItems executes one batch of a running HTTP cache
// cleanup preview in a single transaction under its fence. An item is retired
// only if its entry is still the current entry of the same path and, for the
// last_access basis, was not accessed since the preview; every item records
// its outcome.
func (s *Store) RetireHTTPCachePreviewItems(ctx context.Context, p Preview, items []PreviewItem, at time.Time) (HTTPRetirement, error) {
	var out HTTPRetirement
	if p.Kind != PreviewCacheCleanup {
		return out, errors.New("not an HTTP cache cleanup preview")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = requireRunning(ctx, tx, p); err != nil {
		return out, err
	}
	for _, item := range items {
		if err = ctx.Err(); err != nil {
			return HTTPRetirement{}, err
		}
		detail, err := HTTPCachePreviewDetailOf(item)
		if err != nil {
			return HTTPRetirement{}, err
		}
		e, err := scanHTTPCacheEntry(tx.QueryRowContext(ctx, `SELECT `+httpEntryColumns+` FROM http_cache_generations WHERE id=? AND storage_id=?`, item.Ref, p.StorageID()))
		status := "retired"
		switch {
		case errors.Is(err, sql.ErrNoRows):
			status = "skipped_changed"
		case err != nil:
			return HTTPRetirement{}, err
		case !e.Current || e.Path != item.Label:
			status = "skipped_changed"
		case detail.Basis == "last_access" && e.AccessBucket != detail.AccessBucket:
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
		if err = recordPreviewItem(ctx, tx, p.ID, PreviewOutcome{Ordinal: item.Ordinal, Status: status}); err != nil {
			return HTTPRetirement{}, err
		}
	}
	return out, tx.Commit()
}
