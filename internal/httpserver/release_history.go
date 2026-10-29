package httpserver

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/jsonrelease"
	"github.com/PMExtra/RedApp/internal/releaseconfig"
	"github.com/PMExtra/RedApp/internal/store"
)

// maxReleaseHistory bounds the releases listReleases returns.
const maxReleaseHistory = 200

type releaseSummaryDTO struct {
	Version      string    `json:"version"`
	Build        string    `json:"build"`
	DiscoveredAt time.Time `json:"discovered_at"`
	// Channels currently pointing at this release.
	Channels []string `json:"channels"`
}

type releaseHistoryDTO struct {
	Items     []releaseSummaryDTO `json:"items"`
	Truncated bool                `json:"truncated"`
}

// releaseHistory is every release the application's current source epoch
// discovered, newest version first; a release is named by its version.
// Releases of earlier epochs are not served, so they are not listed.
func (s *Server) releaseHistory(e application.Entry) ([]releaseSummaryDTO, error) {
	stored, err := s.store.Releases(e.StorageID())
	if err != nil {
		return nil, err
	}
	heads := map[string][]string{}
	for _, channel := range e.Descriptor.Channels {
		if channel == "latest" {
			continue
		}
		head, err := s.store.Channel(e.StorageID(), channel)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		heads[head.Version] = append(heads[head.Version], channel)
	}
	out := make([]releaseSummaryDTO, 0, len(stored))
	for _, r := range stored {
		data, err := jsonrelease.DecodeRelease(r.Raw, true)
		if err != nil {
			return nil, err
		}
		channels := append([]string{}, heads[r.Version]...)
		slices.Sort(channels)
		out = append(out, releaseSummaryDTO{Version: data.Version, Build: data.Build, DiscoveredAt: r.FirstSeen, Channels: channels})
	}
	slices.SortStableFunc(out, func(a, b releaseSummaryDTO) int {
		return jsonrelease.CompareVersions(b.Version, a.Version)
	})
	return out, nil
}

func releaseLookupError(err error) *apiError {
	if errors.Is(err, sql.ErrNoRows) {
		return newError(codeFileNotFound, err, "Version not found")
	}
	return storageError(err)
}

func (s *Server) listReleases(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.publishedApp(w, r)
	if !ok {
		return
	}
	if _, ok = entry.Protocol.(application.SnapshotProtocol); !ok {
		s.fail(w, r, codeCapabilityUnsupported, nil, "Release history is unavailable")
		return
	}
	history, err := s.releaseHistory(entry)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	out := releaseHistoryDTO{Items: history}
	if len(history) > maxReleaseHistory {
		out.Items, out.Truncated = history[:maxReleaseHistory], true
	}
	w.Header().Set("Cache-Control", "no-store")
	writeOK(w, out)
}

