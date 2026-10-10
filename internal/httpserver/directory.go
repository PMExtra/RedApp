package httpserver

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/media"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/store"
)

// ReloadDirectory republishes the runtime from the database, ordered with
// configuration writes; source fences reject work from replaced snapshots.
func (s *Server) ReloadDirectory() error { return s.store.RepublishConfiguration() }

// directoryFailure maps a store error of a directory, configuration or notes
// operation to its response. notFound is the code for a missing object
// addressed by the path (VENDOR_NOT_FOUND or APPLICATION_NOT_FOUND).
func directoryFailure(err error, notFound errorCode) *apiError {
	var invalid *store.ValidationError
	switch {
	case errors.Is(err, store.ErrVendorNotFound):
		return newError(codeVendorNotFound, nil, "Vendor not found")
	case errors.Is(err, store.ErrApplicationNotFound):
		return newError(codeApplicationNotFound, nil, "Application not found")
	case errors.Is(err, store.ErrNotFound) && notFound == codeVendorNotFound:
		return newError(codeVendorNotFound, nil, "Vendor not found")
	case errors.Is(err, store.ErrNotFound) && notFound == codeApplicationNotFound:
		return newError(codeApplicationNotFound, nil, "Application not found")
	case errors.Is(err, store.ErrNotFound) && notFound == codeCategoryNotFound:
		return newError(codeCategoryNotFound, nil, "Category not found")
	case errors.Is(err, errDeletePending), errors.Is(err, download.ErrTransfersActive):
		return newError(codeApplicationDeletePending, err, "Deletion is not complete; the application stays read-only while its work stops. Retry the deletion")
	case errors.Is(err, store.ErrConflict):
		return revisionConflict(err)
	case errors.Is(err, store.ErrDirectoryDeleted):
		return newError(codeEntityDeleted, nil, "The vendor or application is deleted and read-only")
	case errors.Is(err, store.ErrDirectoryExists):
		return newError(codeAlreadyExists, nil, "This ID is already taken")
	case errors.Is(err, store.ErrVendorHasApplications):
		return newError(codeVendorNotEmpty, nil, "Delete the vendor's applications first")
	case errors.Is(err, store.ErrBuiltinTemplate):
		return newError(codeBuiltinProtected, nil, "Built-in vendors and applications cannot be deleted; disable them instead")
	case errors.Is(err, store.ErrCategoryAmbiguous):
		return newError(codeCategoryAmbiguous, nil, "A category name matches more than one existing category; choose one from the list")
	case errors.Is(err, networkproxy.ErrRedactedMismatch):
		return newError(codeProxyRedactedMismatch, nil, "The saved proxy password can only be kept for the same proxy scheme, user and host; enter the password again")
	case errors.As(err, &invalid):
		return newError(codeValidationFailed, err, sentence(invalid.Detail()))
	case errors.Is(err, store.ErrInvalidDirectory):
		return newError(codeValidationFailed, err, "Invalid vendor or application configuration")
	}
	return storageError(err)
}

// sentence capitalizes a validation detail for the response message.
func sentence(detail string) string {
	if detail == "" {
		return "Invalid value"
	}
	return displayText(detail)
}

// vendorParam is the validated {vendor} path segment.
func vendorParam(r *http.Request) (string, *apiError) {
	id := r.PathValue("vendor")
	if !identity.ValidVendor(id) {
		return "", newError(codeInvalidPath, nil, "Invalid vendor ID")
	}
	return id, nil
}

// appParam is the validated application key of the {vendor}/{app} segments.
func appParam(r *http.Request) (string, *apiError) {
	key := r.PathValue("vendor") + "/" + r.PathValue("app")
	if !identity.ValidKey(key) {
		return "", newError(codeInvalidPath, nil, "Invalid application identity")
	}
	return key, nil
}

// validateIcon accepts "", an embedded preset image or an uploaded icon that
// exists in icon storage.
func (s *Server) validateIcon(field, path string) *apiError {
	if _, ok := builtin.BrandAsset(path); path == "" || ok {
		return nil
	}
	f, _, err := s.icons.Open(path)
	if err != nil {
		return newError(codeValidationFailed, err, field+" must be an uploaded icon or a preset image; upload the icon before selecting it")
	}
	_ = f.Close()
	return nil
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	writeOK(w, providerList())
}

func (s *Server) getVendor(w http.ResponseWriter, r *http.Request) {
	id, e := vendorParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	v, err := s.store.Vendor(id)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeVendorNotFound))
		return
	}
	writeRevision(w, http.StatusOK, v.Revision, vendorDocument(v))
}

type vendorCreateRequest struct {
	ID             *string        `json:"id"`
	Name           *localizedText `json:"name"`
	Description    *localizedText `json:"description"`
	Icon           *string        `json:"icon"`
	LocalizedIcons *localizedText `json:"localized_icons"`
	Enabled        *bool          `json:"enabled"`
}

