package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/releasemaintenance"
	"github.com/PMExtra/RedApp/internal/store"
)

type retentionRunDTO struct {
	AttemptedAt     time.Time  `json:"attempted_at"`
	SucceededAt     *time.Time `json:"succeeded_at"`
	Outcome         string     `json:"outcome"`
	Reason          *string    `json:"reason"`
	RetiredVersions int        `json:"retired_versions"`
	LogicalBytes    int64      `json:"logical_bytes"`
}

type retentionStatusDTO struct {
	LastRun     *retentionRunDTO `json:"last_run"`
	NextCheckAt *time.Time       `json:"next_check_at"`
}

func (s *Server) getRetentionStatus(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, releaseCapable)
	if !ok {
		return
	}
	run, err := s.maintenance.Status(e.UID)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	status := retentionStatusDTO{NextCheckAt: s.maintenance.NextCheck()}
	if run != nil {
		status.LastRun = &retentionRunDTO{AttemptedAt: run.Attempt.UTC(), SucceededAt: utcPointer(run.Success), Outcome: run.Outcome, RetiredVersions: run.RetiredVersions, LogicalBytes: run.LogicalBytes}
		if run.Reason != "" {
			reason := run.Reason
			status.LastRun.Reason = &reason
		}
	}
	writeOK(w, status)
}

type retentionReceiptDTO struct {
	RetiredVersions int                   `json:"retired_versions"`
	LogicalBytes    int64                 `json:"logical_bytes"`
	Skipped         map[string]string     `json:"skipped"`
	Selection       []cleanupSelectionDTO `json:"selection"`
}

type retentionPreviewDTO struct {
	ID               string               `json:"id"`
	CreatedAt        time.Time            `json:"created_at"`
	ExpiresAt        time.Time            `json:"expires_at"`
	ExecutedAt       *time.Time           `json:"executed_at"`
	SelectedVersions int                  `json:"selected_versions"`
	LogicalBytes     int64                `json:"logical_bytes"`
	ReclaimableBytes int64                `json:"reclaimable_bytes"`
	Result           *retentionReceiptDTO `json:"result"`
}

func retentionPreview(p store.CleanupPreview) (retentionPreviewDTO, error) {
	_, logical := cleanupSelection(p.Selection)
	out := retentionPreviewDTO{ID: p.ID, CreatedAt: p.CreatedAt.UTC(), ExpiresAt: p.ExpiresAt.UTC(), ExecutedAt: utcPointer(p.ExecutedAt), LogicalBytes: logical, ReclaimableBytes: p.ReclaimableBytes}
	for _, v := range p.Retention.Versions {
		if v.Selected {
			out.SelectedVersions++
		}
	}
	if p.ExecutedAt != nil && len(p.Result) > 0 {
		var receipt store.RetentionReceipt
		if err := json.Unmarshal(p.Result, &receipt); err != nil {
			return out, err
		}
		selection, _ := cleanupSelection(receipt.Selection)
		skipped := receipt.Skipped
		if skipped == nil {
			skipped = map[string]string{}
		}
		out.Result = &retentionReceiptDTO{RetiredVersions: receipt.RetiredVersions, LogicalBytes: receipt.LogicalBytes, Skipped: skipped, Selection: selection}
	}
	return out, nil
}

func (s *Server) writeRetentionPreview(w http.ResponseWriter, r *http.Request, status int, e application.Entry, p store.CleanupPreview) {
	dto, err := retentionPreview(p)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	if status == http.StatusCreated {
		writeCreated(w, "/admin/api/apps/"+e.Descriptor.ID+"/retention/"+p.ID, 0, dto)
		return
	}
	writeOK(w, dto)
}

// retentionRecord reads a retention preview of the application while it is
// readable: until it expires, and for 24 hours after execution.
func (s *Server) retentionRecord(w http.ResponseWriter, r *http.Request, e application.Entry, id string) (store.CleanupPreview, bool) {
	p, err := s.store.ApplicationCleanupPreview(e.UID, id)
	if err != nil && !isNotFound(err) {
		s.writeError(w, r, storageError(err))
		return p, false
	}
	now := time.Now()
	if err != nil || p.Retention == nil || p.ExecutedAt == nil && !now.Before(p.ExpiresAt) || p.ExecutedAt != nil && !now.Before(p.ExecutedAt.Add(24*time.Hour)) {
		s.fail(w, r, codePreviewNotFound, err, "The retention preview is unknown or expired; build a new preview")
		return p, false
	}
	return p, true
}

