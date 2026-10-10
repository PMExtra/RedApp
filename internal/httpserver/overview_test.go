package httpserver

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/store"
)

var metricGroups = map[string]bool{"disk": true, "traffic": true, "speed": true, "runtime": true, "resources": true}

func TestStatusReportsCatalogMetricsWithStableGroups(t *testing.T) {
	data := []byte("official archive")
	h := newHarness(t)
	h.upstreamProxy(codexRelease("0.159.2", map[string][]byte{"archive.tgz": data}, nil))
	key := h.releaseApp("fixture", "codex", "codex")
	h.expectError("GET", "/admin/api/status", nil, 401, codeAuthRequired, nil)
	h.login("")
	h.request("GET", "/"+key+"/releases/0.159.2/archive.tgz", nil, 200, nil)
	body, _ := h.request("GET", "/admin/api/status", nil, 200, nil)
	global := decodeJSONBody[globalStatusDTO](t, body)
	if len(global.Metrics) != 41 || global.StartedAt.IsZero() || global.SampledAt.Before(global.StartedAt) {
		t.Fatal("global status", string(body))
	}
	values := map[string]*float64{}
	for _, metric := range global.Metrics {
		if !metricGroups[metric.Group] {
			t.Fatal("metric group is not a stable identifier", metric)
		}
		values[metric.Key] = metric.Value
	}
	if *values["disk.free_bytes"] <= 0 || *values["counters.artifact_requests"] != 1 || *values["versions.total"] != 1 {
		t.Fatal("global metric values", string(body))
	}
	body, _ = h.request("GET", "/admin/api/apps/"+key+"/status", nil, 200, nil)
	app := decodeJSONBody[appStatusDTO](t, body)
	if len(app.Metrics) != 25 || app.SampledAt.IsZero() {
		t.Fatal("app status", string(body))
	}
	for _, metric := range app.Metrics {
		if metric.Key == "versions.total" && (metric.Value == nil || *metric.Value != 1) || strings.HasPrefix(metric.Key, "disk.") {
			t.Fatal("app metric", metric)
		}
	}
	// HTTP cache applications have no version count; content applications no metrics.
	h.createApp("fixture", "files", application.HttpCache, map[string]any{"base_url": fixtureUpstream})
	body, _ = h.request("GET", "/admin/api/apps/fixture/files/status", nil, 200, nil)
	for _, metric := range decodeJSONBody[appStatusDTO](t, body).Metrics {
		if metric.Key == "versions.total" && metric.Value != nil {
			t.Fatal("HTTP cache version count fabricated", metric)
		}
	}
	h.createApp("fixture", "about", application.Info, nil)
	h.expectError("GET", "/admin/api/apps/fixture/about/status", nil, 404, codeCapabilityUnsupported, nil)
	h.expectError("GET", "/admin/api/apps/fixture/unknown/status", nil, 404, codeApplicationNotFound, nil)
	h.expectError("GET", "/admin/api/status?verbose=1", nil, 400, codeInvalidQuery, nil)
	// Disabled and deleted applications stay readable.
	h.setAppEnabled(key, false)
	h.request("GET", "/admin/api/apps/"+key+"/status", nil, 200, nil)
	row, _ := h.store.Application(key)
	if err := h.store.DeleteApplication(key, row.Revision); err != nil {
		t.Fatal(err)
	}
	h.request("GET", "/admin/api/apps/"+key+"/status", nil, 200, nil)
}