func (s *Server) createVendor(w http.ResponseWriter, r *http.Request) {
	var in vendorCreateRequest
	if e := decodeJSON(r, &in); e != nil {
		s.writeError(w, r, e)
		return
	}
	if in.ID == nil || in.Name == nil || in.Enabled == nil {
		s.fail(w, r, codeInvalidRequest, nil, "id, name and enabled are required")
		return
	}
	v := store.VendorInput{ID: *in.ID, Name: store.LocalizedText(*in.Name), Enabled: *in.Enabled}
	if in.Description != nil {
		v.Description = store.LocalizedText(*in.Description)
	}
	if in.Icon != nil {
		v.Icon = *in.Icon
	}
	if in.LocalizedIcons != nil {
		v.LocalizedIcons = store.LocalizedText(*in.LocalizedIcons)
	}
	for field, icon := range map[string]string{"icon": v.Icon, "localized_icons.en": v.LocalizedIcons.En, "localized_icons.zh-CN": v.LocalizedIcons.ZhCN} {
		if e := s.validateIcon(field, icon); e != nil {
			s.writeError(w, r, e)
			return
		}
	}
	created, err := s.store.CreateVendor(v)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeVendorNotFound))
		return
	}
	writeCreated(w, "/admin/api/vendors/"+created.ID, created.Revision, vendorDocument(created))
}

type entityStateRequest struct {
	Enabled *bool `json:"enabled"`
}

// decodeEntityState reads the If-Match revision and the EntityStateUpdate body.
func decodeEntityState(r *http.Request) (int64, bool, *apiError) {
	revision, e := ifMatch(r)
	if e != nil {
		return 0, false, e
	}
	var in entityStateRequest
	if e = decodeJSON(r, &in); e != nil {
		return 0, false, e
	}
	if in.Enabled == nil {
		return 0, false, newError(codeInvalidRequest, nil, "enabled is required")
	}
	return revision, *in.Enabled, nil
}

func (s *Server) updateVendor(w http.ResponseWriter, r *http.Request) {
	id, e := vendorParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	revision, enabled, e := decodeEntityState(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	v, err := s.store.PatchVendorFields(id, revision, nil, &enabled)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeVendorNotFound))
		return
	}
	writeRevision(w, http.StatusOK, v.Revision, vendorDocument(v))
}

func (s *Server) deleteVendor(w http.ResponseWriter, r *http.Request) {
	id, e := vendorParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	revision, e := ifMatch(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	if err := s.store.PermanentlyDeleteVendor(id, revision); err != nil {
		s.writeError(w, r, directoryFailure(err, codeVendorNotFound))
		return
	}
	writeNoContent(w)
}

func (s *Server) getApp(w http.ResponseWriter, r *http.Request) {
	key, e := appParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	a, err := s.store.Application(key)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeApplicationNotFound))
		return
	}
	writeRevision(w, http.StatusOK, a.Revision, appDocument(a))
}

type appCreateRequest struct {
	Vendor          *string        `json:"vendor"`
	ID              *string        `json:"id"`
	Provider        *string        `json:"provider"`
	Name            *localizedText `json:"name"`
	Description     *localizedText `json:"description"`
	Icon            *string        `json:"icon"`
	Enabled         *bool          `json:"enabled"`
	Categories      []string       `json:"categories"`
	Tags            []string       `json:"tags"`
	BaseURL         *string        `json:"base_url"`
	BaseURLs        []string       `json:"base_urls"`
	SourceStrategy  *string        `json:"source_strategy"`
	CacheTTLSeconds *int           `json:"cache_ttl_seconds"`
}

// applicationInput validates the provider-specific fields of a create request.
func (in appCreateRequest) applicationInput() (store.ApplicationInput, *apiError) {
	if in.Vendor == nil || in.ID == nil || in.Provider == nil || in.Name == nil || in.Enabled == nil {
		return store.ApplicationInput{}, newError(codeInvalidRequest, nil, "vendor, id, provider, name and enabled are required")
	}
	definition, ok := application.ProviderDefinition(*in.Provider)
	if !ok {
		return store.ApplicationInput{}, newError(codeValidationFailed, nil, "Unknown provider")
	}
	provider := definition.Key
	for path, present := range map[string]bool{"base_url": in.BaseURL != nil, "base_urls": in.BaseURLs != nil, "source_strategy": in.SourceStrategy != nil, "cache_ttl_seconds": in.CacheTTLSeconds != nil} {
		if present && !store.AppPathApplies(provider, path) {
			return store.ApplicationInput{}, newError(codeValidationFailed, nil, fmt.Sprintf("%s does not apply to the %s provider", path, provider))
		}
	}
	if provider == application.HttpCache && len(in.BaseURLs) == 0 {
		return store.ApplicationInput{}, newError(codeValidationFailed, nil, "base_urls requires 1..16 URLs for the http-cache provider")
	}
	a := store.ApplicationInput{ID: *in.ID, Name: store.LocalizedText(*in.Name), Provider: provider, Enabled: *in.Enabled, CacheTTLSeconds: definition.DefaultCacheTTLSeconds,
		Categories: nonNil(in.Categories), Tags: nonNil(in.Tags), BaseURLs: in.BaseURLs}
	if in.Description != nil {
		a.Description = store.LocalizedText(*in.Description)
	}
	if in.Icon != nil {
		a.Icon = *in.Icon
	}
	if in.BaseURL != nil {
		a.BaseURL = *in.BaseURL
	}
	if in.SourceStrategy != nil {
		a.SourceStrategy = *in.SourceStrategy
	}
	if in.CacheTTLSeconds != nil {
		a.CacheTTLSeconds = *in.CacheTTLSeconds
	}
	config, err := application.NormalizeConfig(provider, application.ProviderConfig{BaseURL: a.BaseURL, BaseURLs: a.BaseURLs, SourceStrategy: a.SourceStrategy, CacheTTLSeconds: a.CacheTTLSeconds})
	if err != nil {
		return a, newError(codeValidationFailed, err, sentence(err.Error()))
	}
	a.BaseURL, a.BaseURLs, a.SourceStrategy, a.CacheTTLSeconds = config.BaseURL, config.BaseURLs, config.SourceStrategy, config.CacheTTLSeconds
	return a, nil
}

