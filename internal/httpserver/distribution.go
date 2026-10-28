package httpserver

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

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
		if entry.AutoIndex {
			s.serveFileIndex(w, r, entry, "")
			return
		}
		// Temporary: enabling auto_index later lists the root at this address.
		w.Header().Set("Location", "/"+entry.Descriptor.ID)
		w.WriteHeader(http.StatusTemporaryRedirect)
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
	if entry.AutoIndex && strings.HasSuffix(file, "/") {
		s.serveFileIndex(w, r, entry, strings.TrimSuffix(file, "/"))
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
	done, admitted := s.admitDownload(w, r)
	if !admitted {
		return
	}
	defer done()
	file, row, release, err := s.hosted.Open(entry.UID, relative)
	if err != nil && isNotFound(err) && entry.AutoIndex {
		directory, dirErr := s.store.HostedDirectoryExists(entry.UID, relative)
		if dirErr != nil {
			s.writeError(w, r, storageError(dirErr))
			return
		}
		if directory {
			redirectToDirectory(w, entry, relative)
			return
		}
	}
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
	if s.loopDetected(w, r, entry) {
		return
	}
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
	done, admitted := s.admitDownload(w, r)
	if !admitted {
		return
	}
	defer done()
	receipt := newDownloadReceipt(w, r)
	if err := s.httpCache.Serve(receipt, r, entry, relative); err != nil {
		if errors.Is(err, httpcache.ErrDirectory) {
			redirectToDirectory(w, entry, relative)
			return
		}
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
	case application.DownloadAliasOperation:
		if s.loopDetected(w, r, entry) {
			return
		}
		s.serveReleaseAlias(w, r, entry, op)
		return
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
		if s.loopDetected(w, r, entry) {
			return
		}
		rep, err := s.catalog.Represent(r.Context(), key, op, appURL)
		if err != nil {
			s.writeError(w, r, releaseError(err))
			return
		}
		w.Header().Set("Content-Type", rep.ContentType)
		w.Header().Set("Accept-Ranges", "none")
		_, _ = w.Write(rep.Body)
	case application.ArtifactOperation:
		if s.loopDetected(w, r, entry) {
			return
		}
		resource, err := s.catalog.Authorize(r.Context(), key, op.Target, op.Resource)
		if err != nil {
			s.writeError(w, r, releaseError(err))
			return
		}
		if entry.Provider == application.JSONRelease {
			// Stable choice keys need not be filenames. Use only the authorized
			// URL's final segment, never its query or an upstream header.
			filename := resource.Key
			if source, parseErr := url.Parse(resource.Source); parseErr == nil {
				candidate := path.Base(source.Path)
				if candidate != "." && candidate != "/" && len(candidate) <= 255 && !strings.ContainsAny(candidate, "\\\r\n\x00") {
					filename = candidate
				}
			}
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
			w.Header().Set("Content-Security-Policy", sandboxPolicy)
		}
		if r.Method == http.MethodHead {
			// HEAD never starts a download or counts as one.
			artifactHeaders(w, resource)
			if resource.Size != nil {
				w.Header().Set("Content-Length", strconv.FormatInt(*resource.Size, 10))
			}
			return
		}
		done, admitted := s.admitDownload(w, r)
		if !admitted {
			return
		}
		defer done()
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
}

// artifactETag names the verified bytes of an artifact, which never change.
func artifactETag(resource download.Resource) string { return `"sha256-` + resource.Hash + `"` }

// etagListed reports whether an If-None-Match value matches etag (weak comparison).
func etagListed(header, etag string) bool {
	for _, tag := range strings.Split(header, ",") {
		tag = strings.TrimPrefix(strings.TrimSpace(tag), "W/")
		if tag == "*" || tag == etag {
			return true
		}
	}
	return false
}

// singleRange drops a multi-range request header: ServeContent would read
// such ranges in its own goroutine, which can outlive the shared reader.
func singleRange(r *http.Request) *http.Request {
	if !strings.Contains(r.Header.Get("Range"), ",") {
		return r
	}
	copy := r.Clone(r.Context())
	copy.Header.Del("Range")
	return copy
}

// serveArtifact streams a release artifact while the shared download
// progresses. A failure after the headers aborts the connection instead of
// truncating the body silently.
func (s *Server) serveArtifact(w *downloadReceipt, r *http.Request, resource download.Resource) {
	defer s.store.SettleCounters() // Persist this transfer's counters promptly; failures are reported, never fatal.
	etag := artifactETag(resource)
	// A client that already holds the verified bytes needs no transfer, and
	// none is started or counted for it.
	if etagListed(r.Header.Get("If-None-Match"), etag) {
		artifactHeaders(w, resource)
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
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
	count := func(n int) {
		// Counters are buffered in memory; accounting never aborts a client transfer.
		_ = s.store.AddFor(metricApp, "downstream_bytes", int64(n))
		_ = s.store.AddVersion(app, resource.Version, 0, int64(n))
	}
	// Only verified, complete content carries the ETag and honours ranges and
	// preconditions, for example to let a downstream RedApp resume. Bytes of a
	// transfer still in progress are unverified: a client resuming them from
	// another generation could join a rejected prefix to a valid tail.
	if total, seekErr := rd.Seek(0, io.SeekEnd); rd.Kind == "cache_hit" && seekErr == nil {
		if _, err = rd.Seek(0, io.SeekStart); err != nil {
			s.writeError(w, r, storageError(err))
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Accept-Ranges", "bytes")
		reader := &artifactReader{Reader: rd, total: total}
		http.ServeContent(&artifactWriter{downloadReceipt: w, count: count}, singleRange(r), "", time.Time{}, reader)
		if reader.err != nil && reader.err != io.EOF {
			_ = s.store.AddFor(metricApp, "download_errors", 1)
			panic(http.ErrAbortHandler)
		}
		// Only a response that reached the end of the file is a completed download.
		reachedEnd := w.status == http.StatusOK || w.status == http.StatusPartialContent && strings.HasSuffix(w.Header().Get("Content-Range"), "-"+strconv.FormatInt(total-1, 10)+"/"+strconv.FormatInt(total, 10))
		if w.failed {
			_ = s.store.AddFor(metricApp, "download_errors", 1)
		} else if reachedEnd {
			_ = s.store.AddFor(metricApp, "download_success", 1)
		}
		return
	}
	if _, err = rd.Seek(0, io.SeekStart); err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	// Wait for the first byte, so that a transfer failing at once is reported
	// as an error the client can retry before any header is sent.
	var first [1]byte
	if _, re := rd.Read(first[:]); re != nil && re != io.EOF {
		_ = s.store.AddFor(metricApp, "download_errors", 1)
		s.fail(w, r, codeUpstreamUnavailable, re, "Artifact download failed")
		return
	}
	if _, err = rd.Seek(0, io.SeekStart); err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	w.Header().Set("Accept-Ranges", "none")
	buf := make([]byte, 32<<10)
	for {
		n, re := rd.Read(buf)
		if n > 0 {
			written, we := w.Write(buf[:n])
			count(written)
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

// artifactReader serves complete content of known length to http.ServeContent.
// It records a failure, so the connection is aborted instead of truncated
// silently, and delivers the final byte only with the body's verified EOF: a
// finished body may still be failed later, and with a declared Content-Length
// a client that received every byte would take that for a complete download.
type artifactReader struct {
	*download.Reader
	total int64
	err   error
}

func (r *artifactReader) Read(p []byte) (n int, err error) {
	defer func() {
		if err != nil {
			r.err = err
		}
	}()
	if last := r.total - 1; r.Offset() < last && int64(len(p)) > last-r.Offset() {
		p = p[:last-r.Offset()]
	}
	if n, err = r.Reader.Read(p); err != nil || n == 0 || r.Offset() != r.total {
		return n, err
	}
	// EOF follows only verified content; anything else withholds the last byte.
	var probe [1]byte
	if _, err = r.Reader.Read(probe[:]); err != io.EOF {
		if err == nil {
			err = errors.New("artifact exceeds its declared length")
		}
		return 0, err
	}
	return n, nil
}

// artifactWriter accounts and flushes every artifact write, as the streaming
// loop does, so bytes of a transfer in progress reach the client as they arrive.
type artifactWriter struct {
	*downloadReceipt
	count func(int)
}

func (w *artifactWriter) Write(b []byte) (int, error) {
	n, err := w.downloadReceipt.Write(b)
	// The text body of a 416 is not artifact traffic.
	if w.status == http.StatusOK || w.status == http.StatusPartialContent {
		w.count(n)
	}
	if err == nil {
		w.downloadReceipt.Flush()
	}
	return n, err
}

// countRequest increments counters.requests for a valid file request.
func (s *Server) countRequest(w http.ResponseWriter, r *http.Request) bool {
	if err := s.store.Add("requests", 1); err != nil {
		s.writeError(w, r, storageError(err))
		return false
	}
	return true
}
