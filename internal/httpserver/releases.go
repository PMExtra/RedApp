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
	if !ok {
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
			message := displayText(v.Error)
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

func cleanupSelection(items []store.CleanupSelection) []cleanupSelectionDTO {
	out := make([]cleanupSelectionDTO, 0, len(items))
	for _, item := range items {
		out = append(out, cleanupSelectionDTO{GenerationID: item.GenerationID, Version: item.Version, ResourceKey: item.ResourceKey, Bytes: item.SnapshotBytes})
	}
	return out
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

// writeVersionCleanup writes a version cleanup preview with its whole
// frozen selection.
func (s *Server) writeVersionCleanup(w http.ResponseWriter, r *http.Request, status int, p store.Preview) {
	summary, err := store.ReleaseSummary(p)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	items, err := s.store.AllPreviewItems(r.Context(), p.ID)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	selection := []store.CleanupSelection{}
	for _, item := range items {
		detail, err := store.ReleaseDetail(item)
		if err != nil {
			s.writeError(w, r, storageError(err))
			return
		}
		selection = append(selection, detail.Generations...)
	}
	out := versionCleanupDTO{
		ID: p.ID, SourceEpoch: p.SourceEpoch, CreatedAt: p.CreatedAt.UTC(), ExpiresAt: p.ExpiresAt.UTC(), ExecutedAt: utcPointer(p.ExecutedAt),
		Selected: cleanupSelection(selection), LogicalBytes: p.SelectedBytes, ReclaimableBytes: summary.ReclaimableBytes, ActiveGenerations: p.ActiveItems, UnknownVersions: summary.UnknownVersions,
	}
	if status == http.StatusCreated {
		writeCreated(w, "", 0, out)
		return
	}
	writeOK(w, out)
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
	preview, err := s.downloads.Preview(storage, ids, unknown)
	if err != nil {
		s.writeError(w, r, previewBuildFailure(err))
		return
	}
	s.writeVersionCleanup(w, r, http.StatusCreated, preview)
}

func (s *Server) executeVersionCleanup(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, releaseCapable)
	if !ok {
		return
	}
	if _, ok = s.pathUID(w, r, "preview_id"); !ok || s.refuseDeleted(w, r, e) {
		return
	}
	preview, ok := s.previewRecord(w, r, e, store.PreviewVersionCleanup)
	if !ok {
		return
	}
	if err := s.downloads.Cleanup(preview.StorageID(), preview.ID); err != nil {
		s.writeError(w, r, previewError(err))
		return
	}
	if preview, ok = s.previewRecord(w, r, e, store.PreviewVersionCleanup); ok {
		s.writeVersionCleanup(w, r, http.StatusOK, preview)
	}
}
