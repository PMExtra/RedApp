package httpserver

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/media"
	"github.com/PMExtra/RedApp/internal/store"
)

// ReloadDirectory publishes one complete runtime snapshot. Callers serialize
// mutations with directoryMu; source fences reject work from replaced snapshots.
func (s *Server) ReloadDirectory() error {
	if s.pool == nil {
		return errors.New("Application transport unavailable")
	}
	snapshot, err := s.store.DirectoryConfigurationSnapshot()
	if err != nil {
		return err
	}
	entries, err := builtin.EntriesFromConfiguration(snapshot, s.pool)
	if err != nil {
		return err
	}
	next, err := application.NewRegistry(entries)
	if err != nil {
		return err
	}
	sources := snapshot.Sources
	clients := make(map[string]*distributor.Client, len(sources))
	for _, source := range sources {
		client, e := builtin.NewScopedSourceClient(source.Provider, source.BaseURL, snapshot.ProviderDefaults[source.Provider], source.AppUID, snapshot.ProxyScopes[source.AppUID].VendorUID, s.pool)
		if e != nil {
			return e
		}
		clients[source.StorageID()] = client
	}
	if s.downloads != nil {
		if err = s.downloads.RegisterUpstreams(clients); err != nil {
			return err
		}
	}
	return s.registry.Replace(next.AllEntries())
}

func (s *Server) setDirectoryTTL(key string, expected int64, seconds int) (int64, error) {
	s.directoryMu.Lock()
	defer s.directoryMu.Unlock()
	row, err := s.store.Application(key)
	if err != nil {
		return 0, err
	}
	row, err = s.store.PatchApplicationFields(key, expected, map[string]json.RawMessage{"cache_ttl_seconds": encodeJSON(seconds)}, nil)
	if err != nil {
		return 0, err
	}
	return row.Revision, nil
}

type directoryInput struct {
	Categories      *[]string `json:"categories"`
	Tags            *[]string `json:"tags"`
	explicit        map[string]json.RawMessage
	ConfirmUID      string               `json:"confirm_uid"`
	ConfirmKey      string               `json:"confirm_key"`
	Revision        int64                `json:"revision"`
	ID              string               `json:"id"`
	Name            *store.LocalizedText `json:"name"`
	Description     *store.LocalizedText `json:"description"`
	LocalizedIcons  *store.LocalizedText `json:"localized_icons"`
	Icon            *string              `json:"icon"`
	Enabled         *bool                `json:"enabled"`
	Provider        string               `json:"provider"`
	BaseURL         *string              `json:"base_url"`
	BaseURLs        *[]string            `json:"base_urls"`
	SourceStrategy  *string              `json:"source_strategy"`
	CacheTTLSeconds *int                 `json:"cache_ttl_seconds"`
}

func (in directoryInput) vendorInput() store.VendorInput {
	v := store.VendorInput{ID: in.ID, Enabled: true}
	if in.LocalizedIcons != nil {
		v.LocalizedIcons = *in.LocalizedIcons
	}
	if in.Name != nil {
		v.Name = *in.Name
	}
	if in.Description != nil {
		v.Description = *in.Description
	}
	if in.Icon != nil {
		v.Icon = *in.Icon
	}
	if in.Enabled != nil {
		v.Enabled = *in.Enabled
	}
	return v
}

func (in directoryInput) applicationInput() (store.ApplicationInput, error) {
	if in.LocalizedIcons != nil {
		return store.ApplicationInput{}, store.ErrInvalidDirectory
	}
	d, ok := application.ProviderDefinition(in.Provider)
	if !ok {
		return store.ApplicationInput{}, store.ErrInvalidDirectory
	}
	v := in.vendorInput()
	a := store.ApplicationInput{ID: v.ID, Name: v.Name, Description: v.Description, Icon: v.Icon, Enabled: v.Enabled, Provider: in.Provider, CacheTTLSeconds: d.DefaultCacheTTLSeconds}
	if in.Categories != nil {
		a.Categories = append([]string{}, (*in.Categories)...)
	}
	if in.Tags != nil {
		a.Tags = append([]string{}, (*in.Tags)...)
	}
	if in.BaseURL != nil {
		a.BaseURL = *in.BaseURL
	}
	if in.BaseURLs != nil {
		a.BaseURLs = append([]string{}, (*in.BaseURLs)...)
	}
	if in.SourceStrategy != nil {
		a.SourceStrategy = *in.SourceStrategy
	}
	if in.BaseURL != nil && in.BaseURLs != nil {
		return a, fmt.Errorf("%w: provide base_urls or base_url, not both", store.ErrInvalidDirectory)
	}
	if in.CacheTTLSeconds != nil {
		a.CacheTTLSeconds = *in.CacheTTLSeconds
	}
	return normalizedApplication(a)
}

