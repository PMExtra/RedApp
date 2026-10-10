package httpserver

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/hosted"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
)

// hostedMetadataBytes is x-limits.metadata_field_bytes of uploadHostedFile.
const hostedMetadataBytes = 4096

// hostedTransferWindow is the streaming window of uploads and imports
// (x-timeout); the server's default read deadline does not apply.
const hostedTransferWindow = 5 * time.Minute

func hostedCapable(e application.Entry) bool { return e.Provider == application.Hosted }

func (s *Server) listHostedFiles(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, hostedCapable)
	if !ok {
		return
	}
	page, limit, apiErr := pageQuery(r, 25)
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	files, err := s.store.HostedPage(e.UID, page, limit)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	writeOK(w, hostedFilePage(files))
}

// trackedBody remembers the first read error of a transfer source, so a
// failure of the client body or the import source is told apart from a
// storage failure.
type trackedBody struct {
	io.Reader
	io.Closer
	err error
}

func (b *trackedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err != nil && err != io.EOF && b.err == nil {
		b.err = err
	}
	return n, err
}

// hostedTransfer is one create/replace request of an upload or import.
type hostedTransfer struct {
	entry      application.Entry
	path       string
	expectedID string
	transferID string
	// sourceError is the error code for a failure of the file source.
	sourceError errorCode
}

// hostedTarget resolves the application and transfer_id of an upload or import.
func (s *Server) hostedTarget(w http.ResponseWriter, r *http.Request) (application.Entry, string, bool) {
	e, ok := s.maintainedApp(w, r, hostedCapable)
	if !ok || s.refuseDeleted(w, r, e) {
		return e, "", false
	}
	id := r.URL.Query().Get("transfer_id")
	if !identity.ValidUID(id) {
		s.fail(w, r, codeInvalidQuery, nil, "transfer_id must be 32 lowercase hexadecimal characters")
		return e, "", false
	}
	return e, id, true
}

func (s *Server) validHostedInput(w http.ResponseWriter, r *http.Request, path, expectedID string) bool {
	if !store.ValidHostedPath(path) {
		s.fail(w, r, codeValidationFailed, nil, "path must be a relative file path without empty, . or .. segments")
		return false
	}
	if expectedID != "" && !identity.ValidUID(expectedID) {
		s.fail(w, r, codeValidationFailed, nil, "expected_id must be a file ID")
		return false
	}
	return true
}

func (s *Server) storeHostedFile(w http.ResponseWriter, r *http.Request, t hostedTransfer, source *trackedBody, open func(context.Context) (io.ReadCloser, int64, error)) {
	file, err := s.hosted.Put(r.Context(), t.entry, t.path, t.expectedID, t.transferID, open)
	var tooLarge *http.MaxBytesError
	switch {
	case err == nil:
		writeCreated(w, "", 0, hostedFileDTO{ID: file.ID, Path: file.Path, SHA256: file.SHA256, SizeBytes: file.SizeBytes, CreatedAt: file.CreatedAt.UTC()})
	case errors.Is(err, download.ErrArtifactLimit), errors.As(err, &tooLarge), source.err != nil && errors.As(source.err, &tooLarge):
		s.fail(w, r, codeArtifactTooLarge, err, "The file exceeds the configured size limit")
	case errors.Is(err, context.Canceled), r.Context().Err() != nil:
		s.fail(w, r, codeTransferCancelled, err, "The transfer was cancelled")
	case source.err != nil, errors.Is(err, distributor.ErrImport), errors.Is(err, io.ErrUnexpectedEOF):
		s.fail(w, r, t.sourceError, err, "The file could not be received completely")
	case errors.Is(err, hosted.ErrTransferInUse):
		s.fail(w, r, codeTransferIdInUse, err, "Another running transfer uses this transfer_id")
	case errors.Is(err, store.ErrHostedFileChanged):
		s.fail(w, r, codeFileConflict, err, "The file at this path changed; reload and select the current file to replace it")
	case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrSourceInactive):
		s.fail(w, r, codeSourceChanged, err, "The application changed during the transfer; retry")
	case errors.Is(err, download.ErrWriterLimit), errors.Is(err, download.ErrReaderLimit):
		s.fail(w, r, codeTransferCapacity, err, "Transfer capacity is currently full; retry later")
	default:
		s.writeError(w, r, storageError(err))
	}
}

func (s *Server) uploadHostedFile(w http.ResponseWriter, r *http.Request) {
	e, transferID, ok := s.hostedTarget(w, r)
	if !ok {
		return
	}
	if media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || media != "multipart/form-data" {
		s.fail(w, r, codeUnsupportedMediaType, err, "Content-Type must be multipart/form-data")
		return
	}
	r, finish, ok := s.applicationLease(w, r, e.StorageID())
	if !ok {
		return
	}
	defer finish()
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(hostedTransferWindow))
	r.Body = http.MaxBytesReader(w, r.Body, s.downloads.MaxArtifactBytes()+128<<10)
	reader, err := r.MultipartReader()
	if err != nil {
		s.fail(w, r, codeInvalidRequest, err, "Request body is not a multipart upload")
		return
	}
	// The small fields precede the single file part (x-multipart-order), so
	// the file streams without buffering.
	path, part, apiErr := hostedField(reader, "path")
	expectedID := ""
	if apiErr == nil && part.FormName() == "expected_id" && part.FileName() == "" {
		expectedID, part, apiErr = hostedField(reader, "expected_id", part)
	}
	if apiErr == nil && (part.FormName() != "file" || part.FileName() == "") {
		apiErr = newError(codeInvalidRequest, nil, hostedPartOrder)
	}
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	s.storeHostedUpload(w, r, e, transferID, path, expectedID, part)
}