func (s *Server) createApp(w http.ResponseWriter, r *http.Request) {
	var in appCreateRequest
	if e := decodeJSON(r, &in); e != nil {
		s.writeError(w, r, e)
		return
	}
	input, e := in.applicationInput()
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	if e = s.validateIcon("icon", input.Icon); e != nil {
		s.writeError(w, r, e)
		return
	}
	a, err := s.store.CreateApplication(*in.Vendor, input)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeVendorNotFound))
		return
	}
	writeCreated(w, "/admin/api/apps/"+a.Key, a.Revision, appDocument(a))
}

func (s *Server) updateApp(w http.ResponseWriter, r *http.Request) {
	key, e := appParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	revision, enabled, e := decodeEntityState(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	a, err := s.store.PatchApplicationFields(key, revision, nil, &enabled)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeApplicationNotFound))
		return
	}
	writeRevision(w, http.StatusOK, a.Revision, appDocument(a))
}

// deleteApp permanently deletes an application. confirm_uid guards against
// deleting a different application recreated under the same key.
func (s *Server) deleteApp(w http.ResponseWriter, r *http.Request) {
	key, e := appParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	uid := r.URL.Query().Get("confirm_uid")
	if !identity.ValidUID(uid) {
		s.fail(w, r, codeInvalidQuery, nil, "confirm_uid must be the application UID")
		return
	}
	revision, e := ifMatch(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	a, err := s.store.Application(key)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeApplicationNotFound))
		return
	}
	if a.UID != uid {
		s.writeError(w, r, revisionConflict(nil))
		return
	}
	if err = s.deleteApplication(r.Context(), key, uid, revision); err != nil {
		s.writeError(w, r, directoryFailure(err, codeApplicationNotFound))
		return
	}
	writeOK(w, appDeletionDTO{CleanupPending: s.store.ProcessPendingDeletes(s.dataDir) != nil})
}

// multipartBody opens a multipart/form-data request body.
func multipartBody(r *http.Request) (*multipart.Reader, *apiError) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return nil, newError(codeUnsupportedMediaType, nil, "Content-Type must be multipart/form-data")
	}
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, newError(codeInvalidRequest, err, "Malformed multipart body")
	}
	return reader, nil
}

// readPart reads one part of at most limit bytes. Exceeding the route's body
// limit is PAYLOAD_TOO_LARGE; exceeding limit reports tooLarge.
func readPart(part *multipart.Part, limit int64) (body []byte, tooLarge bool, e *apiError) {
	body, err := io.ReadAll(io.LimitReader(part, limit+1))
	if err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			return nil, false, newError(codePayloadTooLarge, nil, "Request body exceeds the operation limit")
		}
		return nil, false, newError(codeInvalidRequest, err, "Malformed multipart body")
	}
	return body, int64(len(body)) > limit, nil
}

func (s *Server) uploadIcon(w http.ResponseWriter, r *http.Request) {
	reader, e := multipartBody(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" {
		s.fail(w, r, codeInvalidRequest, err, "Exactly one part named file is required")
		return
	}
	body, tooLarge, e := readPart(part, media.MaxBytes)
	part.Close()
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	if tooLarge {
		s.fail(w, r, codePayloadTooLarge, nil, "Icons are limited to 2 MiB")
		return
	}
	if next, err := reader.NextPart(); err != io.EOF {
		if next != nil {
			next.Close()
		}
		s.fail(w, r, codeInvalidRequest, err, "Exactly one part named file is required")
		return
	}
	path, err := s.icons.Put(bytes.NewReader(body))
	switch {
	case errors.Is(err, media.ErrInvalidIcon), errors.Is(err, media.ErrTooLarge):
		s.fail(w, r, codeIconInvalid, err, "Unsupported or unsafe image, or raster dimensions over the limit")
	case err != nil:
		s.writeError(w, r, storageError(err))
	default:
		writeCreated(w, "", 0, storedIconDTO{Icon: path})
	}
}
