package httpserver

import (
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

func retentionPreview(p store.Preview) (retentionPreviewDTO, error) {
	summary, err := store.ReleaseSummary(p)
	if err != nil {
		return retentionPreviewDTO{}, err
	}
	out := retentionPreviewDTO{ID: p.ID, CreatedAt: p.CreatedAt.UTC(), ExpiresAt: p.ExpiresAt.UTC(), ExecutedAt: utcPointer(p.ExecutedAt), SelectedVersions: p.SelectedItems, LogicalBytes: p.SelectedBytes, ReclaimableBytes: summary.ReclaimableBytes}
	if p.State == store.PreviewDone {
		receipt, err := store.ReleaseResult(p)
		if err != nil {
			return out, err
		}
		out.Result = &retentionReceiptDTO{RetiredVersions: receipt.RetiredVersions, LogicalBytes: receipt.LogicalBytes, Skipped: receipt.Skipped, Selection: cleanupSelection(receipt.Selection)}
	}
	return out, nil
}

func (s *Server) writeRetentionPreview(w http.ResponseWriter, r *http.Request, status int, e application.Entry, p store.Preview) {
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
	if s.refuseDeleted(w, r, e) || s.refuseDisabled(w, r, e) {
		return
	}
	// The preview must reflect the policy the administrator saw, not a newer one.
	if revision != e.Revision {
		s.writeError(w, r, revisionConflict(nil))
		return
	}
	preview, err := s.maintenance.Preview(r.Context(), e.Descriptor.ID)
	switch {
	case err == nil:
		if current, found := s.registry.LookupAny(e.Descriptor.ID); !found || current.Revision != revision {
			s.writeError(w, r, revisionConflict(nil))
			return
		}
		s.writeRetentionPreview(w, r, http.StatusCreated, e, preview)
	case errors.Is(err, releasemaintenance.ErrChannels):
		s.fail(w, r, codeChannelsUnverified, err, "Release channels could not be verified; nothing was selected")
	case errors.Is(err, download.ErrReaderLimit):
		s.fail(w, r, codeTransferCapacity, err, "Transfer capacity is currently full; retry later")
	default:
		s.writeError(w, r, previewBuildFailure(err))
	}
}

func (s *Server) getRetentionPreview(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, releaseCapable)
	if !ok {
		return
	}
	if p, ok := s.previewRecord(w, r, e, store.PreviewRetention); ok {
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
	if _, ok = s.pathUID(w, r, "preview_id"); !ok {
		return
	}
	page, limit, apiErr := pageQuery(r, 25)
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	p, ok := s.previewRecord(w, r, e, store.PreviewRetention)
	if !ok {
		return
	}
	// Retention items are numbered from 1 in display order, so a page starts
	// after a fixed ordinal.
	meta := store.NewPage[retentionVersionDTO](page, limit, int64(p.ScannedItems))
	out := pageDTO[retentionVersionDTO]{Items: []retentionVersionDTO{}, Page: meta.Page, Limit: meta.Limit, Total: meta.Total, TotalPages: meta.TotalPages}
	items, err := s.store.PreviewItems(r.Context(), p.ID, int64(page-1)*int64(limit), limit, false)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	for _, item := range items {
		detail, err := store.ReleaseDetail(item)
		if err != nil {
			s.writeError(w, r, storageError(err))
			return
		}
		reasons := detail.Reasons
		if reasons == nil {
			reasons = []string{}
		}
		out.Items = append(out.Items, retentionVersionDTO{Version: item.Label, Reasons: reasons, Bytes: item.SizeBytes, Selected: item.Selected})
	}
	writeOK(w, out)
}

func (s *Server) executeRetention(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, releaseCapable)
	if !ok {
		return
	}
	if _, ok = s.pathUID(w, r, "preview_id"); !ok || s.refuseDeleted(w, r, e) {
		return
	}
	p, ok := s.previewRecord(w, r, e, store.PreviewRetention)
	if !ok {
		return
	}
	if p.Finished() {
		// The preview ID is the idempotency key: repeat the stored receipt.
		s.writeRetentionPreview(w, r, http.StatusOK, e, p)
		return
	}
	if s.refuseDisabled(w, r, e) {
		return
	}
	if _, err := s.maintenance.Execute(r.Context(), e.Descriptor.ID, p.ID); err != nil {
		s.writeError(w, r, previewError(err))
		return
	}
	if p, ok = s.previewRecord(w, r, e, store.PreviewRetention); ok {
		s.writeRetentionPreview(w, r, http.StatusOK, e, p)
	}
}
