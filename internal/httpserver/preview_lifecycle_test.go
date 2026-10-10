package httpserver

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/pathmatch"
)

// previewKindCase drives one kind of frozen preview through the HTTP API.
type previewKindCase struct {
	name string
	// fixture returns a harness, the application key and the API base of the
	// kind (".../version-cleanup", ".../retention", ".../cache/refresh" or
	// ".../cache/cleanup").
	fixture func(t *testing.T) (*harness, string, string)
	// build creates a preview and returns its ID.
	build func(h *harness, key, base string) string
	// execute is the success status of execute: 202 for the background refresh.
	execute int
	// items is the item listing: "cursor", "page" or "" (embedded in the preview).
	items string
	// readable reports whether GET {base}/{id} exists.
	readable bool
	// disabled and deleted are the errors of executing a preview after the
	// application was disabled or deleted.
	disabled, deleted errorCode
}

func releasePreviewFixture(t *testing.T) (*harness, string, string) {
	h, _ := retentionFixture(t, t.TempDir())
	setRetention(t, h, 1, false)
	return h, "retention/binary", "/admin/api/apps/retention/binary"
}

func cachePreviewFixture(t *testing.T) (*harness, string, string) {
	upstream := newCacheFixture(t)
	h, app, api := policyHTTPApp(t, upstream.server.URL)
	for i := range 5 {
		h.request("GET", fmt.Sprintf("/%s/files/%d.bin", app.Key, i), nil, 200, nil)
	}
	ageCache(t, h, app.StorageID(), 2*time.Hour)
	return h, app.Key, api
}

func previewID(t *testing.T, body []byte) string {
	t.Helper()
	return decodeJSONBody[struct {
		ID string `json:"id"`
	}](t, body).ID
}

var previewKindCases = []previewKindCase{{
	name:    "version_cleanup",
	fixture: releasePreviewFixture,
	build: func(h *harness, _, base string) string {
		body, _ := h.request("POST", base+"/version-cleanup/preview", map[string]any{"minimum_version": "10.0.0"}, 201, nil)
		return previewID(h.t, body)
	},
	execute: 200, disabled: codePreviewStale, deleted: codeEntityDeleted,
}, {
	name:    "retention",
	fixture: releasePreviewFixture,
	build: func(h *harness, key, base string) string {
		body, _ := h.request("POST", base+"/retention/preview", nil, 201, ifMatchHeader(h.appRevision(key)))
		return previewID(h.t, body)
	},
	execute: 200, items: "page", readable: true, disabled: codeApplicationDisabled, deleted: codeEntityDeleted,
}, {
	name:    "cache_refresh",
	fixture: cachePreviewFixture,
	build: func(h *harness, _, base string) string {
		body, _ := h.request("POST", base+"/cache/refresh/preview", map[string]any{"match": pathmatch.Spec{Type: "glob", Pattern: "/"}}, 201, nil)
		return previewID(h.t, body)
	},
	execute: 202, items: "cursor", readable: true, disabled: codeApplicationDisabled, deleted: codePreviewStale,
}, {
	name:    "cache_cleanup",
	fixture: cachePreviewFixture,
	build: func(h *harness, _, base string) string {
		body, _ := h.request("POST", base+"/cache/cleanup/preview", map[string]any{"basis": "fetched_at", "before": time.Now().Add(-time.Hour).UTC()}, 201, nil)
		return previewID(h.t, body)
	},
	execute: 200, items: "cursor", readable: true, disabled: codePreviewStale, deleted: codePreviewStale,
}}

// path is the API base of the kind's previews.
func (c previewKindCase) path(base string) string {
	switch c.name {
	case "version_cleanup":
		return base + "/version-cleanup"
	case "retention":
		return base + "/retention"
	case "cache_refresh":
		return base + "/cache/refresh"
	}
	return base + "/cache/cleanup"
}

// itemKeys pages through the items of a preview one at a time and returns
// each item's identity and outcome.
func (c previewKindCase) itemKeys(h *harness, root, id string) []string {
	h.t.Helper()
	keys := []string{}
	switch c.items {
	case "page":
		for page := 1; ; page++ {
			body, _ := h.request("GET", fmt.Sprintf("%s/%s/items?page=%d&limit=1", root, id, page), nil, 200, nil)
			items := decodeJSONBody[pageDTO[retentionVersionDTO]](h.t, body)
			for _, item := range items.Items {
				keys = append(keys, fmt.Sprint(item.Version, item.Selected))
			}
			if page >= items.TotalPages {
				return keys
			}
		}
	case "cursor":
		path := root + "/" + id + "/items?limit=1"
		for {
			body, _ := h.request("GET", path, nil, 200, nil)
			page := decodeJSONBody[maintenanceItemPageDTO](h.t, body)
			for _, item := range page.Items {
				keys = append(keys, fmt.Sprint(item.Ordinal, item.Path, item.GenerationID))
			}
			if page.NextCursor == nil {
				return keys
			}
			path = root + "/" + id + "/items?limit=1&cursor=" + *page.NextCursor
		}
	}
	return keys
}

