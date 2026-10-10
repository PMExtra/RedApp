package httpserver

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/store"
)

// tableOrder is the sort of listApps.
type tableOrder struct{ key, direction, lang string }

func tableOrderQuery(r *http.Request) (tableOrder, *apiError) {
	query := r.URL.Query()
	o := tableOrder{key: query.Get("sort"), direction: query.Get("order"), lang: query.Get("lang")}
	if o.key == "" {
		o.key = "name"
	}
	if o.direction == "" {
		o.direction = "asc"
	}
	if o.lang == "" {
		o.lang = "en"
	}
	switch {
	case o.key != "name" && o.key != "version" && o.key != "updated" && o.key != "downloads":
		return o, newError(codeInvalidQuery, nil, "sort must be name, version, updated or downloads")
	case o.direction != "asc" && o.direction != "desc":
		return o, newError(codeInvalidQuery, nil, "order must be asc or desc")
	case o.lang != "en" && o.lang != "zh-CN":
		return o, newError(codeInvalidQuery, nil, "lang must be en or zh-CN")
	}
	return o, nil
}

// applicationTable returns every matching application, sorted. Version
// metadata uses the current source epoch; lifetime counters use the stable
// application UID. Missing values sort last in both directions.
func (s *Server) applicationTable(vendor, q, state string, o tableOrder) ([]appListItemDTO, error) {
	apps, err := s.store.ApplicationsMatching(vendor, q, state)
	if err != nil {
		return nil, err
	}
	rows := make([]appListItemDTO, 0, len(apps))
	names := make(map[string]string, len(apps))
	for _, app := range apps {
		row := appListItemDTO{appDTO: appDocument(app)}
		if entry, ok := s.registry.LookupAny(app.Key); ok && entry.UID == app.UID && entry.SourceEpoch == app.SourceEpoch {
			latest, discovered, err := s.latestKnownVersion(entry)
			if err != nil {
				return nil, err
			}
			if latest != "" {
				row.LatestVersion, row.VersionDiscoveredAt = &latest, discovered
			}
		}
		// Hosted files and information pages do not record this counter.
		if store.AppPathApplies(app.Provider, "cache_ttl_seconds") {
			counters, err := s.store.CountersFor(app.MetricsID())
			if err != nil {
				return nil, err
			}
			n := counters["download_success"]
			row.SuccessfulDownloads = &n
		}
		names[app.Key] = strings.ToLower(app.Name.En)
		if o.lang == "zh-CN" {
			names[app.Key] = strings.ToLower(app.Name.ZhCN)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		order := 0
		missingA, missingB := false, false
		switch o.key {
		case "name":
			order = strings.Compare(names[a.Key], names[b.Key])
		case "version":
			missingA, missingB = a.LatestVersion == nil, b.LatestVersion == nil
			if !missingA && !missingB {
				order = s.compareVersions(a.Key, *a.LatestVersion, *b.LatestVersion)
			}
		case "updated":
			missingA, missingB = a.VersionDiscoveredAt == nil, b.VersionDiscoveredAt == nil
			if !missingA && !missingB {
				order = compareTimes(*a.VersionDiscoveredAt, *b.VersionDiscoveredAt)
			}
		case "downloads":
			missingA, missingB = a.SuccessfulDownloads == nil, b.SuccessfulDownloads == nil
			if !missingA && !missingB {
				order = compareInts(*a.SuccessfulDownloads, *b.SuccessfulDownloads)
			}
		}
		if missingA != missingB {
			return !missingA
		}
		if order == 0 {
			return a.Key < b.Key
		}
		if o.direction == "desc" {
			return order > 0
		}
		return order < 0
	})
	return rows, nil
}

// compareVersions orders two versions of one application by its protocol,
// falling back to text order.
func (s *Server) compareVersions(key, a, b string) int {
	if entry, ok := s.registry.LookupAny(key); ok && entry.Protocol != nil {
		if n, err := entry.Protocol.CompareVersions(a, b); err == nil {
			return n
		}
	}
	return strings.Compare(a, b)
}

func compareTimes(a, b time.Time) int { return a.Compare(b) }

func compareInts(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
