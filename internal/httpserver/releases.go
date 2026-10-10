package httpserver

import (
	"errors"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
)

// maintainedApp resolves {vendor}/{app} of an administrator maintenance route
// (releases, retention, prewarm, hosted files). Disabled and deleted
// applications resolve; supported decides the provider capability.
func (s *Server) maintainedApp(w http.ResponseWriter, r *http.Request, supported func(application.Entry) bool) (application.Entry, bool) {
	key := r.PathValue("vendor") + "/" + r.PathValue("app")
	if _, err := application.ParseKey(key); err != nil {
		s.fail(w, r, codeInvalidPath, nil, "Invalid application identity")
		return application.Entry{}, false
	}
	e, ok := s.registry.LookupAny(key)
	if !ok || e.UID == "" {
		s.fail(w, r, codeApplicationNotFound, nil, "Application not found")
		return application.Entry{}, false
	}
	if !supported(e) {
		s.fail(w, r, codeCapabilityUnsupported, nil, "This application's provider does not support this endpoint")
		return application.Entry{}, false
	}
	return e, true
}

// releaseCapable: versions, resources, version cleanup and retention exist for
// release providers (codex, claude-code).
func releaseCapable(e application.Entry) bool { return e.Protocol != nil }

// pathUID reads a 32-hex ID path parameter; anything else is INVALID_PATH.
func (s *Server) pathUID(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id := r.PathValue(name)
	if !identity.ValidUID(id) {
		s.fail(w, r, codeInvalidPath, nil, "Invalid "+name)
		return "", false
	}
	return id, true
}

// refuseDeleted writes ENTITY_DELETED for maintenance writes on a deleted application.
func (s *Server) refuseDeleted(w http.ResponseWriter, r *http.Request, e application.Entry) bool {
	if e.DeletedAt == nil {
		return false
	}
	s.fail(w, r, codeEntityDeleted, nil, "The application is deleted and read-only")
	return true
}

// refuseDisabled writes APPLICATION_DISABLED for actions that need an enabled
// application (and vendor): retention runs, prewarm and HTTP cache refresh.
func (s *Server) refuseDisabled(w http.ResponseWriter, r *http.Request, e application.Entry) bool {
	if e.Enabled {
		return false
	}
	s.fail(w, r, codeApplicationDisabled, nil, "The application or its vendor is disabled; enable it first")
	return true
}

type versionDTO struct {
	Version         string    `json:"version"`
	FirstSeen       time.Time `json:"first_seen"`
	Requests        int64     `json:"requests"`
	DownstreamBytes int64     `json:"downstream_bytes"`
}

// newestFirst orders versions by the provider's version order, newest first.
// Versions the provider cannot parse follow in string order.
func newestFirst(protocol application.Protocol, versions []string) []string {
	var comparable, other []string
	for _, v := range versions {
		if canonical, err := protocol.ValidateVersion(v); err == nil && canonical == v {
			comparable = append(comparable, v)
		} else {
			other = append(other, v)
		}
	}
	sort.Strings(comparable)
	sort.Strings(other)
	sort.SliceStable(comparable, func(i, j int) bool {
		order, err := protocol.CompareVersions(comparable[i], comparable[j])
		return err == nil && order > 0
	})
	return append(comparable, other...)
}

func (s *Server) listVersions(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, releaseCapable)
	if !ok {
		return
	}
	limit, apiErr := queryInt(r, "limit", 50, 1, 100)
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	scope := cursorScope(e.StorageID())
	last, apiErr := decodeAfterCursor(r, "listVersions", scope)
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	stats, err := s.store.VersionStats(e.StorageID())
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	names := make([]string, 0, len(stats))
	for v := range stats {
		names = append(names, v)
	}
	ordered := newestFirst(e.Protocol, names)
	start := 0
	if last != "" {
		i := slices.Index(ordered, last)
		if i < 0 {
			s.writeError(w, r, invalidCursor())
			return
		}
		start = i + 1
	}
	end := min(start+limit, len(ordered))
	page := cursorPage[versionDTO]{Items: make([]versionDTO, 0, end-start)}
	for _, v := range ordered[start:end] {
		row := stats[v]
		page.Items = append(page.Items, versionDTO{Version: row.Version, FirstSeen: row.FirstSeen.UTC(), Requests: row.ArtifactRequests, DownstreamBytes: row.DownstreamBytes})
	}
	if end < len(ordered) {
		page.NextCursor = afterCursor("listVersions", scope, ordered[end-1])
	}
	writeOK(w, page)
}

