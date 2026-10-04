package httpserver

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/media"
	"github.com/PMExtra/RedApp/internal/store"
)

// ReloadDirectory publishes one complete runtime snapshot. Callers serialize
// mutations with directoryMu; source fences reject work from replaced snapshots.
func (s *Server) ReloadDirectory() error {
	if s.Pool == nil {
		return errors.New("Application transport unavailable")
	}
	vendors, err := s.DB.Vendors(true)
	if err != nil {
		return err
	}
	apps, err := s.DB.Applications(true)
	if err != nil {
		return err
	}
	next, err := builtin.NewDynamic(vendors, apps, s.Pool)
	if err != nil {
		return err
	}
	sources, err := s.DB.Sources()
	if err != nil {
		return err
	}
	clients := make(map[string]*distributor.Client, len(sources))
	for _, source := range sources {
		client, e := builtin.NewSourceClient(source.Provider, source.BaseURL, s.Pool)
		if e != nil {
			return e
		}
		clients[source.StorageID()] = client
	}
	if s.Downloads != nil {
		if err = s.Downloads.RegisterUpstreams(clients); err != nil {
			return err
		}
	}
	return s.Registry.Replace(next.AllEntries())
}

func (s *Server) setDirectoryTTL(key string, expected int64, seconds int) (int64, error) {
	s.directoryMu.Lock()
	defer s.directoryMu.Unlock()
	row, err := s.DB.Application(key)
	if err != nil {
		return 0, err
	}
	row, err = s.DB.UpdateApplication(key, expected, store.ApplicationChanges{Name: row.Name, Description: row.Description, Icon: row.Icon, BaseURL: row.BaseURL, BaseURLs: row.BaseURLs, SourceStrategy: row.SourceStrategy, CacheTTLSeconds: seconds, Enabled: row.Enabled})
	if err != nil {
		return 0, err
	}
	if err = s.ReloadDirectory(); err != nil {
		return 0, err
	}
	return row.Revision, nil
}

type directoryInput struct {
	Revision        int64                `json:"revision"`
	ID              string               `json:"id"`
	Name            *store.LocalizedText `json:"name"`
	Description     *store.LocalizedText `json:"description"`
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
	d, ok := application.ProviderDefinition(in.Provider)
	if !ok {
		return store.ApplicationInput{}, store.ErrInvalidDirectory
	}
	v := in.vendorInput()
	a := store.ApplicationInput{ID: v.ID, Name: v.Name, Description: v.Description, Icon: v.Icon, Enabled: v.Enabled, Provider: in.Provider, CacheTTLSeconds: d.DefaultCacheTTLSeconds}
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
	if path == "" || path == "/openai/codex/icon.svg" {
		return nil
	}
	if s.Icons == nil {
		return store.ErrInvalidDirectory
	}
	f, _, err := s.Icons.Open(path)
	if err != nil {
		return fmt.Errorf("%w: upload the icon before selecting it", store.ErrInvalidDirectory)
	}
	return f.Close()
}

func directoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrRevisionConflict):
		problem(w, 409, "DIRECTORY_REVISION_CONFLICT", "Configuration changed; reload before saving")
	case errors.Is(err, sql.ErrNoRows):
		problem(w, 404, "DIRECTORY_NOT_FOUND", "Vendor or application not found")
	case errors.Is(err, store.ErrDirectoryExists), errors.Is(err, store.ErrDirectoryDeleted), errors.Is(err, store.ErrVendorHasApplications):
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
			row, err = s.DB.Vendor(parts[1])
			result = map[string]any{"vendor": row}
		case isApp && len(parts) == 3:
			var row store.Application
			row, err = s.DB.Application(parts[1] + "/" + parts[2])
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
	if s.Pool == nil {
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
	s.directoryMu.Lock()
	defer s.directoryMu.Unlock()
	var result any
	var err error
	status := 200
	switch {
	case create && endpoint == "vendors":
		if in.Provider != "" || in.BaseURL != nil || in.BaseURLs != nil || in.SourceStrategy != nil || in.CacheTTLSeconds != nil {
			fail(w, 400, "Unexpected vendor fields")
			return true
		}
		var row store.Vendor
		row, err = s.DB.CreateVendor(in.vendorInput())
		result = map[string]any{"vendor": row}
		status = 201
	case create:
		var input store.ApplicationInput
		input, err = in.applicationInput()
		if err == nil {
			var row store.Application
			row, err = s.DB.CreateApplication(parts[1], input)
			result = map[string]any{"app": row}
			status = 201
		}
	case isVendor && len(parts) == 2:
		if in.BaseURL != nil || in.BaseURLs != nil || in.SourceStrategy != nil || in.CacheTTLSeconds != nil {
			fail(w, 400, "Unexpected vendor fields")
			return true
		}
		var row store.Vendor
		row, err = s.DB.Vendor(parts[1])
		if err != nil {
			break
		}
		if r.Method == http.MethodDelete {
			err = s.DB.DeleteVendor(parts[1], in.Revision)
			if err == nil {
				row, err = s.DB.Vendor(parts[1])
			}
		} else if r.Method == http.MethodPatch {
			changes := store.VendorChanges{Name: row.Name, Description: row.Description, Icon: row.Icon, Enabled: row.Enabled}
			if in.Name != nil {
				changes.Name = *in.Name
			}
			if in.Description != nil {
				changes.Description = *in.Description
			}
			if in.Icon != nil {
				changes.Icon = *in.Icon
			}
			if in.Enabled != nil {
				changes.Enabled = *in.Enabled
			}
			row, err = s.DB.UpdateVendor(parts[1], in.Revision, changes)
		} else {
			fail(w, 405, "Method not allowed")
			return true
		}
		result = map[string]any{"vendor": row}
	case isApp && len(parts) == 3:
		key := parts[1] + "/" + parts[2]
		var row store.Application
		row, err = s.DB.Application(key)
		if err != nil {
			break
		}
		if r.Method == http.MethodDelete {
			err = s.DB.DeleteApplication(key, in.Revision)
			if err == nil {
				row, err = s.DB.Application(key)
			}
		} else if r.Method == http.MethodPatch {
			input := store.ApplicationInput{ID: row.ID, Name: row.Name, Description: row.Description, Icon: row.Icon, Provider: row.Provider, BaseURL: row.BaseURL, BaseURLs: row.BaseURLs, SourceStrategy: row.SourceStrategy, CacheTTLSeconds: row.CacheTTLSeconds, Enabled: row.Enabled}
			if in.Name != nil {
				input.Name = *in.Name
			}
			if in.Description != nil {
				input.Description = *in.Description
			}
			if in.Icon != nil {
				input.Icon = *in.Icon
			}
			if in.Enabled != nil {
				input.Enabled = *in.Enabled
			}
			if in.BaseURL != nil {
				input.BaseURL = *in.BaseURL
				if row.Provider == application.HttpCache {
					var base string
					base, err = distributor.NormalizeBase(*in.BaseURL, distributor.GeneralHTTP)
					if err != nil {
						err = fmt.Errorf("%w: %v", store.ErrInvalidDirectory, err)
						break
					}
					if base != row.BaseURL {
						input.BaseURLs = nil
					}
				}
			}
			if in.BaseURLs != nil {
				input.BaseURLs = append([]string{}, (*in.BaseURLs)...)
			}
			if in.SourceStrategy != nil {
				input.SourceStrategy = *in.SourceStrategy
			}
			if in.BaseURL != nil && in.BaseURLs != nil {
				err = fmt.Errorf("%w: provide base_urls or base_url, not both", store.ErrInvalidDirectory)
				break
			}
			if in.CacheTTLSeconds != nil {
				input.CacheTTLSeconds = *in.CacheTTLSeconds
			}
			input, err = normalizedApplication(input)
			if err == nil {
				row, err = s.DB.UpdateApplication(key, in.Revision, store.ApplicationChanges{Name: input.Name, Description: input.Description, Icon: input.Icon, BaseURL: input.BaseURL, BaseURLs: input.BaseURLs, SourceStrategy: input.SourceStrategy, CacheTTLSeconds: input.CacheTTLSeconds, Enabled: input.Enabled})
			}
		} else {
			fail(w, 405, "Method not allowed")
			return true
		}
		result = map[string]any{"app": row}
	default:
		fail(w, 405, "Method not allowed")
		return true
	}
	if err == nil {
		err = s.ReloadDirectory()
	}
	if err != nil {
		directoryError(w, err)
	} else {
		reply(w, status, result)
	}
	return true
}

func (s *Server) uploadIcon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, 405, "Method not allowed")
		return
	}
	if s.Icons == nil {
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
	path, err := s.Icons.Put(bytes.NewReader(body))
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
	if s.Icons == nil {
		fail(w, 404, "Icon not found")
		return
	}
	f, mime, err := s.Icons.Open(r.URL.Path)
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