// refetchRelease replaces a stored JSON release with what the upstream
// publishes now, for an upstream that published another build of a version
// RedApp already knows. Its cached files are cleaned up, its metadata is
// forgotten, and it is discovered again: by the channels that pointed at it,
// else by the lookup. A version with running transfers is refused.
func (s *Server) refetchRelease(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.maintainedApp(w, r, func(e application.Entry) bool { return e.Provider == application.JSONRelease })
	if !ok || s.refuseDeleted(w, r, entry) || s.refuseDisabled(w, r, entry) {
		return
	}
	version := r.PathValue("version")
	if _, err := entry.Protocol.ValidateVersion(version); err != nil {
		s.fail(w, r, codeFileNotFound, err, "Version not found")
		return
	}
	storage := entry.StorageID()
	if _, err := s.store.Release(storage, version); err != nil {
		s.writeError(w, r, releaseLookupError(err))
		return
	}
	channels := []string{}
	for _, channel := range entry.Descriptor.Channels {
		if head, err := s.store.Channel(storage, channel); err == nil && head.Version == version && channel != "latest" {
			channels = append(channels, channel)
		}
	}
	lookup, canLookup := entry.Protocol.(application.VersionLookupProtocol)
	if len(channels) == 0 && (!canLookup || !lookup.CanLookup()) {
		s.fail(w, r, codeValidationFailed, nil, "No channel points at this version and the release configuration has no lookup, so it could not be discovered again")
		return
	}
	// Clean up its cached files through the version cleanup path.
	ids := map[string]bool{}
	for _, view := range s.downloads.SnapshotFor(storage, version) {
		if view.ActiveWriter || view.Readers > 0 {
			s.fail(w, r, codeOperationInProgress, nil, "Files of this version are being transferred; retry when they finish")
			return
		}
		if !view.Retired {
			ids[view.Resource.ID] = true
		}
	}
	if len(ids) > 0 {
		preview, err := s.downloads.Preview(storage, ids, nil)
		if err == nil {
			err = s.downloads.Cleanup(storage, preview.ID)
		}
		if errors.Is(err, store.ErrPreviewRunning) {
			s.fail(w, r, codeOperationInProgress, err, "Files of this version are being cleaned up; retry when it finishes")
			return
		}
		if err != nil {
			s.writeError(w, r, previewBuildFailure(err))
			return
		}
	}
	pointed, err := s.catalog.Forget(entry.Descriptor.ID, version)
	if errors.Is(err, store.ErrReleaseInUse) {
		s.fail(w, r, codeOperationInProgress, err, "Files of this version are being transferred; retry when they finish")
		return
	}
	if errors.Is(err, store.ErrSourceInactive) {
		s.fail(w, r, codeSourceChanged, err, "Application source changed; retry the request")
		return
	}
	if err != nil {
		s.writeError(w, r, releaseLookupError(err))
		return
	}
	if len(pointed) > 0 {
		channels = pointed
	}
	for _, channel := range channels {
		if _, err = s.catalog.Refresh(r.Context(), entry.Descriptor.ID, channel); err != nil {
			break
		}
	}
	if len(channels) == 0 {
		_, err = s.catalog.Lookup(r.Context(), entry.Descriptor.ID, version)
	}
	if errors.Is(err, application.ErrVersionNotFound) {
		s.fail(w, r, codeVersionNotFound, err, "The upstream no longer has this version")
		return
	}
	if err != nil {
		s.writeError(w, r, releaseError(err))
		return
	}
	history, err := s.releaseHistory(entry)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	for _, item := range history {
		if item.Version == version {
			writeOK(w, item)
			return
		}
	}
	// The channel moved on to a newer version while this one was forgotten.
	s.fail(w, r, codeVersionNotFound, nil, "The upstream no longer publishes this version")
}

// lookupRelease discovers a version an administrator names and stores it
// without moving any channel.
func (s *Server) lookupRelease(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.maintainedApp(w, r, func(e application.Entry) bool { return e.Provider == application.JSONRelease })
	if !ok || s.refuseDeleted(w, r, entry) || s.refuseDisabled(w, r, entry) {
		return
	}
	var input struct {
		Version string `json:"version"`
	}
	if err := decodeJSON(r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	input.Version = strings.TrimSpace(input.Version)
	if !releaseconfig.ValidVersion(input.Version) {
		s.fail(w, r, codeValidationFailed, nil, "Enter a version such as 1.2.3")
		return
	}
	protocol, ok := entry.Protocol.(application.VersionLookupProtocol)
	if !ok || !protocol.CanLookup() {
		s.fail(w, r, codeValidationFailed, nil, "The release configuration has no lookup")
		return
	}
	release, err := s.catalog.Lookup(r.Context(), entry.Descriptor.ID, input.Version)
	if errors.Is(err, application.ErrVersionNotFound) {
		s.fail(w, r, codeVersionNotFound, err, "The upstream does not have this version")
		return
	}
	if err != nil {
		s.writeError(w, r, releaseError(err))
		return
	}
	history, err := s.releaseHistory(entry)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	for _, item := range history {
		if item.Version == release.Version {
			writeOK(w, item)
			return
		}
	}
	s.writeError(w, r, storageError(errors.New("looked-up release is not stored")))
}