type resourceDTO struct {
	ID             string     `json:"id"`
	Version        string     `json:"version"`
	Key            string     `json:"key"`
	State          string     `json:"state"`
	Current        bool       `json:"current"`
	Retired        bool       `json:"retired"`
	Bytes          int64      `json:"bytes"`
	TotalBytes     *int64     `json:"total_bytes"`
	ExpectedBytes  *int64     `json:"expected_bytes"`
	SHA256         string     `json:"sha256"`
	Readers        int        `json:"readers"`
	ActiveWriter   bool       `json:"active_writer"`
	RecentBPS      float64    `json:"recent_bps"`
	AverageBPS     float64    `json:"average_bps"`
	Resumes        int        `json:"resumes"`
	VerificationNS int64      `json:"verification_ns"`
	DownloadNS     int64      `json:"download_ns"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	Error          *string    `json:"error"`
}

func (s *Server) listResources(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, releaseCapable)
	if !ok {
		return
	}
	limit, apiErr := queryInt(r, "limit", 50, 1, 100)
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	version := r.URL.Query().Get("version")
	if version != "" {
		if canonical, err := e.Protocol.ValidateVersion(version); err != nil || canonical != version || len(version) > 128 {
			s.fail(w, r, codeInvalidQuery, err, "version must be a canonical version")
			return
		}
	}
	scope := cursorScope(e.StorageID(), version)
	last, apiErr := decodeAfterCursor(r, "listResources", scope)
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	views := s.downloads.SnapshotFor(e.StorageID(), version)
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	start := sort.Search(len(views), func(i int) bool { return views[i].ID > last })
	end := min(start+limit, len(views))
	page := cursorPage[resourceDTO]{Items: make([]resourceDTO, 0, end-start)}
	for _, v := range views[start:end] {
		item := resourceDTO{
			ID: v.ID, Version: v.Resource.Version, Key: v.Resource.Key, State: v.State, Current: v.Current, Retired: v.Retired,
			Bytes: v.Bytes, ExpectedBytes: v.Resource.Size, SHA256: v.Resource.Hash, Readers: v.Readers, ActiveWriter: v.ActiveWriter,
			RecentBPS: v.RecentBPS, AverageBPS: v.AverageBPS, Resumes: v.Resumes, VerificationNS: v.VerificationNS, DownloadNS: v.DownloadNS,
			StartedAt: v.Started.UTC(),
		}
		if v.Total >= 0 {
			total := v.Total
			item.TotalBytes = &total
		}
		if !v.Finished.IsZero() {
			finished := v.Finished.UTC()
			item.FinishedAt = &finished
		}
		if v.Error != "" {
			message := v.Error
			item.Error = &message
		}
		page.Items = append(page.Items, item)
	}
	if end < len(views) {
		page.NextCursor = afterCursor("listResources", scope, views[end-1].ID)
	}
	writeOK(w, page)
}

type cleanupSelectionDTO struct {
	GenerationID string `json:"generation_id"`
	Version      string `json:"version"`
	ResourceKey  string `json:"resource_key"`
	Bytes        int64  `json:"bytes"`
}

func cleanupSelection(items []store.CleanupSelection) ([]cleanupSelectionDTO, int64) {
	out := make([]cleanupSelectionDTO, 0, len(items))
	var total int64
	for _, item := range items {
		out = append(out, cleanupSelectionDTO{GenerationID: item.GenerationID, Version: item.Version, ResourceKey: item.ResourceKey, Bytes: item.SnapshotBytes})
		total += item.SnapshotBytes
	}
	return out, total
}

type versionCleanupDTO struct {
	ID                string                `json:"id"`
	SourceEpoch       int64                 `json:"source_epoch"`
	CreatedAt         time.Time             `json:"created_at"`
	ExpiresAt         time.Time             `json:"expires_at"`
	ExecutedAt        *time.Time            `json:"executed_at"`
	Selected          []cleanupSelectionDTO `json:"selected"`
	LogicalBytes      int64                 `json:"logical_bytes"`
	ReclaimableBytes  int64                 `json:"reclaimable_bytes"`
	ActiveGenerations int                   `json:"active_generations"`
	UnknownVersions   []string              `json:"unknown_versions"`
}

func versionCleanup(p store.CleanupPreview) versionCleanupDTO {
	_, epoch, _ := identity.ParseStorageID(p.AppID)
	selected, logical := cleanupSelection(p.Selection)
	unknown := p.UnknownVersions
	if unknown == nil {
		unknown = []string{}
	}
	return versionCleanupDTO{
		ID: p.ID, SourceEpoch: epoch, CreatedAt: p.CreatedAt.UTC(), ExpiresAt: p.ExpiresAt.UTC(), ExecutedAt: utcPointer(p.ExecutedAt),
		Selected: selected, LogicalBytes: logical, ReclaimableBytes: p.ReclaimableBytes, ActiveGenerations: p.ActiveGenerations, UnknownVersions: unknown,
	}
}

func utcPointer(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	value := t.UTC()
	return &value
}

func (s *Server) previewVersionCleanup(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, releaseCapable)
	if !ok || s.refuseDeleted(w, r, e) {
		return
	}
	var input struct {
		MinimumVersion string `json:"minimum_version"`
		SourceEpoch    *int64 `json:"source_epoch"`
	}
	if apiErr := decodeJSON(r, &input); apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	if canonical, err := e.Protocol.ValidateVersion(input.MinimumVersion); err != nil || canonical != input.MinimumVersion || len(canonical) > 128 {
		s.fail(w, r, codeValidationFailed, err, "minimum_version must be a canonical version")
		return
	}
	storage := e.StorageID()
	if input.SourceEpoch != nil {
		if *input.SourceEpoch < 1 {
			s.fail(w, r, codeValidationFailed, nil, "source_epoch must be a positive integer")
			return
		}
		storage = identity.StorageID(e.UID, *input.SourceEpoch)
	}
	ids, unknown, err := s.catalog.CandidatesForSource(e.Descriptor.ID, storage, input.MinimumVersion, s.downloads.SnapshotFor(storage, ""))
	if err != nil {
		if errors.Is(err, application.ErrNotFound) {
			s.fail(w, r, codeSourceNotFound, err, "source_epoch does not exist for this application")
		} else {
			s.writeError(w, r, storageError(err))
		}
		return
	}
	job, err := s.downloads.Preview(storage, ids, unknown)
	if err == nil {
		var row store.CleanupPreview
		if row, err = s.store.CleanupPreview(storage, job.ID); err == nil {
			writeCreated(w, "", 0, versionCleanup(row))
			return
		}
	}
	switch {
	case errors.Is(err, store.ErrSourceInactive), errors.Is(err, store.ErrConflict):
		s.fail(w, r, codeSourceChanged, err, "The application source changed during the preview; retry")
	default:
		s.writeError(w, r, storageError(err))
	}
}

func (s *Server) executeVersionCleanup(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, releaseCapable)
	if !ok {
		return
	}
	id, ok := s.pathUID(w, r, "preview_id")
	if !ok || s.refuseDeleted(w, r, e) {
		return
	}
	preview, err := s.store.ApplicationCleanupPreview(e.UID, id)
	if err == nil && preview.Retention != nil {
		err = store.ErrNotFound
	}
	if err == nil {
		err = s.downloads.Cleanup(preview.AppID, id)
	}
	if err == nil {
		preview, err = s.store.CleanupPreview(preview.AppID, id)
	}
	switch {
	case err == nil:
		writeOK(w, versionCleanup(preview))
	case isNotFound(err), errors.Is(err, store.ErrExpired), errors.Is(err, store.ErrConflict):
		s.fail(w, r, codePreviewNotFound, err, "The preview is unknown or expired; build a new preview")
	case errors.Is(err, store.ErrSourceInactive):
		s.fail(w, r, codePreviewStale, err, "The application source changed since the preview; nothing was removed")
	default:
		s.writeError(w, r, storageError(err))
	}
}