func TestHistorySeriesUseRFC3339TimesAndPublicKeys(t *testing.T) {
	h := newHarness(t)
	h.login("")
	h.createVendor("fixture")
	files := h.createApp("fixture", "files", application.HttpCache, map[string]any{"base_url": fixtureUpstream})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.server.SampleHistory(ctx, func(e error) { t.Fatal(e) })
	for window, resolution := range map[string]int64{"24h": 60, "7d": 3600, "30d": 3600} {
		body, _ := h.request("GET", "/admin/api/history?metric=disk.cache_bytes&range="+window, nil, 200, nil)
		series := decodeJSONBody[historySeriesDTO](t, body)
		if series.Scope != "global" || series.AppKey != nil || series.ResolutionSeconds != resolution || series.Group != "disk" || !series.From.Before(series.To) || series.To.Location() != time.UTC {
			t.Fatal("global series", string(body))
		}
		if window == "24h" && (len(series.Points) != 1 || series.Points[0].Time.After(series.To)) {
			t.Fatal("sampled minute missing", string(body))
		}
	}
	body, _ := h.request("GET", "/admin/api/apps/"+files.Key+"/history?metric=counters.artifact_requests&range=24h", nil, 200, nil)
	series := decodeJSONBody[historySeriesDTO](t, body)
	if series.Scope != "app" || series.AppKey == nil || *series.AppKey != files.Key || len(series.Points) != 1 || strings.Contains(string(body), files.UID) {
		t.Fatal("application series", string(body))
	}
	for _, query := range []string{"metric=disk.cache_bytes", "range=24h", "metric=unknown&range=24h", "metric=disk.cache_bytes&range=1h", "scope=global&metric=disk.cache_bytes&range=24h", "metric=events.recent_total&range=24h"} {
		h.expectError("GET", "/admin/api/history?"+query, nil, 400, codeInvalidQuery, nil)
	}
	for _, metric := range []string{"disk.free_bytes", "runtime.goroutines", "counters.requests"} {
		h.expectError("GET", "/admin/api/apps/"+files.Key+"/history?range=24h&metric="+metric, nil, 400, codeInvalidQuery, nil)
	}
	h.createApp("fixture", "about", application.Info, nil)
	h.expectError("GET", "/admin/api/apps/fixture/about/history?metric=resources.total&range=24h", nil, 404, codeCapabilityUnsupported, nil)
	h.expectError("GET", "/admin/api/apps/fixture/unknown/history?metric=resources.total&range=24h", nil, 404, codeApplicationNotFound, nil)
}

