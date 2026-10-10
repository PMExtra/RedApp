package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/identity"
)

// releasePreview freezes generations, grouped by version, as a ready release
// preview of kind at.
func releasePreview(t *testing.T, s *Store, storage string, kind PreviewKind, at time.Time, generations ...CleanupSelection) Preview {
	t.Helper()
	byVersion := map[string][]CleanupSelection{}
	for _, g := range generations {
		byVersion[g.Version] = append(byVersion[g.Version], g)
	}
	versions := make([]string, 0, len(byVersion))
	for v := range byVersion {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	items := []PreviewItem{}
	for i, v := range versions {
		detail, _ := json.Marshal(ReleaseItemDetail{Generations: byVersion[v]})
		items = append(items, PreviewItem{Ordinal: int64(i + 1), Label: v, Selected: true, Detail: detail})
	}
	p, err := s.CreatePreview(t.Context(), newPreview(t, storage, fenceOf(t, s, storage), kind, at), items)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func newPreview(t *testing.T, storage string, fence SourceFence, kind PreviewKind, at time.Time) Preview {
	t.Helper()
	id, err := fsutil.RandomID()
	if err != nil {
		t.Fatal(err)
	}
	uid, epoch, _ := identity.ParseStorageID(storage)
	return Preview{ID: id, Kind: kind, AppUID: uid, SourceEpoch: epoch, Fence: fence, State: PreviewReady, CreatedAt: at, ExpiresAt: at.Add(PreviewLifetime)}
}

// listedPreview creates a ready refresh preview of count listed items.
func listedPreview(t *testing.T, s *Store, storage string, fence SourceFence, at time.Time, count int) Preview {
	t.Helper()
	items := make([]PreviewItem, 0, count)
	for i := 1; i <= count; i++ {
		items = append(items, PreviewItem{Ordinal: int64(i * 10), Ref: fmt.Sprintf("%032d", i), Label: fmt.Sprintf("file-%d", i), SizeBytes: 2, Selected: true})
	}
	p, err := s.CreatePreview(t.Context(), newPreview(t, storage, fence, PreviewCacheRefresh, at), items)
	if err != nil || p.SelectedItems != count || p.SelectedBytes != int64(2*count) {
		t.Fatal(p, err)
	}
	return p
}

func ordinals(items []PreviewItem) []int64 {
	out := []int64{}
	for _, item := range items {
		out = append(out, item.Ordinal)
	}
	return out
}

func TestPreviewExecutionIsClaimedOnceAndRepeatsItsReceipt(t *testing.T) {
	s, storage, fence := httpCacheFixture(t)
	at := time.Unix(1_700_000_000, 0).UTC()
	p := listedPreview(t, s, storage, fence, at, 3)
	var wg sync.WaitGroup
	var mu sync.Mutex
	claims, running := 0, 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, claimed, err := s.ClaimPreview(t.Context(), storage, PreviewCacheRefresh, p.ID, at)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case claimed && err == nil:
				claims++
			case errors.Is(err, ErrPreviewRunning):
				running++
			default:
				t.Error("unexpected claim result", claimed, err)
			}
		}()
	}
	wg.Wait()
	if claims != 1 || running != 7 {
		t.Fatal("concurrent executions were not claimed exactly once", claims, running)
	}
	claimed, _ := s.Preview(p.AppUID, PreviewCacheRefresh, p.ID, at)
	if err := s.RecordPreviewItems(t.Context(), claimed, []PreviewOutcome{{Ordinal: 10, Status: "refreshed"}, {Ordinal: 20, Status: "failed", ErrorCode: "refresh_failed"}, {Ordinal: 10, Status: "failed"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishPreview(t.Context(), claimed, PreviewDone, json.RawMessage(`{"first":true}`), at); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishPreview(t.Context(), claimed, PreviewFailed, json.RawMessage(`{"second":true}`), at); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordPreviewItems(t.Context(), claimed, []PreviewOutcome{{Ordinal: 30, Status: "refreshed"}}); !errors.Is(err, ErrConflict) {
		t.Fatal("a finished preview accepted outcomes", err)
	}
	done, started, err := s.ClaimPreview(t.Context(), storage, PreviewCacheRefresh, p.ID, at)
	if err != nil || started || done.State != PreviewDone || string(done.Result) != `{"first":true}` || done.CompletedItems != 2 || done.FailedItems != 1 {
		t.Fatal("finished preview did not repeat its first receipt", done, started, err)
	}
	items, err := s.PreviewItems(t.Context(), p.ID, 0, 10, false)
	if err != nil || len(items) != 3 || items[0].Status != "refreshed" || items[1].ErrorCode != "refresh_failed" || items[2].Status != "pending" {
		t.Fatal("item outcomes", items, err)
	}
}

func TestPreviewFenceKindOwnerAndExpiry(t *testing.T) {
	s, storage, fence := httpCacheFixture(t)
	at := time.Now().UTC().Truncate(time.Second)
	p := listedPreview(t, s, storage, fence, at, 1)
	other, err := s.CreateVendor(directoryVendor("other"))
	if err != nil {
		t.Fatal(err)
	}
	otherApp, err := s.CreateApplication(other.ID, directoryApplication("other"))
	if err != nil {
		t.Fatal(err)
	}
	for _, lookup := range []struct {
		uid  string
		kind PreviewKind
	}{{otherApp.UID, PreviewCacheRefresh}, {p.AppUID, PreviewCacheCleanup}} {
		if _, err = s.Preview(lookup.uid, lookup.kind, p.ID, at); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign or other-kind preview was found", lookup, err)
		}
	}
	if _, _, err = s.ClaimPreview(t.Context(), otherApp.StorageID(), PreviewCacheRefresh, p.ID, at); !errors.Is(err, ErrNotFound) {
		t.Fatal("another application executed the preview", err)
	}
	// A changed runtime revision fences the preview; it stays ready.
	if _, err = s.db.Exec(`UPDATE applications SET runtime_revision=runtime_revision+1 WHERE uid=?`, p.AppUID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.ClaimPreview(t.Context(), storage, PreviewCacheRefresh, p.ID, at); !errors.Is(err, ErrPreviewStale) {
		t.Fatal("stale preview executed", err)
	}
	if still, err := s.Preview(p.AppUID, PreviewCacheRefresh, p.ID, at); err != nil || still.State != PreviewReady {
		t.Fatal("rejected execution changed the preview", still, err)
	}
	if _, err = s.CreatePreview(t.Context(), newPreview(t, storage, fence, PreviewCacheRefresh, at), nil); !errors.Is(err, ErrPreviewStale) {
		t.Fatal("preview created under a stale fence", err)
	}
	if _, err = s.Preview(p.AppUID, PreviewCacheRefresh, p.ID, p.ExpiresAt); !errors.Is(err, ErrExpired) {
		t.Fatal("preview readable at its expiry", err)
	}
	if _, _, err = s.ClaimPreview(t.Context(), storage, PreviewCacheRefresh, p.ID, p.ExpiresAt); !errors.Is(err, ErrExpired) {
		t.Fatal("expired preview executed", err)
	}
	if err = s.PrunePreviews(t.Context(), p.ExpiresAt, 4); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Preview(p.AppUID, PreviewCacheRefresh, p.ID, at); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired preview was not pruned", err)
	}
}

func TestPreviewItemPagesAreStableAcrossExecution(t *testing.T) {
	s, storage, fence := httpCacheFixture(t)
	at := time.Now().UTC().Truncate(time.Second)
	p := listedPreview(t, s, storage, fence, at, 5)
	pages := func() [][]int64 {
		out := [][]int64{}
		for after := int64(0); ; {
			items, err := s.PreviewItems(t.Context(), p.ID, after, 2, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) == 0 {
				return out
			}
			out = append(out, ordinals(items))
			after = items[len(items)-1].Ordinal
		}
	}
	before := fmt.Sprint(pages())
	if before != "[[10 20] [30 40] [50]]" {
		t.Fatal("pages", before)
	}
	claimed, _, err := s.ClaimPreview(t.Context(), storage, PreviewCacheRefresh, p.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RecordPreviewItems(t.Context(), claimed, []PreviewOutcome{{Ordinal: 30, Status: "refreshed"}}); err != nil {
		t.Fatal(err)
	}
	if after := fmt.Sprint(pages()); after != before {
		t.Fatal("recording outcomes moved items between pages", after)
	}
	pending, err := s.PreviewItems(t.Context(), p.ID, 0, 10, true)
	if err != nil || fmt.Sprint(ordinals(pending)) != "[10 20 40 50]" {
		t.Fatal("pending items", pending, err)
	}
}

func TestInterruptedPreviewsBecomeReceiptsAndDeletedApplicationsDropThem(t *testing.T) {
	s, storage, fence := httpCacheFixture(t)
	at := time.Now().UTC().Truncate(time.Second)
	running := listedPreview(t, s, storage, fence, at, 2)
	if _, _, err := s.ClaimPreview(t.Context(), storage, PreviewCacheRefresh, running.ID, at); err != nil {
		t.Fatal(err)
	}
	building, err := s.CreatePreview(t.Context(), func() Preview {
		p := newPreview(t, storage, fence, PreviewCacheCleanup, at)
		p.State = PreviewBuilding
		return p
	}(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.InterruptPreviews(t.Context(), at, PreviewCacheRefresh); err != nil {
		t.Fatal(err)
	}
	if p, err := s.Preview(running.AppUID, PreviewCacheRefresh, running.ID, at); err != nil || p.State != PreviewFailed || !json.Valid(p.Result) || string(p.Result) == "" {
		t.Fatal("running execution not interrupted", p, err)
	}
	if p, err := s.Preview(building.AppUID, PreviewCacheCleanup, building.ID, at); err != nil || p.State != PreviewBuilding {
		t.Fatal("another kind was interrupted", p, err)
	}
	a, err := s.Application("vendor/app")
	if err != nil || a.UID != running.AppUID {
		t.Fatal(a, err)
	}
	if err = s.PermanentlyDeleteApplication(a.Key, a.Revision); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.read.QueryRow(`SELECT (SELECT count(*) FROM previews)+(SELECT count(*) FROM preview_items)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("previews of a permanently deleted application remain", count, err)
	}
}
