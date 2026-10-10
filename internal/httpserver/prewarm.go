package httpserver

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/prewarm"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/warmplan"
)

func (s *Server) prewarmAPI(w http.ResponseWriter, r *http.Request, app, endpoint string) bool {
	if app == "" || !strings.HasPrefix(endpoint, "prewarm/") {
		return false
	}
	entry, ok := s.registry.LookupAny(app)
	if !ok {
		fail(w, 404, "Application not found")
		return true
	}
	capability, release := entry.Protocol.(application.PlatformProtocol)
	if entry.Protocol != nil && !release || entry.Protocol == nil && entry.Provider != application.HttpCache {
		fail(w, 404, "Prewarm is not supported")
		return true
	}
	if endpoint == "prewarm/options" && r.Method == http.MethodGet {
		var platforms []application.Platform
		if release {
			platforms = capability.Platforms()
		}
		reply(w, 200, map[string]any{"release": release, "platforms": platforms, "channels": entry.Descriptor.Channels, "limits": warmplan.DefaultLimits()})
		return true
	}
	service := s.prewarmer
	respond := func(job store.PrewarmJob, err error) {
		switch {
		case errors.Is(err, prewarm.ErrBusy):
			problem(w, 409, "PREWARM_BUSY", "Another prewarm task is running; retry later")
		case errors.Is(err, prewarm.ErrInvalid):
			fail(w, 400, "Invalid prewarm input")
		case err != nil:
			problem(w, 409, "PREWARM_CONFLICT", "Prewarm input or application changed")
		default:
			reply(w, 200, job)
		}
	}
	if endpoint == "prewarm/start" && r.Method == http.MethodPost {
		var input warmplan.Input
		if decodePrewarmInput(w, r, &input) != nil || len(input.RetrySkip) > 0 {
			fail(w, 400, "Invalid prewarm input")
			return true
		}
		job, err := service.Start(r.Context(), app, input, false)
		respond(job, err)
		return true
	}
	parts := strings.Split(endpoint, "/")
	if len(parts) < 2 || !identity.ValidUID(parts[1]) {
		fail(w, 404, "Prewarm job not found")
		return true
	}
	job, err := service.Status(entry.UID, parts[1])
	if err != nil {
		fail(w, 404, "Prewarm job not found")
		return true
	}
	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		reply(w, 200, job)
	case len(parts) == 3 && parts[2] == "items" && r.Method == http.MethodGet:
		page, limit := 1, 25
		if v := r.URL.Query().Get("page"); v != "" {
			page, err = strconv.Atoi(v)
		}
		if err != nil || page < 1 {
			fail(w, 400, "Invalid page")
			return true
		}
		if v := r.URL.Query().Get("limit"); v != "" {
			limit, err = strconv.Atoi(v)
		}
		if err != nil || limit < 1 || limit > 100 {
			fail(w, 400, "Invalid limit")
			return true
		}
		items, total, err := s.store.PrewarmItems(entry.UID, job.ID, page, limit)
		if err != nil {
			fail(w, 503, "Prewarm items unavailable")
		} else {
			reply(w, 200, map[string]any{"items": items, "total": total, "page": page, "total_pages": max(1, (total+limit-1)/limit)})
		}
	case len(parts) == 3 && parts[2] == "cancel" && r.Method == http.MethodPost:
		var input struct{}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid cancellation")
			return true
		}
		if service.Cancel(entry.UID, job.ID) != nil {
			fail(w, 404, "Prewarm job not found")
		} else {
			reply(w, 200, map[string]bool{"cancel_requested": true})
		}
	case len(parts) == 3 && parts[2] == "retry" && r.Method == http.MethodPost:
		var input struct {
			RequestID string `json:"request_id"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid retry")
			return true
		}
		next, err := service.Retry(r.Context(), app, job.ID, input.RequestID)
		respond(next, err)
	default:
		fail(w, 405, "Prewarm method not allowed")
	}
	return true
}

func decodePrewarmInput(w http.ResponseWriter, r *http.Request, input *warmplan.Input) error {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		return err
	}
	if !utf8.Valid(raw) {
		return prewarm.ErrInvalid
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return decodeLimit(w, r, input, 8<<20)
}