func (s *Server) previewRetention(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, releaseCapable)
	if !ok {
		return
	}
	revision, apiErr := ifMatch(r)
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	if s.refuseDeleted(w, r, e) {
		return
	}
	// The preview must reflect the policy the administrator saw, not a newer one.
	if revision != e.Revision {
		s.writeError(w, r, revisionConflict(nil))
		return
	}
	preview, err := s.maintenance.Preview(r.Context(), e.Descriptor.ID)
	var row store.CleanupPreview
	if err == nil {
		row, err = s.store.ApplicationCleanupPreview(e.UID, preview.ID)
	}
	switch {
	case err == nil:
		if current, found := s.registry.LookupAny(e.Descriptor.ID); !found || current.Revision != revision {
			s.writeError(w, r, revisionConflict(nil))
			return
		}
		s.writeRetentionPreview(w, r, http.StatusCreated, e, row)
	case errors.Is(err, releasemaintenance.ErrChannels):
		s.fail(w, r, codeChannelsUnverified, err, "Release channels could not be verified; nothing was selected")
	case errors.Is(err, download.ErrReaderLimit):
		s.fail(w, r, codeTransferCapacity, err, "Transfer capacity is currently full; retry later")
	case errors.Is(err, store.ErrSourceInactive), errors.Is(err, store.ErrConflict):
		s.fail(w, r, codeSourceChanged, err, "The application is disabled or its source changed; retry after it is enabled and published")
	default:
		s.writeError(w, r, storageError(err))
	}
}

func (s *Server) getRetentionPreview(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, releaseCapable)
	if !ok {
		return
	}
	id, ok := s.pathUID(w, r, "preview_id")
	if !ok {
		return
	}
	if p, ok := s.retentionRecord(w, r, e, id); ok {
		s.writeRetentionPreview(w, r, http.StatusOK, e, p)
	}
}

type retentionVersionDTO struct {
	Version  string   `json:"version"`
	Reasons  []string `json:"reasons"`
	Bytes    int64    `json:"bytes"`
	Selected bool     `json:"selected"`
}

func (s *Server) listRetentionPreviewItems(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, releaseCapable)
	if !ok {
		return
	}
	id, ok := s.pathUID(w, r, "preview_id")
	if !ok {
		return
	}
	page, limit, apiErr := pageQuery(r, 25)
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	p, ok := s.retentionRecord(w, r, e, id)
	if !ok {
		return
	}
	versions := p.Retention.Versions
	meta := store.NewPage[retentionVersionDTO](page, limit, int64(len(versions)))
	out := pageDTO[retentionVersionDTO]{Items: []retentionVersionDTO{}, Page: meta.Page, Limit: meta.Limit, Total: meta.Total, TotalPages: meta.TotalPages}
	start := min((page-1)*limit, len(versions))
	for _, v := range versions[start:min(start+limit, len(versions))] {
		reasons := v.Reasons
		if reasons == nil {
			reasons = []string{}
		}
		out.Items = append(out.Items, retentionVersionDTO{Version: v.Version, Reasons: reasons, Bytes: v.Bytes, Selected: v.Selected})
	}
	writeOK(w, out)
}

func (s *Server) executeRetention(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, releaseCapable)
	if !ok {
		return
	}
	id, ok := s.pathUID(w, r, "preview_id")
	if !ok || s.refuseDeleted(w, r, e) {
		return
	}
	p, ok := s.retentionRecord(w, r, e, id)
	if !ok {
		return
	}
	if p.ExecutedAt != nil {
		// The preview ID is the idempotency key: repeat the stored receipt.
		s.writeRetentionPreview(w, r, http.StatusOK, e, p)
		return
	}
	if p.AppID != e.StorageID() {
		s.fail(w, r, codePreviewStale, nil, "The application source changed since the preview; nothing was removed")
		return
	}
	_, err := s.maintenance.Execute(r.Context(), e.Descriptor.ID, id)
	if err == nil {
		p, err = s.store.ApplicationCleanupPreview(e.UID, id)
	}
	switch {
	case err == nil:
		s.writeRetentionPreview(w, r, http.StatusOK, e, p)
	case isNotFound(err), errors.Is(err, store.ErrExpired):
		s.fail(w, r, codePreviewNotFound, err, "The retention preview is unknown or expired; build a new preview")
	case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrSourceInactive):
		s.fail(w, r, codePreviewStale, err, "Policy, source or channels changed since the preview; nothing was removed")
	default:
		s.writeError(w, r, storageError(err))
	}
}