// settle waits until a background execution finished and returns the preview.
func (c previewKindCase) settle(h *harness, root, id string) []byte {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		body, _ := h.request("GET", root+"/"+id, nil, 200, nil)
		var state struct {
			State string `json:"state"`
		}
		_ = json.Unmarshal(body, &state)
		if state.State == "done" {
			return body
		}
		if state.State == "failed" || time.Now().After(deadline) {
			h.t.Fatalf("execution did not finish: %s", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEveryPreviewKindSharesOneLifecycle(t *testing.T) {
	for _, c := range previewKindCases {
		t.Run(c.name, func(t *testing.T) {
			h, key, base := c.fixture(t)
			root := c.path(base)

			// Preview, review the frozen items page by page, execute.
			id := c.build(h, key, base)
			if c.readable {
				h.request("GET", root+"/"+id, nil, 200, nil)
			}
			reviewed := c.itemKeys(h, root, id)
			if c.items != "" && len(reviewed) == 0 {
				t.Fatal("preview froze no items")
			}
			// Concurrent executions run the frozen set once; the others repeat
			// its receipt or report that it is still running.
			var wg sync.WaitGroup
			statuses := make([]int, 4)
			bodies := make([][]byte, 4)
			for i := range statuses {
				wg.Add(1)
				go func() {
					defer wg.Done()
					statuses[i], bodies[i], _ = h.raw("POST", root+"/"+id+"/execute", nil, "", nil)
				}()
			}
			wg.Wait()
			succeeded := 0
			for i, status := range statuses {
				switch {
				case status == c.execute:
					succeeded++
				case status == 409 && errorCodeOf(t, bodies[i]) == string(codeOperationInProgress):
				default:
					t.Fatalf("concurrent execution: %d %s", status, bodies[i])
				}
			}
			if succeeded == 0 {
				t.Fatal("no execution succeeded", statuses)
			}
			var receipt []byte
			if c.execute == 202 {
				receipt = c.settle(h, root, id)
			} else {
				receipt, _ = h.request("POST", root+"/"+id+"/execute", nil, 200, nil)
			}
			for i, status := range statuses {
				if c.execute == 200 && status == 200 && string(bodies[i]) != string(receipt) {
					t.Fatalf("concurrent executions returned different receipts:\n%s\n%s", receipt, bodies[i])
				}
			}
			if again, _ := h.request("POST", root+"/"+id+"/execute", nil, c.execute, nil); string(again) != string(receipt) {
				t.Fatalf("repeated execution changed the receipt:\n%s\n%s", receipt, again)
			}
			if c.readable {
				if got, _ := h.request("GET", root+"/"+id, nil, 200, nil); string(got) != string(receipt) {
					t.Fatalf("receipt not readable:\n%s\n%s", receipt, got)
				}
			}
			executed := c.itemKeys(h, root, id)
			if fmt.Sprint(executed) != fmt.Sprint(reviewed) {
				t.Fatal("items moved during execution", reviewed, executed)
			}

			// An expired preview is gone.
			expired := c.build(h, key, base)
			if _, err := h.sql().Exec(`UPDATE previews SET expires_at_s=? WHERE id=?`, time.Now().Add(-time.Second).Unix(), expired); err != nil {
				t.Fatal(err)
			}
			h.expectError("POST", root+"/"+expired+"/execute", nil, 404, codePreviewNotFound, nil)
			if c.readable {
				h.expectError("GET", root+"/"+expired, nil, 404, codePreviewNotFound, nil)
			}

			// Disabling the application, and enabling it again, fences the preview.
			fenced := c.build(h, key, base)
			h.setAppEnabled(key, false)
			h.expectError("POST", root+"/"+fenced+"/execute", nil, 409, c.disabled, nil)
			h.setAppEnabled(key, true)
			h.expectError("POST", root+"/"+fenced+"/execute", nil, 409, codePreviewStale, nil)
			if c.readable {
				h.request("GET", root+"/"+fenced, nil, 200, nil)
			}

			// A deleted application executes nothing.
			deleted := c.build(h, key, base)
			h.markDeleted(key)
			h.expectError("POST", root+"/"+deleted+"/execute", nil, 409, c.deleted, nil)
		})
	}
}
