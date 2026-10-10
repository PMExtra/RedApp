package httpserver

import (
	"errors"
	"net/http"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/prewarm"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/warmplan"
)

// prewarmCapable: release providers with platforms, and the HTTP cache.
func prewarmCapable(e application.Entry) bool {
	if _, release := e.Protocol.(application.PlatformProtocol); release {
		return true
	}
	return e.Protocol == nil && e.Provider == application.HttpCache
}

type prewarmOptionsDTO struct {
	Kind          string                 `json:"kind"`
	Channels      []string               `json:"channels"`
	Platforms     []application.Platform `json:"platforms"`
	DefaultLimits warmplan.Limits        `json:"default_limits"`
}

func (s *Server) getPrewarmOptions(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, prewarmCapable)
	if !ok {
		return
	}
	options := prewarmOptionsDTO{Kind: "http_cache", Channels: []string{}, Platforms: []application.Platform{}, DefaultLimits: warmplan.DefaultLimits()}
	if release, ok := e.Protocol.(application.PlatformProtocol); ok {
		options.Kind = "release"
		options.Channels = append(options.Channels, e.Descriptor.Channels...)
		options.Platforms = append(options.Platforms, release.Platforms()...)
	}
	writeOK(w, options)
}

type prewarmJobDTO struct {
	ID              string          `json:"id"`
	State           string          `json:"state"`
	Reason          *string         `json:"reason"`
	Automatic       bool            `json:"automatic"`
	Target          *string         `json:"target"`
	ResolvedVersion *string         `json:"resolved_version"`
	Platforms       []string        `json:"platforms"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	Completed       int             `json:"completed"`
	Succeeded       int             `json:"succeeded"`
	Bytes           int64           `json:"bytes"`
	Ignored         map[string]int  `json:"ignored"`
	Limits          warmplan.Limits `json:"limits"`
}

func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func prewarmJob(job store.PrewarmJob) prewarmJobDTO {
	out := prewarmJobDTO{
		ID: job.ID, State: job.State, Reason: optionalText(job.Reason), Automatic: job.Automatic, Target: optionalText(job.Target),
		ResolvedVersion: optionalText(job.ResolvedVersion), Platforms: append([]string{}, job.Platforms...), CreatedAt: job.Created.UTC(),
		UpdatedAt: job.Updated.UTC(), Completed: job.Completed, Succeeded: job.Succeeded, Bytes: job.Bytes, Ignored: map[string]int{}, Limits: job.Limits,
	}
	for reason, n := range job.Ignored {
		out.Ignored[reason] = n
	}
	return out
}

// writePrewarmStart answers startPrewarm and retryPrewarmJob.
func (s *Server) writePrewarmStart(w http.ResponseWriter, r *http.Request, e application.Entry, job store.PrewarmJob, created bool, err error) {
	switch {
	case err == nil && created:
		writeCreated(w, "/admin/api/apps/"+e.Descriptor.ID+"/prewarm/jobs/"+job.ID, 0, prewarmJob(job))
	case err == nil:
		writeOK(w, prewarmJob(job))
	case errors.Is(err, prewarm.ErrInvalid):
		s.fail(w, r, codeValidationFailed, err, "Invalid prewarm request: check request_id, target, platforms, paths, indexes, match and limits")
	case errors.Is(err, prewarm.ErrBusy):
		s.fail(w, r, codePrewarmBusy, err, "Another prewarm task is running; retry later")
	case errors.Is(err, prewarm.ErrRunning):
		s.fail(w, r, codeOperationInProgress, err, "The job is still running")
	case isNotFound(err):
		s.fail(w, r, codeJobNotFound, err, "Prewarm job not found")
	case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrExpired):
		s.fail(w, r, codePrewarmRequestConflict, err, "request_id was used with a different request or its job expired; generate a new ID")
	case errors.Is(err, store.ErrSourceInactive):
		s.fail(w, r, codeSourceChanged, err, "The application is disabled or its source changed; retry after it is enabled and published")
	default:
		s.writeError(w, r, storageError(err))
	}
}

func (s *Server) startPrewarm(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, prewarmCapable)
	if !ok || s.refuseDeleted(w, r, e) {
		return
	}
	var input struct {
		RequestID string           `json:"request_id"`
		Target    string           `json:"target"`
		Platforms []string         `json:"platforms"`
		Paths     []string         `json:"paths"`
		Indexes   []string         `json:"indexes"`
		Manifest  string           `json:"manifest"`
		Match     *pathmatch.Spec  `json:"match"`
		Limits    *warmplan.Limits `json:"limits"`
	}
	if apiErr := decodeJSON(r, &input); apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	in := warmplan.Input{RequestID: input.RequestID, Target: input.Target, Platforms: input.Platforms, Paths: input.Paths, Indexes: input.Indexes, Manifest: input.Manifest, Match: input.Match}
	if input.Limits != nil {
		if !input.Limits.Valid() {
			s.fail(w, r, codeValidationFailed, nil, "limits are out of range")
			return
		}
		in.Limits = *input.Limits
	}
	job, created, err := s.prewarmer.Start(r.Context(), e.Descriptor.ID, in, false)
	s.writePrewarmStart(w, r, e, job, created, err)
}

// prewarmJobOf reads a retained job of the application or writes JOB_NOT_FOUND.
func (s *Server) prewarmJobOf(w http.ResponseWriter, r *http.Request) (application.Entry, store.PrewarmJob, bool) {
	e, ok := s.maintainedApp(w, r, prewarmCapable)
	if !ok {
		return e, store.PrewarmJob{}, false
	}
	id, ok := s.pathUID(w, r, "job_id")
	if !ok {
		return e, store.PrewarmJob{}, false
	}
	job, err := s.prewarmer.Status(e.UID, id)
	switch {
	case err == nil:
		return e, job, true
	case isNotFound(err):
		s.fail(w, r, codeJobNotFound, nil, "Prewarm job not found")
	default:
		s.writeError(w, r, storageError(err))
	}
	return e, job, false
}

func (s *Server) getPrewarmJob(w http.ResponseWriter, r *http.Request) {
	if _, job, ok := s.prewarmJobOf(w, r); ok {
		writeOK(w, prewarmJob(job))
	}
}

type prewarmItemDTO struct {
	Key    string  `json:"key"`
	Status string  `json:"status"`
	Reason *string `json:"reason"`
	Bytes  int64   `json:"bytes"`
}

func (s *Server) listPrewarmItems(w http.ResponseWriter, r *http.Request) {
	page, limit, apiErr := pageQuery(r, 25)
	e, job, ok := s.prewarmJobOf(w, r)
	if !ok {
		return
	}
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	items, total, err := s.store.PrewarmItems(e.UID, job.ID, page, limit)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	meta := store.NewPage[prewarmItemDTO](page, limit, int64(total))
	out := pageDTO[prewarmItemDTO]{Items: make([]prewarmItemDTO, 0, len(items)), Page: meta.Page, Limit: meta.Limit, Total: meta.Total, TotalPages: meta.TotalPages}
	for _, item := range items {
		out.Items = append(out.Items, prewarmItemDTO{Key: item.Key, Status: item.Status, Reason: optionalText(item.Reason), Bytes: item.Bytes})
	}
	writeOK(w, out)
}

func (s *Server) cancelPrewarmJob(w http.ResponseWriter, r *http.Request) {
	e, job, ok := s.prewarmJobOf(w, r)
	if !ok {
		return
	}
	if err := s.prewarmer.Cancel(e.UID, job.ID); err != nil {
		if isNotFound(err) {
			s.fail(w, r, codeJobNotFound, nil, "Prewarm job not found")
		} else {
			s.writeError(w, r, storageError(err))
		}
		return
	}
	if current, err := s.prewarmer.Status(e.UID, job.ID); err == nil {
		job = current
	}
	writeJSON(w, http.StatusAccepted, prewarmJob(job))
}

func (s *Server) retryPrewarmJob(w http.ResponseWriter, r *http.Request) {
	e, job, ok := s.prewarmJobOf(w, r)
	if !ok || s.refuseDeleted(w, r, e) {
		return
	}
	var input struct {
		RequestID string `json:"request_id"`
	}
	if apiErr := decodeJSON(r, &input); apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	next, created, err := s.prewarmer.Retry(r.Context(), e.Descriptor.ID, job.ID, input.RequestID)
	s.writePrewarmStart(w, r, e, next, created, err)
}
