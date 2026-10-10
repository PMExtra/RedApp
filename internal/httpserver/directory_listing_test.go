package httpserver

import (
	"fmt"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/store"
)

func TestApplicationListSortsAllMatchesBeforePaging(t *testing.T) {
	h := newHarness(t)
	h.expectError("GET", "/admin/api/apps?vendor=openai", nil, 401, codeAuthRequired, nil)
	h.login("")
	for i := 0; i < 23; i++ {
		a, err := h.store.CreateApplication("openai", store.ApplicationInput{ID: fmt.Sprintf("tool-%02d", i), Name: store.LocalizedText{En: fmt.Sprintf("Tool %02d", i), ZhCN: fmt.Sprintf("工具 %02d", 22-i)}, Provider: "codex", BaseURL: "https://example.com/releases/", CacheTTLSeconds: 1, Enabled: i%2 == 0})
		if err != nil {
			t.Fatal(err)
		}
		for v, at := range map[string]int64{"1.9.0": 300, "1.10.0": int64(100 + i), "invalid": 900} {
			if _, err = h.store.DB.Exec("INSERT INTO app_versions(app_id,version,first_seen_s) VALUES(?,?,?)", a.StorageID(), v, at); err != nil {
				t.Fatal(err)
			}
		}
		if err = h.store.AddFor(a.MetricsID(), "download_success", int64(i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.store.CreateApplication("openai", store.ApplicationInput{ID: "info", Name: store.LocalizedText{En: "Info", ZhCN: "介绍"}, Provider: "info"}); err != nil {
		t.Fatal(err)
	}
	if err := h.server.ReloadDirectory(); err != nil {
		t.Fatal(err)
	}
	get := func(query string) pageDTO[appListItemDTO] {
		t.Helper()
		return getJSON[pageDTO[appListItemDTO]](h, "/admin/api/apps?vendor=openai&"+query)
	}
	p := get("q=tool&sort=downloads&order=desc&limit=2&page=2")
	if p.Total != 23 || p.TotalPages != 12 || p.Items[0].ID != "tool-20" || p.Items[1].ID != "tool-19" || p.Items[1].Enabled {
		t.Fatalf("%+v", p)
	}
	if *p.Items[0].LatestVersion != "1.10.0" || p.Items[0].VersionDiscoveredAt.Unix() != 120 || *p.Items[0].SuccessfulDownloads != 20 {
		t.Fatal(p.Items[0])
	}
	if p = get("q=tool&sort=updated&order=desc&limit=2"); p.Items[0].ID != "tool-22" {
		t.Fatal(p)
	}
	if p = get("q=tool&sort=name&lang=zh-CN&limit=2"); p.Items[0].ID != "tool-22" {
		t.Fatal(p)
	}
	if p = get("q=tool&sort=downloads&order=asc&limit=2"); *p.Items[0].SuccessfulDownloads != 0 {
		t.Fatal(p)
	}
	if p = get("q=tool-22&page=99&limit=2"); p.Total != 1 || p.Page != 99 || len(p.Items) != 0 {
		t.Fatal("page beyond the end", p)
	}
	if p = get("q=absent"); p.Total != 0 || p.TotalPages != 1 || len(p.Items) != 0 || p.Limit != 20 {
		t.Fatal("empty result", p)
	}
	if p = get("q=info"); len(p.Items) != 1 || p.Items[0].LatestVersion != nil || p.Items[0].VersionDiscoveredAt != nil || p.Items[0].SuccessfulDownloads != nil {
		t.Fatal("content application statistics", p)
	}
	if p = get("q=tool&state=disabled&limit=100"); p.Total != 11 {
		t.Fatal(p.Total)
	}
	a, _ := h.store.Application("openai/tool-22")
	if _, err := h.store.DB.Exec("INSERT INTO app_versions(app_id,version,first_seen_s) VALUES(?,?,0)", a.StorageID(), "10.0.0"); err != nil {
		t.Fatal(err)
	}
	if p = get("q=tool&sort=version&order=desc&limit=1"); p.Items[0].ID != "tool-22" || p.Items[0].VersionDiscoveredAt != nil {
		t.Fatal("protocol version order", p)
	}
	if p = get("q=tool&sort=updated&order=asc&limit=100"); p.Items[len(p.Items)-1].ID != "tool-22" {
		t.Fatal("missing time must sort last", p)
	}
	// Version metadata belongs to the current source epoch; counters to the application.
	if _, err := h.store.DB.Exec("UPDATE applications SET source_epoch=source_epoch+1 WHERE uid=?", a.UID); err != nil {
		t.Fatal(err)
	}
	if err := h.server.ReloadDirectory(); err != nil {
		t.Fatal(err)
	}
	if p = get("q=tool-22"); p.Items[0].LatestVersion != nil || *p.Items[0].SuccessfulDownloads != 22 {
		t.Fatal("epoch isolation", p)
	}
	for _, query := range []string{"sort=bad", "order=bad", "lang=fr", "view=table", "limit=101", "page=0", "state=gone", "q=", "vendor=Bad"} {
		h.expectError("GET", "/admin/api/apps?"+query, nil, 400, codeInvalidQuery, nil)
	}
	h.expectError("GET", "/admin/api/apps?vendor=missing", nil, 404, codeVendorNotFound, nil)
}

func TestApplicationListStateFiltersFollowMutationsAndIsolateVendors(t *testing.T) {
	h := newHarness(t)
	h.login("")
	v := h.createVendor("many")
	h.createVendor("other")
	h.createApp("other", "foreign", application.Info, nil)
	var last store.Application
	for i := range 21 {
		last = h.createApp(v.ID, fmt.Sprintf("tool-%02d", i), application.Info, map[string]any{"enabled": i%2 == 0})
	}
	h.setEnabled("vendors/many", false)
	seen := map[string]bool{}
	for page := 1; page <= 2; page++ {
		p := getJSON[pageDTO[appListItemDTO]](h, fmt.Sprintf("/admin/api/apps?vendor=many&page=%d&limit=20", page))
		if p.Total != 21 || p.TotalPages != 2 {
			t.Fatal(p)
		}
		for _, a := range p.Items {
			if a.VendorID != v.ID || seen[a.UID] {
				t.Fatal("mixed vendor or duplicate", a)
			}
			seen[a.UID] = true
		}
	}
	h.setEnabled("vendors/many", true)
	count := func(state string) int64 {
		t.Helper()
		return getJSON[pageDTO[appListItemDTO]](h, "/admin/api/apps?vendor=many&q=tool&state="+state).Total
	}
	if count("enabled") != 11 || count("disabled") != 10 {
		t.Fatal("state filters", count("enabled"), count("disabled"))
	}
	h.setEnabled("apps/"+last.Key, false)
	if count("enabled") != 10 || count("disabled") != 11 {
		t.Fatal("state filters after disable")
	}
	current := h.adminApp(last.Key)
	h.deleteApp(last.Key, last.UID, current.Revision, 200)
	if count("disabled") != 10 || count("current") != 20 {
		t.Fatal("state filters after delete")
	}
}

func TestVendorListFiltersAndPreviewsApplications(t *testing.T) {
	h := newHarness(t)
	h.login("")
	h.createVendor("acme")
	for i := range 7 {
		h.createApp("acme", fmt.Sprintf("tool-%d", i), application.Info, map[string]any{"enabled": i != 6})
	}
	h.createVendor("zeta")
	p := getJSON[pageDTO[vendorListItemDTO]](h, "/admin/api/vendors?q=tool&limit=1")
	if p.Total != 1 || len(p.Items) != 1 || p.Items[0].ID != "acme" || len(p.Items[0].Apps) != 5 || p.Items[0].AppTotal != 7 || p.Items[0].Apps[0].Key != "acme/tool-0" {
		t.Fatal("search by application with five-item preview", p)
	}
	if p = getJSON[pageDTO[vendorListItemDTO]](h, "/admin/api/vendors?q=tool-6&state=disabled"); p.Total != 1 || p.Items[0].AppTotal != 1 {
		t.Fatal("state filter applies to the preview", p)
	}
	if p = getJSON[pageDTO[vendorListItemDTO]](h, "/admin/api/vendors?page=9"); p.Page != 9 || len(p.Items) != 0 || p.Limit != 12 || p.Total < 4 {
		t.Fatal("page beyond the end", p)
	}
	if p = getJSON[pageDTO[vendorListItemDTO]](h, "/admin/api/vendors?q=zeta"); p.Total != 1 || len(p.Items[0].Apps) != 0 || p.Items[0].AppTotal != 0 {
		t.Fatal("vendor without applications", p)
	}
	for _, query := range []string{"state=all", "limit=0", "page=x", "sort=name"} {
		h.expectError("GET", "/admin/api/vendors?"+query, nil, 400, codeInvalidQuery, nil)
	}
}
