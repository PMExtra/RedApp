package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/jsonrelease"
	"github.com/PMExtra/RedApp/internal/releaseconfig"
)

type releaseDownloadsDTO struct {
	Stale     bool                     `json:"stale"`
	FetchedAt time.Time                `json:"fetched_at"`
	Channel   string                   `json:"channel"`
	ReleaseID string                   `json:"release_id"`
	Version   string                   `json:"version"`
	Build     string                   `json:"build"`
	Artifacts []releaseconfig.Artifact `json:"artifacts"`
}

func (s *Server) getReleaseDownloads(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.publishedApp(w, r)
	if !ok || s.loopDetected(w, r) {
		return
	}
	protocol, ok := entry.Protocol.(application.SnapshotProtocol)
	if !ok {
		s.fail(w, r, codeCapabilityUnsupported, nil, "Download choices are unavailable")
		return
	}
	channel := r.URL.Query().Get("channel")
	if channel == "" {
		channel = "latest"
	}
	canonical, err := protocol.CanonicalChannel(channel)
	if err != nil {
		s.fail(w, r, codeFileNotFound, err, "Channel not found")
		return
	}
	release, err := s.catalog.Release(r.Context(), entry.Descriptor.ID, canonical)
	if err != nil {
		s.writeError(w, r, releaseError(err))
		return
	}
	data, err := jsonrelease.DecodeRelease(release.Envelope.Raw, true)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	snapshot, err := jsonrelease.NewSnapshot(data)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	for i := range data.Artifacts {
		path, pathErr := snapshot.DownloadPath(entry.Descriptor.ID, data.Artifacts[i].Key)
		if pathErr != nil {
			s.writeError(w, r, storageError(pathErr))
			return
		}
		data.Artifacts[i].URL = s.publicURL(r) + path
		// Relative to the application: a downstream RedApp reaches this
		// instance at an address of its own, not at the public URL.
		data.Artifacts[i].Path = strings.TrimPrefix(path, "/"+entry.Descriptor.ID+"/")
		data.Artifacts[i].ChecksumURL = ""
		data.Artifacts[i].ChecksumFilename = ""
	}
	head, err := s.store.Channel(entry.StorageID(), canonical)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	// Resolve may have raced a later refresh: use this response's captured
	// release metadata for the timestamp, never another channel's version.
	metadata, err := s.store.Release(entry.StorageID(), release.Version)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	stale := !time.Now().Before(metadata.FetchedAt.Add(time.Duration(entry.Descriptor.DefaultChannelTTLSeconds) * time.Second))
	if head.Version == release.Version && !time.Now().Before(head.ExpiresAt) {
		stale = true
	}
	w.Header().Set("Cache-Control", "no-store")
	writeOK(w, releaseDownloadsDTO{Stale: stale, FetchedAt: metadata.FetchedAt, Channel: canonical, ReleaseID: release.Version, Version: data.Version, Build: data.Build, Artifacts: data.Artifacts})
}
func (s *Server) previewJSONRelease(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.maintainedApp(w, r, func(e application.Entry) bool { return e.Provider == application.JSONRelease })
	if !ok {
		return
	}
	var input struct {
		Config  releaseconfig.Config `json:"config"`
		Channel string               `json:"channel"`
	}
	if err := decodeJSON(r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := input.Config.Validate(); err != nil {
		s.fail(w, r, codeValidationFailed, err, "Invalid JSON release configuration")
		return
	}
	client, err := s.pool.NewScopedClient(input.Config.Sources.Metadata[0].Origin, distributor.ConfiguredRelease, entry.UID, entry.VendorUID)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	release, err := jsonrelease.NewDiscoverer(builtin.JSONQueryRunner(), client).Discover(r.Context(), input.Config, input.Channel)
	if err != nil {
		if errors.Is(err, jsonrelease.ErrSource) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			s.fail(w, r, codeUpstreamUnavailable, err, "Release preview source is unavailable")
			return
		}
		s.fail(w, r, codeValidationFailed, err, "Release preview failed; check query, source policy and upstream availability")
		return
	}
	writeOK(w, release)
}
func (s *Server) serveReleaseAlias(w http.ResponseWriter, r *http.Request, entry application.Entry, op application.Operation) {
	release, err := s.catalog.Release(r.Context(), entry.Descriptor.ID, op.Target)
	if err != nil {
		s.writeError(w, r, releaseError(err))
		return
	}
	data, err := jsonrelease.DecodeRelease(release.Envelope.Raw, true)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	snapshot, err := jsonrelease.NewSnapshot(data)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	path, err := snapshot.DownloadPath(entry.Descriptor.ID, op.Resource)
	if err != nil {
		s.fail(w, r, codeFileNotFound, err, "Artifact is unavailable in this release")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", path)
	w.WriteHeader(http.StatusTemporaryRedirect)
}
