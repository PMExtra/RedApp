package httpserver

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"

	"github.com/PMExtra/RedApp/installers"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/httpcache"
	"github.com/PMExtra/RedApp/internal/store"
)

const sandboxPolicy = "sandbox; default-src 'none'"

// getDistributionFile serves /{vendor}/{app}/{file_path...} (GET and HEAD) for
// a published application: hosted files, HTTP cache files, reviewed static
// assets and installers, then the release protocol routes.
func (s *Server) getDistributionFile(w http.ResponseWriter, r *http.Request) {
	vendor := r.PathValue("vendor")
	if s.reservedPath(w, r, vendor) {
		return
	}
	entry, ok := s.publishedApp(w, r)
	if !ok {
		return
	}
	file := r.PathValue("file_path")
	fileQuery := r.URL.RawQuery != "" || r.URL.ForceQuery
	if file == "" {
		if fileQuery {
			s.fail(w, r, codeInvalidQuery, nil, "Unexpected query string")
			return
		}
		w.Header().Set("Location", "/"+entry.Descriptor.ID)
		w.WriteHeader(http.StatusPermanentRedirect)
		return
	}
	if fileQuery {
		code := codeInvalidQuery
		if entry.Provider == application.Hosted || entry.Provider == application.HttpCache {
			code = codeInvalidPath
		}
		s.fail(w, r, code, nil, "File requests do not accept a query string")
		return
	}
	switch entry.Provider {
	case application.Hosted:
		s.serveHostedFile(w, r, entry, file)
	case application.HttpCache:
		s.serveCacheFile(w, r, entry, file)
	default:
		s.serveReleasePath(w, r, entry, file)
	}
}

// applicationLease admits the response as application work, so deletion and
// source changes wait for or interrupt it (see applicationResponse).
func (s *Server) applicationLease(w http.ResponseWriter, r *http.Request, storageID string) (*http.Request, func(), bool) {
	leased, finish, err := s.applicationResponse(w, r, storageID)
	if err != nil {
		if errors.Is(err, store.ErrSourceInactive) || errors.Is(err, store.ErrInvalidDirectory) {
			s.fail(w, r, codeApplicationNotFound, err, "Application not found")
		} else {
			s.writeError(w, r, storageError(err))
		}
		return nil, nil, false
	}
	return leased, finish, true
}

func (s *Server) serveHostedFile(w http.ResponseWriter, r *http.Request, entry application.Entry, relative string) {
	if !store.ValidHostedPath(relative) {
		s.fail(w, r, codeInvalidPath, nil, "Invalid file path")
		return
	}
	if !s.countRequest(w, r) {
		return
	}
	r, finish, ok := s.applicationLease(w, r, entry.StorageID())
	if !ok {
		return
	}
	defer finish()
	file, row, release, err := s.hosted.Open(entry.UID, relative)
	if err != nil {
		switch {
		case isNotFound(err), errors.Is(err, store.ErrSourceInactive), errors.Is(err, store.ErrDirectoryDeleted), errors.Is(err, fs.ErrNotExist):
			s.fail(w, r, codeFileNotFound, nil, "File not found")
		case errors.Is(err, download.ErrReaderLimit):
			s.fail(w, r, codeTransferCapacity, nil, "Transfer capacity is currently full; retry later")
		default:
			s.writeError(w, r, storageError(err))
		}
		return
	}
	defer file.Close()
	defer release()
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(relative)}))
	h.Set("ETag", `"sha256-`+row.SHA256+`"`)
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Security-Policy", sandboxPolicy)
	receipt := newDownloadReceipt(w, r)
	http.ServeContent(receipt, r, path.Base(relative), row.CreatedAt, file)
	s.finishDownload(receipt, r, entry.UID)
}

