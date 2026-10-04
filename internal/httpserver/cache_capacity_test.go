package httpserver

import (
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
)

func TestManualRefreshReportsTransferCapacity(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	h.createVendor("enterprise")
	app := h.createApp("enterprise", "files", application.GeneralHTTP, map[string]any{"base_url": "http://127.0.0.1:1"})
	if err := h.server.Downloads.ConfigureLimits(1, 1, 1<<20); err != nil {
		t.Fatal(err)
	}
	release, err := h.server.Downloads.AcquireHTTPReader()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	body, _ := h.request("POST", "/admin/api/apps/"+app.Key+"/cache/refresh", map[string]any{"path": "/file.bin"}, 503, nil)
	if got := directoryDecode[struct {
		Code string `json:"code"`
	}](t, body, "error").Code; got != "TRANSFER_CAPACITY" {
		t.Fatalf("capacity error must remain distinct from an invalid preview: %s", body)
	}
}
