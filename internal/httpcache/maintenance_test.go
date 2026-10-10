package httpcache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/store"
)

func seedMaintenanceRows(t *testing.T, f *fixture, count int) {
	t.Helper()
	tx, err := f.sql(t).Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	old := f.s.now().Add(-time.Hour).Unix()
	for i := 0; i < count; i++ {
		_, err = tx.Exec(`INSERT INTO http_cache_generations(id,storage_id,path,sha256,size_bytes,fetched_at_s,validated_at_s,last_access_bucket_s,fresh_until_s,headers_json,is_current) VALUES(?,?,?,?,7,?,?,0,?,'{}',1)`, testID(t), f.entry.StorageID(), fmt.Sprintf("seed/%06d", i), strings.Repeat("a", 64), old, old, old)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenancePaginationFrozenBoundaryAndWholeCleanup(t *testing.T) {
	f := newFixture(t, http.NotFoundHandler(), 300)
	seedMaintenanceRows(t, f, 1205)
	// An insert immediately after capturing the high-water mark must not join
	// the selection, even though subsequent selection pages can see it.
	_, err := f.sql(t).Exec(`CREATE TRIGGER add_after_highwater AFTER INSERT ON http_cleanup_previews BEGIN
		INSERT INTO http_cache_generations(id,storage_id,path,sha256,size_bytes,fetched_at_s,validated_at_s,last_access_bucket_s,fresh_until_s,headers_json,is_current)
		SELECT lower(hex(randomblob(16))),NEW.storage_id,'late/file',sha256,7,fetched_at_s,validated_at_s,0,fresh_until_s,'{}',1 FROM http_cache_generations LIMIT 1;
	END`)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := f.s.PreviewCleanup(context.Background(), f.entry, "fetched_at", f.s.now(), allPaths)
	if err != nil || preview.SelectedFiles != 1205 || preview.SelectedBytes != 1205*7 || preview.ScannedFiles != 1205 {
		t.Fatal(preview, err)
	}
	var criteria []byte
	if err = f.sql(t).QueryRow(`SELECT selection_json FROM http_cleanup_previews WHERE id=?`, preview.ID).Scan(&criteria); err != nil {
		t.Fatal(err)
	}
	if len(criteria) > 512 || strings.Contains(string(criteria), "generation_id") {
		t.Fatal("selection embedded the entire row set", len(criteria))
	}
	cursor, seen, last := "", 0, int64(0)
	for {
		page, err := f.s.PreviewItems(f.entry.StorageID(), "cleanup", preview.ID, cursor, 25)
		if err != nil || len(page.Items) > 25 || page.TotalFiles != 1205 || page.TotalBytes != 1205*7 {
			t.Fatal(page, err)
		}
		for _, item := range page.Items {
			if item.Ordinal <= last || item.Path == "late/file" {
				t.Fatal("unstable page or late insert", item)
			}
			last = item.Ordinal
			seen++
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if seen != 1205 {
		t.Fatal("pagination lost or repeated items", seen)
	}
	if _, err = f.s.PreviewItems(f.entry.StorageID(), "refresh", preview.ID, "", 25); !errors.Is(err, ErrInvalidPreview) {
		t.Fatal("kind confused", err)
	}
	if _, err = f.s.PreviewItems("different/epoch", "cleanup", preview.ID, "", 25); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("ownership confused", err)
	}
	if _, err = f.s.PreviewItems(f.entry.StorageID(), "cleanup", preview.ID, "01", 25); !errors.Is(err, ErrInvalidPreview) {
		t.Fatal("noncanonical cursor", err)
	}
	result, err := f.s.ExecuteCleanup(context.Background(), f.entry, preview.ID)
	if err != nil || result.RetiredFiles != 1205 || result.RetiredBytes != 1205*7 {
		t.Fatal(result, err)
	}
	rows := f.rows(t)
	if len(rows) != 1 || rows[0].Path != "late/file" {
		t.Fatal("cleanup affected an unfrozen insertion", rows)
	}
	again, err := f.s.ExecuteCleanup(context.Background(), f.entry, preview.ID)
	if err != nil || again != result {
		t.Fatal("receipt not idempotent", again, err)
	}
	status, err := f.s.LookupPreview(f.entry.StorageID(), "cleanup", preview.ID)
	if err != nil || status.CompletedFiles != 1205 || status.State != "done" || len(status.Result) == 0 {
		t.Fatal(status, err)
	}
}

func TestMaintenanceReceiptsExpiryPruningAndRestart(t *testing.T) {
	f := newFixture(t, http.NotFoundHandler(), 300)
	seedMaintenanceRows(t, f, 1205)
	preview, err := f.s.PreviewRefresh(context.Background(), f.entry, allPaths)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.claimPreview(context.Background(), f.entry, "refresh", preview.ID); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(11 * 60)
	if _, err = f.s.LookupPreview(f.entry.StorageID(), "refresh", preview.ID); err != nil {
		t.Fatal("running expired", err)
	}
	page, err := f.s.PreviewItems(f.entry.StorageID(), "refresh", preview.ID, "", 25)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.recordPreviewItem(context.Background(), f.entry, "refresh", preview.ID, page.Items[0].Ordinal, "refreshed", ""); err != nil {
		t.Fatal(err)
	}
	if err = f.s.recoverPreviews(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := f.s.LookupPreview(f.entry.StorageID(), "refresh", preview.ID)
	if err != nil || status.State != "failed" || status.CompletedFiles != 1 || !strings.Contains(string(status.Result), "interrupted_by_restart") {
		t.Fatal(status, err)
	}
	if _, err = f.s.claimPreview(context.Background(), f.entry, "refresh", preview.ID); err != nil {
		t.Fatal("terminal receipt cannot be read", err)
	}
	f.clock.Add(24 * 60 * 60)
	if _, err = f.s.LookupPreview(f.entry.StorageID(), "refresh", preview.ID); !errors.Is(err, store.ErrExpired) {
		t.Fatal("receipt expiry", err)
	}
	if err = f.s.prunePreviews(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	var items, headers int
	f.sql(t).QueryRow(`SELECT COUNT(*) FROM http_cleanup_preview_items WHERE preview_id=?`, preview.ID).Scan(&items)
	f.sql(t).QueryRow(`SELECT COUNT(*) FROM http_cleanup_previews WHERE id=?`, preview.ID).Scan(&headers)
	if items != 205 || headers != 1 {
		t.Fatal("unbounded cascade or early header deletion", items, headers)
	}
	if err = f.s.prunePreviews(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	f.sql(t).QueryRow(`SELECT COUNT(*) FROM http_cleanup_previews WHERE id=?`, preview.ID).Scan(&headers)
	if headers != 0 {
		t.Fatal("empty expired header retained")
	}
	ready, err := f.s.PreviewCleanup(context.Background(), f.entry, "fetched_at", f.s.now(), allPaths)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Add(601)
	if _, err = f.s.claimPreview(context.Background(), f.entry, "cleanup", ready.ID); !errors.Is(err, store.ErrExpired) {
		t.Fatal("ready preview did not expire", err)
	}
}

func TestCleanupCommittedBatchSurvivesPhysicalDeleteFailure(t *testing.T) {
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "body") }), 300)
	if _, err := f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	row := f.rows(t)[0]
	f.clock.Add(60)
	preview, err := f.s.PreviewCleanup(context.Background(), f.entry, "fetched_at", f.s.now(), allPaths)
	if err != nil {
		t.Fatal(err)
	}
	body := f.s.bodyPath(row.GenerationID)
	if err = os.Remove(body); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(body, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(body, "prevent-unlink"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := f.s.ExecuteCleanup(context.Background(), f.entry, preview.ID)
	if err == nil || result.RetiredFiles != 1 || result.RetiredBytes != 4 {
		t.Fatal("committed retirement omitted", result, err)
	}
	status, err := f.s.LookupPreview(f.entry.StorageID(), "cleanup", preview.ID)
	var receipt CleanupResult
	if err != nil || status.State != "failed" || status.CompletedFiles != 1 || json.Unmarshal(status.Result, &receipt) != nil || receipt != result {
		t.Fatal(status, receipt, err)
	}
}

func TestMaintenanceBuildCapacityAndCancellation(t *testing.T) {
	f := newFixture(t, http.NotFoundHandler(), 300)
	releases := []func(){}
	for i := 0; i < PreviewBuilderLimit; i++ {
		_, release, err := f.s.beginMaintenance(context.Background(), true)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	if _, err := f.s.PreviewCleanup(context.Background(), f.entry, "fetched_at", f.s.now(), allPaths); !errors.Is(err, ErrPreviewBusy) {
		t.Fatal("builder cap bypassed", err)
	}
	for _, release := range releases {
		release()
	}
	preview, err := f.s.PreviewCleanup(context.Background(), f.entry, "fetched_at", f.s.now(), allPaths)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = f.s.ExecuteCleanup(ctx, f.entry, preview.ID); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled cleanup ran", err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.PreviewCleanup(context.Background(), f.entry, "fetched_at", f.s.now(), allPaths); !errors.Is(err, ErrClosed) {
		t.Fatal("closed service admitted builder", err)
	}
}

func TestPreviewSurvivesEditsOutsideTheSourceFence(t *testing.T) {
	f := newFixture(t, http.NotFoundHandler(), 300)
	seedMaintenanceRows(t, f, 3)
	name := f.app.Name
	name.En = "Renamed"
	updated, err := f.db.UpdateApplication(f.app.Key, f.app.Revision, store.ApplicationChanges{Name: name, BaseURL: f.app.BaseURL, CacheTTLSeconds: 300, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if updated.RuntimeRevision != f.entry.RuntimeRevision || updated.Revision == f.entry.Revision {
		t.Fatal("a name edit should change only the configuration revision", updated)
	}
	f.entry.Revision = updated.Revision
	preview, err := f.s.PreviewCleanup(context.Background(), f.entry, "fetched_at", f.s.now(), allPaths)
	if err != nil || preview.SelectedFiles != 3 {
		t.Fatal(preview, err)
	}
	if result, err := f.s.ExecuteCleanup(context.Background(), f.entry, preview.ID); err != nil || result.RetiredFiles != 3 {
		t.Fatal("preview of a renamed application could not run", result, err)
	}
}