func (s *Server) serveCacheFile(w http.ResponseWriter, r *http.Request, entry application.Entry, relative string) {
	if entry.Upstream == nil {
		s.fail(w, r, codeUpstreamUnavailable, nil, "Application has no source")
		return
	}
	if _, err := entry.Upstream.RelativeURL(relative); err != nil {
		s.fail(w, r, codeInvalidPath, nil, "Invalid relative file path")
		return
	}
	if !s.countRequest(w, r) {
		return
	}
	r, finish, ok := s.applicationLease(w, r, entry.StorageID())
	if !ok {
		return
	}
	defer finish()
	receipt := newDownloadReceipt(w, r)
	if err := s.httpCache.Serve(receipt, r, entry, relative); err != nil {
		s.writeError(w, r, cacheFileError(err))
		return
	}
	s.finishDownload(receipt, r, entry.UID)
}

// cacheFileError maps an httpcache.Serve error (returned before any header
// was written) to the contract.
func cacheFileError(err error) *apiError {
	var status *httpcache.UpstreamStatusError
	var request *distributor.RequestError
	switch {
	case errors.Is(err, httpcache.ErrCacheMiss):
		return newError(codeCacheMiss, nil, "The file is not cached")
	case errors.As(err, &status) && status.NotFound():
		return newError(codeFileNotFound, err, "File not found upstream")
	case errors.As(err, &status):
		return newError(codeUpstreamUnavailable, err, "Upstream returned an error")
	case errors.Is(err, store.ErrSourceInactive):
		return newError(codeSourceChanged, err, "Application source changed; retry the request")
	case errors.Is(err, download.ErrReaderLimit), errors.Is(err, download.ErrWriterLimit):
		return newError(codeTransferCapacity, err, "Transfer capacity is currently full; retry later")
	case errors.Is(err, httpcache.ErrFetchContended):
		return newError(codeCacheContended, err, "Cached file kept changing; retry the request")
	case errors.Is(err, download.ErrArtifactLimit):
		return newError(codeArtifactTooLarge, err, "File exceeds the configured size limit")
	case errors.Is(err, httpcache.ErrUpstream), errors.As(err, &request), errors.Is(err, distributor.ErrConnection),
		errors.Is(err, distributor.ErrUnsafeEncoding), errors.Is(err, distributor.ErrAddressNotAllowed),
		errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return newError(codeUpstreamUnavailable, err, "Upstream is unavailable")
	default:
		return storageError(err)
	}
}

