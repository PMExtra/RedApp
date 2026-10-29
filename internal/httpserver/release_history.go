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
)

// maxReleaseHistory bounds the releases listReleases returns.
const maxReleaseHistory = 200

// errUnknownVersion is a version this instance has not discovered.
var errUnknownVersion = errors.New("version not discovered")

type releaseSummaryDTO struct {
	ReleaseID    string    `json:"release_id"`
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
// discovered, newest version first. Releases of earlier epochs are not
// served, so they are not listed.
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
		out = append(out, releaseSummaryDTO{ReleaseID: r.Version, Version: data.Version, Build: data.Build, DiscoveredAt: r.FirstSeen, Channels: channels})
	}
	// Stored newest discovery first, so equal versions keep that order.
	slices.SortStableFunc(out, func(a, b releaseSummaryDTO) int {
		if order := jsonrelease.CompareDisplayVersions(b.Version, a.Version); order != 0 {
			return order
		}
		return jsonrelease.CompareDisplayVersions(b.Build, a.Build)
	})
	return out, nil
}

// label is how a release is named to people: its version, with its build
// when it has one.
func (r releaseSummaryDTO) label() string {
	if r.Build == "" {
		return r.Version
	}
	return r.Version + " (" + r.Build + ")"
}

// displayVersions maps the release IDs of a JSON release to their releases;
// nil for other providers, whose versions name themselves.
func (s *Server) displayVersions(e application.Entry) (map[string]releaseSummaryDTO, error) {
	if _, ok := e.Protocol.(application.SnapshotProtocol); !ok {
		return nil, nil
	}
	history, err := s.releaseHistory(e)
	if err != nil {
		return nil, err
	}
	out := make(map[string]releaseSummaryDTO, len(history))
	for _, r := range history {
		out[r.ReleaseID] = r
	}
	return out, nil
}

// byDisplayVersion orders release IDs newest display version first, like
// releaseHistory; IDs without stored metadata follow in string order.
func byDisplayVersion(ids []string, display map[string]releaseSummaryDTO) []string {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b string) int {
		x, okA := display[a]
		y, okB := display[b]
		switch {
		case okA && okB:
			if order := jsonrelease.CompareDisplayVersions(y.Version, x.Version); order != 0 {
				return order
			}
			if order := jsonrelease.CompareDisplayVersions(y.Build, x.Build); order != 0 {
				return order
			}
		case okA:
			return -1
		case okB:
			return 1
		}
		return strings.Compare(a, b)
	})
	return out
}

// releaseOfVersion is the newest discovered release with this display
// version.
func (s *Server) releaseOfVersion(e application.Entry, version string) (string, error) {
	if !releaseconfig.ValidVersion(version) {
		return "", errUnknownVersion
	}
	history, err := s.releaseHistory(e)
	if err != nil {
		return "", err
	}
	for _, r := range history {
		if r.Version == version {
			return r.ReleaseID, nil
		}
	}
	return "", errUnknownVersion
}

func releaseLookupError(err error) *apiError {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errUnknownVersion) {
		return newError(codeFileNotFound, err, "Release not found")
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
		if item.ReleaseID == release.Version {
			writeOK(w, item)
			return
		}
	}
	s.writeError(w, r, storageError(errors.New("looked-up release is not stored")))
}