func TestEventsPaginateNewestFirstWithPublicKeys(t *testing.T) {
	h := newHarness(t)
	h.login("")
	h.createVendor("fixture")
	files := h.createApp("fixture", "files", application.HttpCache, map[string]any{"base_url": fixtureUpstream})
	status := 503
	for i := 0; i < 5; i++ {
		event := store.Event{Category: "http", Code: "upstream_error", Message: fmt.Sprint("event ", i)}
		if i%2 == 0 {
			event.AppID, event.Version, event.ResourceKey, event.UpstreamStatus = files.MetricsID(), "1.0.0", "artifact", &status
		}
		if err := h.store.RecordEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	var events []eventDTO
	path := "/admin/api/events?limit=2"
	for pages := 0; ; pages++ {
		body, _ := h.request("GET", path, nil, 200, nil)
		page := decodeJSONBody[cursorPage[eventDTO]](t, body)
		if strings.Contains(string(body), files.UID) || len(page.Items) > 2 {
			t.Fatal("event page", string(body))
		}
		events = append(events, page.Items...)
		if page.NextCursor == nil {
			break
		}
		path = "/admin/api/events?limit=2&cursor=" + *page.NextCursor
	}
	if len(events) != 5 {
		t.Fatal("events skipped or repeated", events)
	}
	for i, event := range events {
		if i > 0 && event.ID >= events[i-1].ID {
			t.Fatal("events are not newest first", events)
		}
		// Recorded messages are internal lower-case texts shown as sentences.
		if event.Message != fmt.Sprint("Event ", 4-i) {
			t.Fatal("event message", event.Message)
		}
		owned := (4-i)%2 == 0
		if owned && (event.AppKey == nil || *event.AppKey != files.Key || *event.StatusCode != 503 || *event.Version != "1.0.0" || *event.ResourceKey != "artifact" || event.GenerationID != nil) {
			t.Fatal("application event", event)
		}
		if !owned && (event.AppKey != nil || event.StatusCode != nil || event.Version != nil || event.ResourceKey != nil) {
			t.Fatal("empty event fields must be null", event)
		}
	}
	body, _ := h.request("GET", "/admin/api/events?limit=1", nil, 200, nil)
	cursor := *decodeJSONBody[cursorPage[eventDTO]](t, body).NextCursor
	for _, query := range []string{"cursor=not-a-cursor", "cursor=e30", "cursor=" + cursor + "x"} {
		h.expectError("GET", "/admin/api/events?"+query, nil, 400, codeInvalidCursor, nil)
	}
	// A cursor of another list is rejected.
	h.expectError("GET", "/admin/api/apps/"+files.Key+"/cache/entries?cursor="+cursor, nil, 400, codeInvalidCursor, nil)
	for _, query := range []string{"limit=0", "limit=101", "limit=01", "limit=2&limit=3", "cursor=", "app=" + files.Key} {
		h.expectError("GET", "/admin/api/events?"+query, nil, 400, codeInvalidQuery, nil)
	}
	// The application event list was removed.
	h.request("GET", "/admin/api/apps/"+files.Key+"/events", nil, 404, nil)
}

// Application metrics belong to the application's stable identity: they add
// up across its source epochs, while version counts only cover the current
// epoch and never public keys or inapplicable providers.
func TestAppMetricsFollowTheApplicationAcrossSourceEpochs(t *testing.T) {
	data := []byte("official archive")
	h := newHarness(t)
	h.upstreamProxy(codexRelease("0.159.2", map[string][]byte{"archive.tgz": data}, nil))
	key := h.releaseApp("fixture", "codex", "codex")
	other := h.createApp("fixture", "claude", "claude-code", map[string]any{"base_url": fixtureUpstream})
	files := h.createApp("fixture", "files", application.HttpCache, map[string]any{"base_url": fixtureUpstream})
	h.login("")
	h.request("GET", "/"+key+"/releases/0.159.2/archive.tgz", nil, 200, nil)
	first, _ := h.store.Application(key)
	if err := h.store.SeenFor(first.StorageID(), "0.1.0"); err != nil {
		t.Fatal(err)
	}
	h.patchApp(key, map[string]any{"base_url": fixtureUpstream + "/mirror"})
	current, _ := h.store.Application(key)
	if current.SourceEpoch != first.SourceEpoch+1 {
		t.Fatal("base URL change kept the source epoch")
	}
	h.request("GET", "/"+key+"/releases/0.159.2/archive.tgz", nil, 200, nil)
	for owner, version := range map[string]string{other.StorageID(): "3.0.0", files.StorageID(): "not-a-version"} {
		if err := h.store.SeenFor(owner, version); err != nil {
			t.Fatal(err)
		}
	}
	metrics := func(path string) map[string]*float64 {
		t.Helper()
		body, _ := h.request("GET", path, nil, 200, nil)
		values := map[string]*float64{}
		for _, metric := range decodeJSONBody[appStatusDTO](t, body).Metrics {
			values[metric.Key] = metric.Value
		}
		return values
	}
	app := metrics("/admin/api/apps/" + key + "/status")
	if *app["resources.total"] != 2 || *app["counters.artifact_requests"] != 2 || *app["versions.total"] != 1 {
		t.Fatal("application metrics did not follow the application across epochs", *app["resources.total"], *app["counters.artifact_requests"], *app["versions.total"])
	}
	if global := metrics("/admin/api/status"); *global["versions.total"] != 2 {
		t.Fatal("global versions counted an old epoch or an HTTP cache application", *global["versions.total"])
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.server.SampleHistory(ctx, func(e error) { t.Fatal(e) })
	var value int64
	if err := h.sql().QueryRow("SELECT CAST(value AS INTEGER) FROM metric_samples WHERE scope='app' AND app_id=? AND metric='counters.artifact_requests'", current.MetricsID()).Scan(&value); err != nil || value != 2 {
		t.Fatal("history sampler did not use the stable identity", value, err)
	}
	if err := h.sql().QueryRow("SELECT count(*) FROM metric_samples WHERE scope='app' AND (app_id=? OR (app_id=? AND metric='versions.total'))", key, files.MetricsID()).Scan(&value); err != nil || value != 0 {
		t.Fatal("history sampled a public key or an inapplicable metric", value, err)
	}
}