func (s *Server) serveReleasePath(w http.ResponseWriter, r *http.Request, entry application.Entry, file string) {
	op, err := entry.ParsePath(file)
	if err != nil {
		s.fail(w, r, codeFileNotFound, nil, "File not found")
		return
	}
	if !s.countRequest(w, r) {
		return
	}
	r, finish, ok := s.applicationLease(w, r, entry.StorageID())
	if !ok {
		return
	}
	defer finish()
	key := entry.Descriptor.ID
	appURL := s.publicURL(r) + "/" + key
	template := entry.TemplateID
	if template == "" {
		template = key
	}
	switch op.Kind {
	case application.InstallerOperation:
		body, err := installers.Render(template, key, op.Name, appURL)
		if errors.Is(err, fs.ErrNotExist) {
			s.fail(w, r, codeFileNotFound, nil, "Installer not found")
			return
		}
		if err != nil {
			s.fail(w, r, codeInstallerUnavailable, err, "Installer failed its integrity check")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(body)
	case application.StaticOperation:
		body, contentType := []byte(nil), "text/plain; charset=utf-8"
		if asset, found := entry.PublicAsset(op.Name); found {
			body, contentType = asset.Body, asset.ContentType
		} else if body, err = installers.PublicAsset(template, op.Name); err != nil {
			s.fail(w, r, codeFileNotFound, nil, "File not found")
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Security-Policy", sandboxPolicy)
		_, _ = w.Write(body)
	case application.ChannelOperation, application.MetadataOperation:
		rep, err := s.catalog.Represent(r.Context(), key, op, appURL)
		if err != nil {
			s.writeError(w, r, releaseError(err))
			return
		}
		w.Header().Set("Content-Type", rep.ContentType)
		w.Header().Set("Accept-Ranges", "none")
		_, _ = w.Write(rep.Body)
	case application.ArtifactOperation:
		resource, err := s.catalog.Authorize(r.Context(), key, op.Target, op.Resource)
		if err != nil {
			s.writeError(w, r, releaseError(err))
			return
		}
		if r.Method == http.MethodHead {
			// HEAD never starts a download or counts as one.
			artifactHeaders(w, resource)
			if resource.Size != nil {
				w.Header().Set("Content-Length", strconv.FormatInt(*resource.Size, 10))
			}
			return
		}
		receipt := newDownloadReceipt(w, r)
		s.serveArtifact(receipt, r, resource)
		s.finishDownload(receipt, r, entry.UID)
	default:
		s.fail(w, r, codeFileNotFound, nil, "File not found")
	}
}

// releaseError maps catalog errors (metadata fetch, verification, authorization).
func releaseError(err error) *apiError {
	switch {
	case errors.Is(err, application.ErrNotFound):
		return newError(codeFileNotFound, nil, "File not found")
	case errors.Is(err, application.ErrBusy):
		return newError(codeTransferCapacity, err, "Metadata capacity is currently full; retry later")
	case errors.Is(err, store.ErrSourceInactive):
		return newError(codeSourceChanged, err, "Application source changed; retry the request")
	case errors.Is(err, application.ErrUntrusted):
		return newError(codeMetadataUntrusted, err, "Upstream release metadata failed verification")
	case errors.Is(err, application.ErrUpstream), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return newError(codeUpstreamUnavailable, err, "Upstream release metadata is unavailable")
	default:
		return storageError(err)
	}
}

func artifactHeaders(w http.ResponseWriter, resource download.Resource) {
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("X-Expected-SHA256", resource.Hash)
	h.Set("Accept-Ranges", "none")
}

// serveArtifact streams a release artifact while the shared download
// progresses. A failure after the headers aborts the connection instead of
// truncating the body silently.
func (s *Server) serveArtifact(w *downloadReceipt, r *http.Request, resource download.Resource) {
	defer s.store.SettleCounters() // Persist this transfer's counters promptly; failures are reported, never fatal.
	app := resource.Application
	metricApp := resource.MetricScope()
	if err := s.store.AddFor(metricApp, "artifact_requests", 1); err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	if err := s.store.AddVersion(app, resource.Version, 1, 0); err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	rd, _, err := s.downloads.Acquire(r.Context(), resource)
	if err != nil {
		switch {
		case errors.Is(err, download.ErrArtifactLimit):
			s.fail(w, r, codeArtifactTooLarge, err, "Artifact exceeds the configured size limit")
		case errors.Is(err, download.ErrReaderLimit), errors.Is(err, download.ErrWriterLimit):
			s.fail(w, r, codeTransferCapacity, err, "Transfer capacity is currently full; retry later")
		case errors.Is(err, store.ErrSourceInactive):
			s.fail(w, r, codeSourceChanged, err, "Application source changed; retry the request")
		default:
			s.writeError(w, r, storageError(err))
		}
		return
	}
	defer rd.Close()
	if err = s.store.AddFor(metricApp, rd.Kind+"_requests", 1); err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	artifactHeaders(w, resource)
	buf := make([]byte, 32<<10)
	for {
		n, re := rd.Read(buf)
		if n > 0 {
			written, we := w.Write(buf[:n])
			// Counters are buffered in memory; accounting never aborts a client transfer.
			_ = s.store.AddFor(metricApp, "downstream_bytes", int64(written))
			_ = s.store.AddVersion(app, resource.Version, 0, int64(written))
			if we != nil {
				_ = s.store.AddFor(metricApp, "download_errors", 1)
				return
			}
			w.Flush()
		}
		if re != nil {
			if re == io.EOF {
				_ = s.store.AddFor(metricApp, "download_success", 1)
				if w.status == 0 {
					w.WriteHeader(http.StatusOK)
				}
				return
			}
			_ = s.store.AddFor(metricApp, "download_errors", 1)
			if w.status == 0 {
				// Nothing was sent yet: the client can retry the same request.
				s.fail(w, r, codeUpstreamUnavailable, re, "Artifact download failed")
				return
			}
			panic(http.ErrAbortHandler)
		}
	}
}

// countRequest increments counters.requests for a valid file request.
func (s *Server) countRequest(w http.ResponseWriter, r *http.Request) bool {
	if err := s.store.Add("requests", 1); err != nil {
		s.writeError(w, r, storageError(err))
		return false
	}
	return true
}