func normalizedApplication(in store.ApplicationInput) (store.ApplicationInput, error) {
	conf, err := application.NormalizeConfig(in.Provider, application.ProviderConfig{BaseURL: in.BaseURL, BaseURLs: in.BaseURLs, SourceStrategy: in.SourceStrategy, CacheTTLSeconds: in.CacheTTLSeconds})
	if err != nil {
		return in, fmt.Errorf("%w: %v", store.ErrInvalidDirectory, err)
	}
	in.BaseURL, in.CacheTTLSeconds = conf.BaseURL, conf.CacheTTLSeconds
	in.BaseURLs, in.SourceStrategy = conf.BaseURLs, conf.SourceStrategy
	return in, nil
}

func (s *Server) validateDirectoryIcon(path string) error {
	_, builtinIcon := builtin.BrandAsset(path)
	if path == "" || builtinIcon {
		return nil
	}
	if s.icons == nil {
		return store.ErrInvalidDirectory
	}
	f, _, err := s.icons.Open(path)
	if err != nil {
		return fmt.Errorf("%w: upload the icon before selecting it", store.ErrInvalidDirectory)
	}
	return f.Close()
}

func directoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errDeletePending), errors.Is(err, download.ErrTransfersActive):
		problem(w, 409, "DIRECTORY_DELETE_PENDING", "Deletion is not complete. This application is blocked while its tasks stop. Retry deletion; restarting also resumes it.")
	case errors.Is(err, store.ErrConflict):
		problem(w, 409, "DIRECTORY_REVISION_CONFLICT", "Configuration changed; reload before saving")
	case errors.Is(err, store.ErrCategoryAmbiguous):
		problem(w, 409, "CATEGORY_AMBIGUOUS", err.Error())
	case errors.Is(err, sql.ErrNoRows):
		problem(w, 404, "DIRECTORY_NOT_FOUND", "Vendor or application not found")
	case errors.Is(err, store.ErrBuiltinTemplate), errors.Is(err, store.ErrDirectoryExists), errors.Is(err, store.ErrDirectoryDeleted), errors.Is(err, store.ErrVendorHasApplications):
		problem(w, 409, "DIRECTORY_CONFLICT", err.Error())
	case errors.Is(err, store.ErrInvalidDirectory):
		problem(w, 400, "INVALID_DIRECTORY", err.Error())
	default:
		problem(w, 503, "DIRECTORY_UNAVAILABLE", "Unable to persist or load the application directory")
	}
}

