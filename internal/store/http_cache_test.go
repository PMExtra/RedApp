package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func httpCacheFixture(t *testing.T) (*Store, string, SourceFence) {
	t.Helper()
	s := openTest(t)
	v, err := s.CreateVendor(directoryVendor("vendor"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateApplication(v.ID, directoryApplication("app"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := s.Source(a.StorageID())
	if err != nil {
		t.Fatal(err)
	}
	return s, a.StorageID(), source.Fence()
}

func httpEntry(storageID, id, path string, at time.Time) HTTPCacheEntry {
	return HTTPCacheEntry{ID: strings.Repeat(id, 32), StorageID: storageID, Path: path, SourceURL: "http://files.internal:8080/packages/" + path, SHA256: strings.Repeat("a", 64), SizeBytes: 3, Headers: []byte(`{}`), FetchedAt: at, ValidatedAt: at, FreshUntil: at.Add(time.Minute)}
}

func TestHTTPCacheEntryPublicationIsFencedAndReplacesOnlyTheExpectedEntry(t *testing.T) {
	s, storage, fence := httpCacheFixture(t)
	at := time.Unix(1_700_000_000, 0).UTC()
	first := httpEntry(storage, "1", "file", at)
	if err := s.PublishHTTPCacheEntry(first, fence, "", at); err != nil {
		t.Fatal(err)
	}
	stale := fence
	stale.AppRuntimeRevision++
	if err := s.PublishHTTPCacheEntry(httpEntry(storage, "2", "file", at), stale, "", at); !errors.Is(err, ErrSourceInactive) {
		t.Fatal("stale fence published", err)
	}
	if err := s.PublishHTTPCacheEntry(httpEntry(storage, "2", "file", at), fence, strings.Repeat("9", 32), at); !errors.Is(err, ErrConflict) {
		t.Fatal("replaced an entry that was not current", err)
	}
	if err := s.PublishHTTPCacheEntry(httpEntry(storage, "2", "file", at), fence, first.ID, at); err != nil {
		t.Fatal(err)
	}
	current, err := s.CurrentHTTPCacheEntry(storage, "file")
	if err != nil || current.ID != strings.Repeat("2", 32) || !current.Current {
		t.Fatal(current, err)
	}
	old, err := s.HTTPCacheEntry(first.ID)
	if err != nil || old.Current {
		t.Fatal("replaced entry still current", old, err)
	}
	if err = s.RevalidateHTTPCacheEntry(first.ID, storage, fence, at, at, []byte(`{}`)); !errors.Is(err, ErrConflict) {
		t.Fatal("revalidated a retired entry", err)
	}
	later := at.Add(time.Hour)
	if err = s.RevalidateHTTPCacheEntry(current.ID, storage, fence, later, later.Add(time.Minute), []byte(`{"ETag":["\"v2\""]}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.TouchHTTPCacheEntry(current.ID, 120); err != nil {
		t.Fatal(err)
	}
	if err = s.TouchHTTPCacheEntry(current.ID, 60); err != nil {
		t.Fatal(err)
	}
	current, _ = s.CurrentHTTPCacheEntry(storage, "file")
	if !current.ValidatedAt.Equal(later) || current.AccessBucket != 120 || string(current.Headers) != `{"ETag":["\"v2\""]}` {
		t.Fatal("validation or access bucket", current)
	}
	if deleted, err := s.DeleteRetiredHTTPCacheEntry(current.ID); deleted || err != nil {
		t.Fatal("deleted a current entry", deleted, err)
	}
	if deleted, err := s.DeleteRetiredHTTPCacheEntry(first.ID); !deleted || err != nil {
		t.Fatal(deleted, err)
	}
	if err = s.TouchHTTPCacheEntry(first.ID, 180); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("touched a deleted entry", err)
	}
}

func TestHTTPPreviewBatchRetiresOnlyUnchangedEntries(t *testing.T) {
	s, storage, fence := httpCacheFixture(t)
	at := time.Unix(1_700_000_000, 0).UTC()
	for i, path := range []string{"a", "b", "c"} {
		if err := s.PublishHTTPCacheEntry(httpEntry(storage, string(rune('1'+i)), path, at), fence, "", at); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := s.CreateHTTPPreview(t.Context(), HTTPPreview{ID: strings.Repeat("f", 32), StorageID: storage, Kind: "cleanup", Fence: fence, CreatedAt: at, ExpiresAt: at.Add(time.Minute), Criteria: []byte(`{}`)}, CleanupPreviewFence)
	if err != nil || preview.HighWater != 3 {
		t.Fatal(preview, err)
	}
	// An entry published after the preview started is beyond its high water.
	if err = s.PublishHTTPCacheEntry(httpEntry(storage, "4", "d", at), fence, "", at); err != nil {
		t.Fatal(err)
	}
	page, err := s.FreezeHTTPPreviewPage(t.Context(), preview, CleanupPreviewFence, 0, 10, func(e HTTPCacheEntry) (HTTPPreviewItem, bool, bool) {
		return HTTPPreviewItem{GenerationID: e.ID, Path: e.Path, SizeBytes: e.SizeBytes, AccessBucket: e.AccessBucket, Basis: "last_access", Match: []byte(`{}`), RuleIndex: -1}, true, false
	})
	if err != nil || page.Scanned != 3 || page.Selected != 3 || page.Last != 3 {
		t.Fatal(page, err)
	}
	if err = s.FinishHTTPPreviewBuild(t.Context(), preview, CleanupPreviewFence); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimHTTPPreview(t.Context(), storage, preview.ID, func(p HTTPPreview) (HTTPPreviewDecision, error) {
		return HTTPPreviewDecision{Apply: p.State == "ready", Fence: CleanupPreviewFence}, nil
	})
	if err != nil || claimed.State != "running" || claimed.SelectedFiles != 3 || claimed.SelectedBytes != 9 {
		t.Fatal(claimed, err)
	}
	// Between preview and execution "a" is accessed and "b" is replaced.
	if err = s.TouchHTTPCacheEntry(strings.Repeat("1", 32), 60); err != nil {
		t.Fatal(err)
	}
	if err = s.PublishHTTPCacheEntry(httpEntry(storage, "5", "b", at), fence, strings.Repeat("2", 32), at); err != nil {
		t.Fatal(err)
	}
	items, err := s.HTTPPreviewItems(t.Context(), preview.ID, 0, 10, true)
	if err != nil || len(items) != 3 {
		t.Fatal(items, err)
	}
	result, err := s.RetireHTTPPreviewItems(t.Context(), claimed, CleanupPreviewFence, items, at)
	if err != nil || len(result.Retired) != 1 || result.Retired[0] != strings.Repeat("3", 32) || result.RetiredBytes != 3 || result.SkippedAccessed != 1 || result.SkippedChanged != 1 {
		t.Fatal(result, err)
	}
	if pending, err := s.HTTPPreviewItems(t.Context(), preview.ID, 0, 10, true); err != nil || len(pending) != 0 {
		t.Fatal("outcomes not recorded", pending, err)
	}
	if p, err := s.HTTPPreview(preview.ID); err != nil || p.CompletedFiles != 3 {
		t.Fatal(p, err)
	}
	if current, err := s.HTTPCacheEntries(storage); err != nil || len(current) != 3 {
		t.Fatal(current, err)
	}
}
