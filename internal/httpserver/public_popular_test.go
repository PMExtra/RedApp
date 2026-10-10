package httpserver

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPublicPopularCandidatesNeverExposeDisabledOrDeletedRecords(t *testing.T) {
	h := newHarness(t)
	a, _ := h.server.store.Application("openai/codex")
	b, _ := h.server.store.Application("anthropic/claude-code")
	for _, uid := range []string{a.UID, b.UID} {
		if err := h.server.store.RecordDownload(uid, "192.0.2.10", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want int) {
		t.Helper()
		data, _ := h.request("GET", "/api/home", nil, 200, nil)
		var body struct {
			Ranking []struct {
				App struct {
					Key string `json:"key"`
				} `json:"app"`
				DownloadClients int64 `json:"download_clients"`
			} `json:"ranking"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Ranking) != want {
			t.Fatal(string(data))
		}
		if want == 1 && body.Ranking[0].App.Key != b.Key || body.Ranking[0].DownloadClients != 1 {
			t.Fatal("hidden application exposed", string(data))
		}
	}
	check(2)
	for _, query := range []string{
		"UPDATE applications SET enabled=0 WHERE uid=?",
		"UPDATE applications SET enabled=1,deleted_at_s=1 WHERE uid=?",
	} {
		if _, err := h.server.store.DB.Exec(query, a.UID); err != nil {
			t.Fatal(err)
		}
		if err := h.server.ReloadDirectory(); err != nil {
			t.Fatal(err)
		}
		check(1)
	}
	if _, err := h.server.store.DB.Exec("UPDATE applications SET enabled=1,deleted_at_s=NULL WHERE uid=?", a.UID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.server.store.DB.Exec("UPDATE vendors SET enabled=0 WHERE uid=?", a.VendorUID); err != nil {
		t.Fatal(err)
	}
	if err := h.server.ReloadDirectory(); err != nil {
		t.Fatal(err)
	}
	check(1)
	if _, err := h.server.store.DB.Exec("UPDATE vendors SET enabled=1,deleted_at_s=1 WHERE uid=?", a.VendorUID); err != nil {
		t.Fatal(err)
	}
	if err := h.server.ReloadDirectory(); err != nil {
		t.Fatal(err)
	}
	check(1)
}