const hostedPartOrder = "Multipart parts must be path, an optional expected_id, then file"

// hostedField reads the metadata field name (from current, or the next part
// when current is nil) and returns it with the part that follows.
func hostedField(reader *multipart.Reader, name string, current ...*multipart.Part) (string, *multipart.Part, *apiError) {
	part := (*multipart.Part)(nil)
	if len(current) > 0 {
		part = current[0]
	} else {
		var err error
		if part, err = reader.NextPart(); err != nil {
			return "", nil, newError(codeInvalidRequest, err, hostedPartOrder)
		}
	}
	if part.FormName() != name || part.FileName() != "" {
		return "", nil, newError(codeInvalidRequest, nil, hostedPartOrder)
	}
	value, err := io.ReadAll(io.LimitReader(part, hostedMetadataBytes+1))
	if err != nil {
		return "", nil, newError(codeInvalidRequest, err, "Upload field "+name+" could not be read")
	}
	if len(value) > hostedMetadataBytes {
		return "", nil, newError(codeValidationFailed, nil, name+" exceeds 4096 bytes")
	}
	next, err := reader.NextPart()
	if err != nil {
		return "", nil, newError(codeInvalidRequest, err, hostedPartOrder)
	}
	return string(value), next, nil
}

func (s *Server) storeHostedUpload(w http.ResponseWriter, r *http.Request, e application.Entry, transferID, path, expectedID string, file io.Reader) {
	if !s.validHostedInput(w, r, path, expectedID) {
		return
	}
	source := &trackedBody{Reader: file, Closer: r.Body}
	open := func(context.Context) (io.ReadCloser, int64, error) { return source, -1, nil }
	s.storeHostedFile(w, r, hostedTransfer{entry: e, path: path, expectedID: expectedID, transferID: transferID, sourceError: codeInvalidRequest}, source, open)
}

func (s *Server) importHostedFile(w http.ResponseWriter, r *http.Request) {
	e, transferID, ok := s.hostedTarget(w, r)
	if !ok {
		return
	}
	var input struct {
		Path       string `json:"path"`
		URL        string `json:"url"`
		ExpectedID string `json:"expected_id"`
	}
	if apiErr := decodeJSON(r, &input); apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	if !s.validHostedInput(w, r, input.Path, input.ExpectedID) {
		return
	}
	if !distributor.ValidImportURL(input.URL) {
		s.fail(w, r, codeValidationFailed, nil, "url must be an absolute http or https URL without credentials or fragment")
		return
	}
	r, finish, ok := s.applicationLease(w, r, e.StorageID())
	if !ok {
		return
	}
	defer finish()
	source := &trackedBody{}
	open := func(ctx context.Context) (io.ReadCloser, int64, error) {
		response, err := s.pool.FetchImport(ctx, e.UID, e.VendorUID, input.URL)
		if err != nil {
			if ctx.Err() == nil {
				source.err = err
			}
			return nil, 0, err
		}
		source.Reader, source.Closer = response.Body, response.Body
		return source, response.ContentLength, nil
	}
	s.storeHostedFile(w, r, hostedTransfer{entry: e, path: input.Path, expectedID: input.ExpectedID, transferID: transferID, sourceError: codeImportSourceFailed}, source, open)
}

func (s *Server) deleteHostedFile(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, hostedCapable)
	if !ok {
		return
	}
	id, ok := s.pathUID(w, r, "file_id")
	if !ok {
		return
	}
	switch err := s.hosted.Delete(e.UID, id); {
	case err == nil:
		writeNoContent(w)
	case isNotFound(err):
		s.fail(w, r, codeFileNotFound, nil, "File not found; it may have been replaced or deleted")
	default:
		s.writeError(w, r, storageError(err))
	}
}

type hostedTransferDTO struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Bytes      int64  `json:"bytes"`
	TotalBytes *int64 `json:"total_bytes"`
	State      string `json:"state"`
}

func (s *Server) getHostedTransfer(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, hostedCapable)
	if !ok {
		return
	}
	id, ok := s.pathUID(w, r, "transfer_id")
	if !ok {
		return
	}
	progress, found := s.hosted.Progress(e.UID, id)
	if !found {
		s.fail(w, r, codeTransferNotFound, nil, "No running transfer with this ID")
		return
	}
	out := hostedTransferDTO{ID: progress.ID, Path: progress.Path, Bytes: progress.Bytes, State: progress.State}
	if progress.Total >= 0 {
		total := progress.Total
		out.TotalBytes = &total
	}
	writeOK(w, out)
}

func (s *Server) cancelHostedTransfer(w http.ResponseWriter, r *http.Request) {
	e, ok := s.maintainedApp(w, r, hostedCapable)
	if !ok {
		return
	}
	id, ok := s.pathUID(w, r, "transfer_id")
	if !ok {
		return
	}
	if !s.hosted.Cancel(e.UID, id) {
		s.fail(w, r, codeTransferNotFound, nil, "No running transfer with this ID")
		return
	}
	writeNoContent(w)
}
