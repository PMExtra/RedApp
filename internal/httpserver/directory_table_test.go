package httpserver

import (
	"encoding/json"
	"fmt"
	"github.com/PMExtra/RedApp/internal/store"
	"testing"
)

func TestVendorApplicationTableFiltersSortsBeforePagingAndUsesRecordedStatistics(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	h.request("GET", "/admin/api/vendors/openai/apps?view=table", nil, 401, nil)
	h.login(h.password)
	for i := 0; i < 23; i++ {
		a, err := h.server.DB.CreateApplication("openai", store.ApplicationInput{ID: fmt.Sprintf("tool-%02d", i), Name: store.LocalizedText{En: fmt.Sprintf("Tool %02d", i), ZhCN: fmt.Sprintf("工具 %02d", 22-i)}, Provider: "codex", BaseURL: "https://example.com/releases/", CacheTTLSeconds: 1, Enabled: i%2 == 0})
		if err != nil {
			t.Fatal(err)
		}
		for v, at := range map[string]int64{"1.9.0": 300, "1.10.0": int64(100 + i), "invalid": 900} {
			if _, err = h.server.DB.DB.Exec("INSERT INTO app_versions(app_id,version,first_seen_s) VALUES(?,?,?)", a.StorageID(), v, at); err != nil {
				t.Fatal(err)
			}
		}
		if err = h.server.DB.AddFor(a.MetricsID(), "download_success", int64(i)); err != nil {
			t.Fatal(err)
		}
	}
	_, err := h.server.DB.CreateApplication("openai", store.ApplicationInput{ID: "info", Name: store.LocalizedText{En: "Info", ZhCN: "介绍"}, Provider: "info"})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.server.ReloadDirectory(); err != nil {
		t.Fatal(err)
	}
	get := func(query string) store.Page[applicationTableRow] {
		t.Helper()
		data, _ := h.request("GET", "/admin/api/vendors/openai/apps?view=table&"+query, nil, 200, nil)
		var page store.Page[applicationTableRow]
		if err := json.Unmarshal(data, &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	p := get("q=tool&sort=downloads&order=desc&limit=2&page=2")
	if p.Total != 23 || p.Items[0].ID != "tool-20" || p.Items[1].ID != "tool-19" || p.Items[1].Enabled {
		t.Fatalf("%+v", p)
	}
	if p.Items[0].LatestVersion != "1.10.0" || p.Items[0].VersionDiscoveredAt.Unix() != 120 || *p.Items[0].SuccessfulDownloads != 20 {
		t.Fatal(p.Items[0])
	}
	p = get("q=tool&sort=updated&order=desc&limit=2")
	if p.Items[0].ID != "tool-22" {
		t.Fatal(p)
	}
	p = get("q=tool&sort=name&lang=zh-CN&limit=2")
	if p.Items[0].ID != "tool-22" {
		t.Fatal(p)
	}
	p = get("q=tool&sort=downloads&order=asc&limit=2")
	if p.Items[0].SuccessfulDownloads == nil || *p.Items[0].SuccessfulDownloads != 0 {
		t.Fatal(p)
	}
	p = get("q=tool-22&page=99&limit=2")
	if p.Total != 1 || p.Page != 1 || p.Items[0].ID != "tool-22" {
		t.Fatal(p)
	}
	p = get("q=absent&limit=2")
	if p.Total != 0 || len(p.Items) != 0 {
		t.Fatal(p)
	}
	p = get("q=info")
	if len(p.Items) != 1 || p.Items[0].LatestVersion != "" || p.Items[0].VersionDiscoveredAt != nil || p.Items[0].SuccessfulDownloads != nil {
		t.Fatal(p)
	}
	p = get("q=tool&state=disabled&limit=100")
	if p.Total != 11 {
		t.Fatal(p.Total)
	}
	a, _ := h.server.DB.Application("openai/tool-22")
	if _, err = h.server.DB.DB.Exec("INSERT INTO app_versions(app_id,version,first_seen_s) VALUES(?,?,0)", a.StorageID(), "10.0.0"); err != nil {
		t.Fatal(err)
	}
	p = get("q=tool&sort=version&order=desc&limit=1")
	if p.Items[0].ID != "tool-22" || p.Items[0].VersionDiscoveredAt != nil {
		t.Fatal(p)
	}
	p = get("q=tool&sort=updated&order=asc&limit=100")
	if p.Items[len(p.Items)-1].ID != "tool-22" {
		t.Fatal("missing time must sort last", p)
	}

	if _, err = h.server.DB.DB.Exec("UPDATE applications SET source_epoch=source_epoch+1 WHERE uid=?", a.UID); err != nil {
		t.Fatal(err)
	}
	if err = h.server.ReloadDirectory(); err != nil {
		t.Fatal(err)
	}
	p = get("q=tool-22")
	if p.Items[0].LatestVersion != "" || p.Items[0].VersionDiscoveredAt != nil || *p.Items[0].SuccessfulDownloads != 22 {
		t.Fatal("epoch isolation", p)
	}
	for _, q := range []string{"sort=bad", "order=bad", "lang=fr", "view=wrong"} {
		h.request("GET", "/admin/api/vendors/openai/apps?view=table&"+q, nil, 400, nil)
	}
	h.request("GET", "/admin/api/vendors/missing/apps?view=table", nil, 404, nil)
}
