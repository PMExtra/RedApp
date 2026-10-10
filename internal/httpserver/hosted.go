package httpserver

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
)

func (s *Server) hostedError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrConflict):
		problem(w, 409, "RESOURCE_CONFLICT", "The file or application changed. Select the current resource explicitly before replacing it.")
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, store.ErrSourceInactive), errors.Is(err, store.ErrDirectoryDeleted):
		fail(w, 404, "File not found")
	case errors.Is(err, store.ErrInvalidDirectory):
		fail(w, 400, "Invalid resource path or transfer")
	case errors.Is(err, download.ErrArtifactLimit):
		problem(w, 413, "ARTIFACT_TOO_LARGE", "File exceeds the configured size limit")
	case errors.Is(err, download.ErrWriterLimit), errors.Is(err, download.ErrReaderLimit):
		fail(w, 503, "Transfer capacity is currently full")
	case errors.Is(err, distributor.ErrImport):
		fail(w, 502, "Import source could not be downloaded")
	case errors.Is(err, context.Canceled):
		fail(w, 409, "Transfer cancelled")
	default:
		fail(w, 503, "Unable to store or read the file")
	}
}
func (s *Server) hostedAPI(w http.ResponseWriter, r *http.Request, app, endpoint string) bool {
	if endpoint != "files" && !strings.HasPrefix(endpoint, "files/") {
		return false
	}
	entry, ok := s.registry.LookupAny(app)
	if !ok || entry.Provider != application.Hosted {
		fail(w, 404, "Hosted resources unavailable")
		return true
	}
	if s.hosted == nil {
		fail(w, 503, "File storage unavailable")
		return true
	}
	r, finish, err := s.applicationResponse(w, r, entry.StorageID())
	if err != nil {
		s.hostedError(w, err)
		return true
	}
	defer finish()
	parts := strings.Split(endpoint, "/")
	if r.Method == http.MethodGet && endpoint == "files" {
		if !queryAllowed(r, "page", "limit") {
			fail(w, 400, "Invalid file page")
			return true
		}
		page, valid := positivePage(r.URL.Query().Get("page"), 1)
		limit, validLimit := positivePage(r.URL.Query().Get("limit"), 25)
		if !valid || !validLimit || limit > 100 {
			fail(w, 400, "Invalid file page")
			return true
		}
		result, err := s.store.HostedPage(entry.UID, page, limit)
		if err != nil {
			s.hostedError(w, err)
		} else {
			reply(w, 200, result)
		}
		return true
	}
	if len(parts) == 3 && parts[1] == "transfers" {
		if !queryAllowed(r) {
			fail(w, 400, "Invalid transfer query")
			return true
		}
		if r.Method == http.MethodDelete {
			if s.hosted.Cancel(entry.UID, parts[2]) {
				reply(w, 200, map[string]bool{"cancelled": true})
			} else {
				fail(w, 404, "Transfer not found")
			}
			return true
		}
		if r.Method == http.MethodGet {
			progress, ok := s.hosted.Progress(entry.UID, parts[2])
			if ok {
				reply(w, 200, progress)
			} else {
				fail(w, 404, "Transfer not found")
			}
			return true
		}
	}
	if r.Method == http.MethodDelete && len(parts) == 2 {
		if !queryAllowed(r) {
			fail(w, 400, "Invalid file query")
			return true
		}
		if err := s.hosted.Delete(entry.UID, parts[1]); err != nil {
			s.hostedError(w, err)
		} else {
			reply(w, 200, map[string]bool{"deleted": true})
		}
		return true
	}
	if entry.DeletedAt != nil {
		fail(w, 409, "Deleted applications are read-only")
		return true
	}
	if r.Method != http.MethodPost || (endpoint != "files" && endpoint != "files/import") {
		fail(w, 405, "Method not allowed")
		return true
	}
	if !queryAllowed(r, "transfer_id") {
		fail(w, 400, "Invalid transfer query")
		return true
	}
	transferID := r.URL.Query().Get("transfer_id")
	var relative, expected string
	var open func(context.Context) (io.ReadCloser, int64, error)
	if endpoint == "files/import" {
		var input struct {
			Path       string `json:"path"`
			URL        string `json:"url"`
			ExpectedID string `json:"expected_id"`
		}
		if decodeLimit(w, r, &input, 16<<10) != nil || s.pool == nil {
			fail(w, 400, "Invalid import request")
			return true
		}
		relative, expected = input.Path, input.ExpectedID
		open = func(ctx context.Context) (io.ReadCloser, int64, error) {
			response, err := s.pool.FetchImport(ctx, entry.UID, entry.VendorUID, input.URL)
			if err != nil {
				return nil, 0, err
			}
			return response.Body, response.ContentLength, nil
		}
	} else {
		// Authenticated streaming uploads need the same five-minute transfer
		// window as imports; the ordinary API body deadline remains short.
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(5 * time.Minute))
		r.Body = http.MaxBytesReader(w, r.Body, s.downloads.MaxArtifactBytes()+(128<<10))
		reader, err := r.MultipartReader()
		if err != nil {
			fail(w, 400, "Expected a multipart file upload")
			return true
		}
		// Small metadata must precede the single streamed file; no whole-file buffering.
		for fields := 0; fields < 4; fields++ {
			part, err := reader.NextPart()
			if err != nil {
				fail(w, 400, "File upload is missing")
				return true
			}
			if part.FormName() == "file" && part.FileName() != "" {
				open = func(context.Context) (io.ReadCloser, int64, error) {
					return uploadBody{Reader: part, Closer: r.Body}, -1, nil
				}
				break
			}
			value, err := io.ReadAll(io.LimitReader(part, 4097))
			if err != nil || len(value) > 4096 {
				fail(w, 400, "Invalid upload metadata")
				return true
			}
			part.Close()
			switch part.FormName() {
			case "path":
				relative = string(value)
			case "expected_id":
				expected = string(value)
			default:
				fail(w, 400, "Unexpected upload field")
				return true
			}
		}
		if open == nil {
			fail(w, 400, "File upload is missing")
			return true
		}
	}
	result, err := s.hosted.Put(r.Context(), entry, relative, expected, transferID, open)
	if err != nil {
		s.hostedError(w, err)
	} else {
		reply(w, 201, result)
	}
	return true
}

type uploadBody struct {
	io.Reader
	io.Closer
}
