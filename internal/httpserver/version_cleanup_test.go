package httpserver

import (
	"strings"
	"testing"
)

func TestVersionCleanupPreviewExecuteAndIdempotency(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	key := cachedReleases(t, h, "1.0.0", "1.10.0", "2.0.0")
	base := "/admin/api/apps/" + key + "/version-cleanup"
	data, headers := h.request("POST", base+"/preview", map[string]any{"minimum_version": "2.0.0"}, 201, nil)
	preview := decodeJSONBody[versionCleanupDTO](t, data)
	entry, _ := h.server.registry.LookupAny(key)
	if preview.SourceEpoch != entry.SourceEpoch || preview.ExecutedAt != nil || len(preview.Selected) != 2 || len(preview.UnknownVersions) != 0 || preview.ActiveGenerations != 0 || headers.Get("ETag") != "" {
		t.Fatal("preview document", string(data))
	}
	want := int64(len("binary:1.0.0") + len("binary:1.10.0"))
	if preview.LogicalBytes != want || preview.ReclaimableBytes != want || !preview.ExpiresAt.After(preview.CreatedAt) {
		t.Fatal("preview sizes", string(data))
	}
	for _, item := range preview.Selected {
		if item.Version == "2.0.0" || item.ResourceKey != "asset.tgz" || item.GenerationID == "" {
			t.Fatal("selected", item)
		}
	}
	other := h.createApp("fixture", "other", "codex", map[string]any{"base_url": fixtureUpstream})
	h.expectError("POST", "/admin/api/apps/"+other.Key+"/version-cleanup/"+preview.ID+"/execute", nil, 404, codePreviewNotFound, nil)
	data, _ = h.request("POST", base+"/"+preview.ID+"/execute", nil, 200, nil)
	executed := decodeJSONBody[versionCleanupDTO](t, data)
	if executed.ID != preview.ID || executed.ExecutedAt == nil || executed.LogicalBytes != want || executed.ReclaimableBytes != want || len(executed.Selected) != 2 {
		t.Fatal("executed preview", string(data))
	}
	if again, _ := h.request("POST", base+"/"+preview.ID+"/execute", nil, 200, nil); string(again) != string(data) {
		t.Fatal("repeated execution changed the result", string(again))
	}
	data, _ = h.request("GET", "/admin/api/apps/"+key+"/resources", nil, 200, nil)
	for _, item := range decodeJSONBody[cursorPage[resourceDTO]](t, data).Items {
		if item.Version != "2.0.0" && item.Current {
			t.Fatal("cleaned version still current", item)
		}
	}
	data, _ = h.request("GET", "/admin/api/apps/"+key+"/versions", nil, 200, nil)
	if names := versionNames(decodeJSONBody[cursorPage[versionDTO]](t, data).Items); names != "2.0.0,1.10.0,1.0.0" {
		t.Fatal("version history must be kept", names)
	}
}

func TestVersionCleanupRejectsInvalidInputAndStalePreviews(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	key := cachedReleases(t, h, "1.0.0", "2.0.0")
	base := "/admin/api/apps/" + key + "/version-cleanup"
	for _, input := range []map[string]any{{"minimum_version": "v2"}, {"minimum_version": ""}, {"minimum_version": "2.0.0", "source_epoch": 0}} {
		h.expectError("POST", base+"/preview", input, 400, codeValidationFailed, nil)
	}
	h.expectError("POST", base+"/preview", map[string]any{"minimum_version": "2.0.0", "unknown": true}, 400, codeInvalidRequest, nil)
	h.expectError("POST", base+"/preview", map[string]any{"minimum_version": "2.0.0", "source_epoch": 99}, 404, codeSourceNotFound, nil)
	if status, data, _ := h.raw("POST", base+"/preview", strings.NewReader(`{"minimum_version":"2.0.0"}`), "text/plain", nil); status != 415 || errorCodeOf(t, data) != string(codeUnsupportedMediaType) {
		t.Fatal(status, string(data))
	}
	h.expectError("POST", base+"/"+strings.Repeat("a", 32)+"/execute", nil, 404, codePreviewNotFound, nil)
	h.expectError("POST", base+"/not-an-id/execute", nil, 400, codeInvalidPath, nil)

	expired := decodeJSONBody[versionCleanupDTO](t, mustBody(h.request("POST", base+"/preview", map[string]any{"minimum_version": "2.0.0"}, 201, nil)))
	if _, err := h.sql().Exec(`UPDATE cleanup_previews SET expires_at_s=0 WHERE id=?`, expired.ID); err != nil {
		t.Fatal(err)
	}
	h.expectError("POST", base+"/"+expired.ID+"/execute", nil, 404, codePreviewNotFound, nil)

	stale := decodeJSONBody[versionCleanupDTO](t, mustBody(h.request("POST", base+"/preview", map[string]any{"minimum_version": "2.0.0"}, 201, nil)))
	h.patchApp(key, map[string]any{"base_url": fixtureUpstream + "/mirror"})
	h.expectError("POST", base+"/"+stale.ID+"/execute", nil, 409, codePreviewStale, nil)

	// The previous source epoch can still be cleaned explicitly.
	old := decodeJSONBody[versionCleanupDTO](t, mustBody(h.request("POST", base+"/preview", map[string]any{"minimum_version": "2.0.0", "source_epoch": stale.SourceEpoch}, 201, nil)))
	if old.SourceEpoch != stale.SourceEpoch || len(old.Selected) != 1 || old.Selected[0].Version != "1.0.0" {
		t.Fatal("old epoch preview", old)
	}
	h.request("POST", base+"/"+old.ID+"/execute", nil, 200, nil)

	h.markDeleted(key)
	h.expectError("POST", base+"/preview", map[string]any{"minimum_version": "2.0.0"}, 409, codeEntityDeleted, nil)
	h.expectError("POST", base+"/"+old.ID+"/execute", nil, 409, codeEntityDeleted, nil)
	h.request("GET", "/admin/api/apps/"+key+"/versions", nil, 200, nil)
}

func mustBody(data []byte, _ any) []byte { return data }