// directoryAPI runs only after the existing session, Origin and CSRF checks.
func (s *Server) directoryAPI(w http.ResponseWriter, r *http.Request) bool {
	endpoint := strings.TrimPrefix(r.URL.Path, "/admin/api/")
	parts := strings.Split(endpoint, "/")
	if len(parts) == 3 && parts[0] == "vendors" && parts[2] == "configuration" {
		s.configurationAPI(w, r, "Vendor", parts[1])
		return true
	}
	if len(parts) == 4 && parts[0] == "apps" && parts[3] == "configuration" {
		s.configurationAPI(w, r, "App", parts[1]+"/"+parts[2])
		return true
	}
	if endpoint == "assets/builtin-icon" {
		if r.Method != http.MethodGet || !queryAllowed(r, "path") {
			fail(w, 400, "Invalid icon request")
			return true
		}
		asset, ok := builtin.BrandAsset(r.URL.Query().Get("path"))
		if !ok {
			fail(w, 404, "Icon not found")
			return true
		}
		w.Header().Set("Content-Type", asset.ContentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		w.Write(asset.Body)
		return true
	}
	if len(parts) == 3 && parts[0] == "vendors" && parts[2] == "admin-notes" {
		s.adminNotesAPI(w, r, "vendor", parts[1])
		return true
	}
	if len(parts) == 4 && parts[0] == "apps" && parts[3] == "admin-notes" {
		s.adminNotesAPI(w, r, "app", parts[1]+"/"+parts[2])
		return true
	}
	if len(parts) == 4 && parts[0] == "apps" && parts[3] == "instructions" {
		s.instructionsAPI(w, r, parts[1]+"/"+parts[2])
		return true
	}
	if r.Method == http.MethodGet && (endpoint == "vendors" || endpoint == "apps" || len(parts) == 3 && parts[0] == "vendors" && parts[2] == "apps") {
		s.directoryList(w, r, parts)
		return true
	}

	isVendor := parts[0] == "vendors" && (len(parts) == 1 || len(parts) == 2 || len(parts) == 3 && parts[2] == "apps")
	isApp := parts[0] == "apps" && (len(parts) == 1 || len(parts) == 3)
	if endpoint != "providers" && endpoint != "assets/icons" && !isVendor && !isApp {
		return false
	}
	if !queryAllowed(r) {
		fail(w, 400, "Unexpected query parameters")
		return true
	}
	if endpoint == "providers" {
		if r.Method != http.MethodGet {
			fail(w, 405, "Method not allowed")
			return true
		}
		reply(w, 200, map[string]any{"providers": application.Definitions()})
		return true
	}
	if endpoint == "assets/icons" {
		s.uploadIcon(w, r)
		return true
	}
	if r.Method == http.MethodGet {
		var result any
		var err error
		switch {
		case isVendor && len(parts) == 2:
			var row store.Vendor
			row, err = s.store.Vendor(parts[1])
			result = map[string]any{"vendor": row}
		case isApp && len(parts) == 3:
			var row store.Application
			row, err = s.store.Application(parts[1] + "/" + parts[2])
			result = map[string]any{"app": row}
		default:
			fail(w, 405, "Method not allowed")
			return true
		}
		if err != nil {
			directoryError(w, err)
		} else {
			reply(w, 200, result)
		}
		return true
	}
	if s.pool == nil {
		fail(w, 503, "Application directory is unavailable")
		return true
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPatch && r.Method != http.MethodDelete {
		fail(w, 405, "Method not allowed")
		return true
	}
	var in directoryInput
	if err := decodeLimit(w, r, &in, 64<<10); err != nil {
		fail(w, 400, "Invalid directory input")
		return true
	}
	create := r.Method == http.MethodPost && (endpoint == "vendors" || isVendor && len(parts) == 3)
	if !create && (in.ID != "" || in.Provider != "") {
		fail(w, 400, "ID, parent and provider cannot be changed")
		return true
	}
	if r.Header.Get("If-Match") != "" {
		rev, err := expectedRevision(r)
		if err != nil || in.Revision != 0 && in.Revision != rev {
			fail(w, 400, "Invalid revision")
			return true
		}
		in.Revision = rev
	}
	if !create && in.Revision < 1 {
		fail(w, 400, "Revision is required")
		return true
	}
	if in.Icon != nil {
		if err := s.validateDirectoryIcon(*in.Icon); err != nil {
			directoryError(w, err)
			return true
		}
	}
	if in.LocalizedIcons != nil {
		if isApp || (create && endpoint != "vendors") {
			fail(w, 400, "Unexpected application fields")
			return true
		}
		for _, icon := range []string{in.LocalizedIcons.En, in.LocalizedIcons.ZhCN} {
			if err := s.validateDirectoryIcon(icon); err != nil {
				directoryError(w, err)
				return true
			}
		}
	}
	s.directoryMu.Lock()
	defer s.directoryMu.Unlock()
	var result any
	var err error
	status := 200
	switch {
	case create && endpoint == "vendors":
		if in.Categories != nil || in.Tags != nil || in.Provider != "" || in.BaseURL != nil || in.BaseURLs != nil || in.SourceStrategy != nil || in.CacheTTLSeconds != nil {
			fail(w, 400, "Unexpected vendor fields")
			return true
		}
		var row store.Vendor
		row, err = s.store.CreateVendor(in.vendorInput())
		result = map[string]any{"vendor": row}
		status = 201
	case create:
		var input store.ApplicationInput
		input, err = in.applicationInput()
		if err == nil {
			var row store.Application
			row, err = s.store.CreateApplication(parts[1], input)
			result = map[string]any{"app": row}
			status = 201
		}
	case isVendor && len(parts) == 2:
		if in.Categories != nil || in.Tags != nil || in.BaseURL != nil || in.BaseURLs != nil || in.SourceStrategy != nil || in.CacheTTLSeconds != nil {
			fail(w, 400, "Unexpected vendor fields")
			return true
		}
		var row store.Vendor
		row, err = s.store.Vendor(parts[1])
		if err != nil {
			break
		}
		if r.Method == http.MethodDelete {
			if in.ConfirmKey != parts[1] {
				fail(w, 400, "Confirm the exact vendor ID for permanent deletion")
				return true
			}
			err = s.store.PermanentlyDeleteVendor(parts[1], in.Revision)
		} else if r.Method == http.MethodPatch {
			set, e := in.configurationFields("Vendor")
			if e != nil {
				err = e
				break
			}
			row, err = s.store.PatchVendorFields(parts[1], in.Revision, set, in.Enabled)
		} else {
			fail(w, 405, "Method not allowed")
			return true
		}
		result = map[string]any{"vendor": row}
	case isApp && len(parts) == 3:
		key := parts[1] + "/" + parts[2]
		var row store.Application
		row, err = s.store.Application(key)
		if errors.Is(err, sql.ErrNoRows) && r.Method == http.MethodDelete && in.ConfirmKey == key && in.ConfirmUID != "" {
			reply(w, 200, map[string]any{"deleted": true})
			return true
		}
		if err != nil {
			break
		}
		if r.Method == http.MethodDelete {
			if in.ConfirmKey != key {
				fail(w, 400, "Confirm the exact application key for permanent deletion")
				return true
			}
			if in.ConfirmUID != row.UID {
				err = store.ErrConflict
				break
			}
			err = s.deleteApplication(r.Context(), key, in.Revision)
		} else if r.Method == http.MethodPatch {
			set, e := in.configurationFields("App")
			if e != nil {
				err = e
				break
			}
			if in.BaseURL != nil && in.BaseURLs != nil {
				err = store.ErrInvalidDirectory
				break
			}
			if row.Provider == application.HttpCache && in.BaseURL != nil {
				base, e := distributor.NormalizeBase(*in.BaseURL, distributor.GeneralHTTP)
				if e != nil {
					err = e
					break
				}
				if base != row.BaseURL {
					set["base_urls"] = encodeJSON([]string{base})
				}
			}
			row, err = s.store.PatchApplicationFields(key, in.Revision, set, in.Enabled)
		} else {
			fail(w, 405, "Method not allowed")
			return true
		}
		result = map[string]any{"app": row}
	default:
		fail(w, 405, "Method not allowed")
		return true
	}
	if err != nil {
		directoryError(w, err)
	} else {
		if r.Method == http.MethodDelete {
			cleanupPending := s.store.ProcessPendingDeletes(s.dataDir) != nil
			result = map[string]any{"deleted": true, "cleanup_pending": cleanupPending}
		}
		reply(w, status, result)
	}
	return true
}

func (s *Server) uploadIcon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, 405, "Method not allowed")
		return
	}
	if s.icons == nil {
		fail(w, 503, "Icon storage unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxBytes+(64<<10))
	mr, err := r.MultipartReader()
	if err != nil {
		fail(w, 400, "Multipart file required")
		return
	}
	part, err := mr.NextPart()
	if err != nil || part.FormName() != "file" {
		fail(w, 400, "One icon file is required")
		return
	}
	body, err := io.ReadAll(io.LimitReader(part, media.MaxBytes+1))
	part.Close()
	if err != nil || int64(len(body)) > media.MaxBytes {
		fail(w, 413, "Icon exceeds the size limit")
		return
	}
	if next, e := mr.NextPart(); e != io.EOF {
		if next != nil {
			next.Close()
		}
		fail(w, 400, "Only one icon file is allowed")
		return
	}
	path, err := s.icons.Put(bytes.NewReader(body))
	if err != nil {
		if errors.Is(err, media.ErrTooLarge) {
			fail(w, 413, "Icon exceeds the size limit")
		} else if errors.Is(err, media.ErrInvalidIcon) {
			fail(w, 400, "Unsupported or unsafe icon")
		} else {
			fail(w, 503, "Unable to save icon")
		}
		return
	}
	reply(w, 201, map[string]string{"icon": path})
}

func (s *Server) icon(w http.ResponseWriter, r *http.Request) {
	if s.icons == nil {
		fail(w, 404, "Icon not found")
		return
	}
	f, mime, err := s.icons.Open(r.URL.Path)
	if err != nil {
		fail(w, 404, "Icon not found")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fail(w, 503, "Icon unavailable")
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
