package httpserver

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/store"
)

type applicationTableRow struct {
	store.Application
	LatestVersion       string     `json:"latest_version"`
	VersionDiscoveredAt *time.Time `json:"version_discovered_at"`
	SuccessfulDownloads *int64     `json:"successful_downloads"`
}

// The table orders all matching records before slicing. Version metadata uses
// the current source epoch; lifetime counters use the stable application UID.
func (s *Server) applicationTable(w http.ResponseWriter, r *http.Request, vendor string, page, limit int, q, state string) {
	key, direction, lang := r.URL.Query().Get("sort"), r.URL.Query().Get("order"), r.URL.Query().Get("lang")
	if key == "" {
		key = "name"
	}
	if direction == "" {
		direction = "asc"
	}
	if lang == "" {
		lang = "en"
	}
	if (key != "name" && key != "version" && key != "updated" && key != "downloads") || (direction != "asc" && direction != "desc") || (lang != "en" && lang != "zh-CN") {
		fail(w, 400, "Invalid application table sort")
		return
	}
	apps, err := s.DB.ApplicationsMatching(vendor, q, state)
	if err != nil {
		directoryError(w, err)
		return
	}
	rows := make([]applicationTableRow, 0, len(apps))
	for _, app := range apps {
		row := applicationTableRow{Application: app}
		if entry, ok := s.Registry.LookupAny(app.Key); ok && entry.UID == app.UID && entry.SourceEpoch == app.SourceEpoch {
			row.LatestVersion, row.VersionDiscoveredAt, err = s.latestKnownVersion(entry)
			if err != nil {
				directoryError(w, err)
				return
			}
		}
		// Hosted files and information pages do not record this counter.
		if app.Provider == "codex" || app.Provider == "claude-code" || app.Provider == "http-cache" {
			counters, e := s.DB.CountersFor(app.MetricsID())
			if e != nil {
				directoryError(w, e)
				return
			}
			n := counters["download_success"]
			row.SuccessfulDownloads = &n
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		order := 0
		missingA, missingB := false, false
		switch key {
		case "name":
			order = strings.Compare(strings.ToLower(tableName(a.Application, lang)), strings.ToLower(tableName(b.Application, lang)))
		case "version":
			missingA, missingB = a.LatestVersion == "", b.LatestVersion == ""
			order = strings.Compare(a.LatestVersion, b.LatestVersion)
			if entry, ok := s.Registry.LookupAny(a.Key); ok && entry.Protocol != nil && !missingA && !missingB {
				if n, e := entry.Protocol.CompareVersions(a.LatestVersion, b.LatestVersion); e == nil {
					order = n
				}
			}
		case "updated":
			missingA, missingB = a.VersionDiscoveredAt == nil, b.VersionDiscoveredAt == nil
			if !missingA && !missingB {
				order = a.VersionDiscoveredAt.Compare(*b.VersionDiscoveredAt)
			}
		case "downloads":
			missingA, missingB = a.SuccessfulDownloads == nil, b.SuccessfulDownloads == nil
			if !missingA && !missingB {
				if *a.SuccessfulDownloads < *b.SuccessfulDownloads {
					order = -1
				}
				if *a.SuccessfulDownloads > *b.SuccessfulDownloads {
					order = 1
				}
			}
		}
		if missingA != missingB {
			return !missingA
		}
		if order == 0 {
			return a.Key < b.Key
		}
		if direction == "desc" {
			return order > 0
		}
		return order < 0
	})
	result := store.NewPage[applicationTableRow](page, limit, int64(len(rows)))
	start := (result.Page - 1) * limit
	end := min(start+limit, len(rows))
	result.Items = rows[start:end]
	reply(w, 200, result)
}

func tableName(a store.Application, lang string) string {
	if lang == "zh-CN" {
		return a.Name.ZhCN
	}
	return a.Name.En
}
