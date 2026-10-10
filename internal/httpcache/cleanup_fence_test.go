package httpcache

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/PMExtra/RedApp/internal/store"
)

func TestCleanupStopsBetweenBatchesWhenRuntimeRevisionChanges(t *testing.T) {
	f := newFixture(t, http.NotFoundHandler(), 300)
	seedMaintenanceRows(t, f, 1205)
	preview, err := f.s.PreviewCleanup(context.Background(), f.entry, "fetched_at", f.s.now(), allPaths)
	if err != nil || preview.SelectedFiles != 1205 {
		t.Fatal(preview, err)
	}
	// Advance the application's runtime revision as the hundredth item's
	// result is recorded. Both become visible at the first batch commit; the
	// next batch must recheck the captured fence before retiring another row.
	// This models a policy edit without adding a production synchronization hook.
	_, err = f.sql(t).Exec(`CREATE TRIGGER change_revision_after_cleanup_batch
		AFTER UPDATE OF completed_count ON previews
		WHEN NEW.kind='cache_cleanup' AND OLD.completed_count=99 AND NEW.completed_count=100
		BEGIN UPDATE applications SET revision=revision+1,runtime_revision=runtime_revision+1; END`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.s.ExecuteCleanup(context.Background(), f.entry, preview.ID)
	if !errors.Is(err, store.ErrPreviewStale) || result.SelectedFiles != 1205 || result.RetiredFiles != 100 || result.RetiredBytes != 700 || result.SkippedAccessed != 0 || result.SkippedChanged != 0 {
		t.Fatal("cleanup continued past the changed revision or lost its committed batch", result, err)
	}
	status, err := f.s.LookupPreview(f.entry.StorageID(), "cleanup", preview.ID)
	var receipt CleanupResult
	if err != nil || status.State != "failed" || status.CompletedFiles != 100 || status.FailedFiles != 0 || json.Unmarshal(status.Result, &receipt) != nil || receipt != result {
		t.Fatal("partial cleanup receipt is inaccurate", status, receipt, err)
	}
	var current, early, retired, pending int
	if err = f.sql(t).QueryRow(`SELECT COUNT(*),COALESCE(SUM(path<'seed/000100'),0) FROM http_cache_generations WHERE storage_id=? AND is_current=1`, f.entry.StorageID()).Scan(&current, &early); err != nil {
		t.Fatal(err)
	}
	if current != 1105 || early != 0 {
		t.Fatal("unprocessed generations were removed or the first batch was rolled back", current, early)
	}
	if err = f.sql(t).QueryRow(`SELECT COALESCE(SUM(result_status='retired'),0),COALESCE(SUM(result_status='pending'),0) FROM preview_items WHERE preview_id=?`, preview.ID).Scan(&retired, &pending); err != nil {
		t.Fatal(err)
	}
	if retired != 100 || pending != 1105 {
		t.Fatal("frozen item results disagree with committed work", retired, pending)
	}
	var revision, epoch int64
	if err = f.sql(t).QueryRow(`SELECT revision,source_epoch FROM applications WHERE uid=?`, f.entry.UID).Scan(&revision, &epoch); err != nil || revision != f.entry.Revision+1 || epoch != f.entry.SourceEpoch {
		t.Fatal("test did not advance the policy fence within the same source", revision, epoch, err)
	}
	again, err := f.s.ExecuteCleanup(context.Background(), f.entry, preview.ID)
	if !errors.Is(err, ErrInvalidPreview) || again != receipt {
		t.Fatal("failed receipt retry resumed the remaining selection", again, err)
	}
	if err = f.sql(t).QueryRow(`SELECT COUNT(*) FROM http_cache_generations WHERE storage_id=? AND is_current=1`, f.entry.StorageID()).Scan(&current); err != nil || current != 1105 {
		t.Fatal("failed receipt retry changed surviving files", current, err)
	}
}
