package httpserver

import (
	"github.com/PMExtra/RedApp/internal/releasemaintenance"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) ReleaseMaintenance() *releasemaintenance.Service {
	if service := s.retention.Load(); service != nil {
		return service
	}
	candidate := &releasemaintenance.Service{DB: s.DB, Registry: s.Registry, Catalog: s.Catalog, Downloads: s.Downloads}
	if s.retention.CompareAndSwap(nil, candidate) {
		return candidate
	}
	return s.retention.Load()
}

func (s *Server) retentionAPI(w http.ResponseWriter, r *http.Request, app, endpoint string) bool {
	if app == "" || !strings.HasPrefix(endpoint, "retention/") {
		return false
	}
	e, ok := s.Registry.LookupAny(app)
	if !ok || e.Protocol == nil {
		fail(w, 404, "Release retention is not supported")
		return true
	}
	service := s.ReleaseMaintenance()
	parts := strings.Split(endpoint, "/")
	switch {
	case endpoint == "retention/status" && r.Method == http.MethodGet:
		raw, err := service.Status(e.UID)
		if err != nil {
			fail(w, 503, "Retention status unavailable")
		} else {
			reply(w, 200, raw)
		}
	case endpoint == "retention/preview" && r.Method == http.MethodPost:
		var input struct {
			Revision int64 `json:"revision"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid retention preview")
			return true
		}
		if input.Revision != e.Revision {
			problem(w, 409, "RETENTION_INVALID", "Configuration changed; reload before previewing")
			return true
		}
		preview, err := service.Preview(r.Context(), app)
		if err != nil {
			problem(w, 409, "RETENTION_INVALID", "Channels could not be verified or the retention source changed; nothing deleted")
		} else {
			reply(w, 200, preview)
		}
	case len(parts) == 3 && r.Method == http.MethodGet:
		job, err := s.DB.CleanupPreview(e.StorageID(), parts[1])
		if err != nil || job.Retention == nil || job.ExecutedAt == nil && !time.Now().Before(job.ExpiresAt) || job.ExecutedAt != nil && !time.Now().Before(job.ExecutedAt.Add(24*time.Hour)) {
			problem(w, 409, "RETENTION_INVALID", "Preview or receipt expired")
			return true
		}
		if parts[2] == "items" {
			page, limit := 1, 25
			var err error
			if value := r.URL.Query().Get("page"); value != "" {
				page, err = strconv.Atoi(value)
			}
			if err != nil || page < 1 {
				fail(w, 400, "Invalid page")
				return true
			}
			if value := r.URL.Query().Get("limit"); value != "" {
				limit, err = strconv.Atoi(value)
			}
			if err != nil || limit < 1 || limit > 100 {
				fail(w, 400, "Invalid limit")
				return true
			}
			items := job.Retention.Versions
			start := len(items)
			if page <= max(1, (len(items)+limit-1)/limit) {
				start = (page - 1) * limit
			}
			end := start + limit
			if end > len(items) {
				end = len(items)
			}
			reply(w, 200, map[string]any{"items": items[start:end], "total": len(items), "page": page, "total_pages": max(1, (len(items)+limit-1)/limit), "result": job.Result})
		} else {
			fail(w, 404, "Retention endpoint not found")
		}
	case len(parts) == 3 && parts[2] == "execute" && r.Method == http.MethodPost:
		var input struct{}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid retention execution")
			return true
		}
		result, err := service.Execute(r.Context(), app, parts[1])
		if err != nil {
			problem(w, 409, "RETENTION_INVALID", "Preview expired or policy, source, or channels changed; rebuild the preview")
		} else {
			reply(w, 200, result)
		}
	default:
		fail(w, 405, "Retention method not allowed")
	}
	return true
}
