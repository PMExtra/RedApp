package httpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/config"
	"github.com/PMExtra/RedApp/internal/site"
)

func (s *Server) publicApplications(origin string) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(s.Registry.Entries()))
	for _, e := range s.Registry.Entries() {
		item, err := s.publicApplication(e, origin)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}
func (s *Server) publicApplication(e application.Entry, origin string) (map[string]any, error) {
	usage, err := s.DB.Instructions(e.UID)
	if err != nil {
		return nil, err
	}
	d := e.Descriptor
	root := origin + "/" + d.ID
	icon := ""
	if d.Icon != "" {
		if strings.HasPrefix(d.Icon, "/") {
			icon = d.Icon
		} else {
			icon = "/" + d.ID + "/" + d.Icon
		}
	}
	definition, _ := application.ProviderDefinition(e.Provider)
	item := map[string]any{"id": d.ID, "name": d.Name, "publisher": d.Publisher, "vendor": map[string]any{"id": e.VendorID, "name": e.VendorName, "description": e.VendorDescription, "icon": e.VendorIcon}, "summary": d.Summary, "origin": root, "detail_url": "/" + d.ID, "distribution_url": root, "icon": icon, "channels": d.Channels, "installers": publicInstallers(d.Installers), "update_policy": d.UpdatePolicy, "provider": e.Provider, "capabilities": definition.Capabilities, "instructions": usage.LocalizedText}
	if definition.Capabilities.Versions && e.Protocol != nil {
		latest, discovered, err := s.latestKnownVersion(e)
		if err != nil {
			return nil, err
		}
		item["latest_known_version"] = nil
		if latest != "" {
			item["latest_known_version"] = map[string]any{"version": latest, "first_seen": discovered}
		}
	}

	if !definition.Capabilities.Files {
		delete(item, "distribution_url")
	}
	return item, nil
}

// Read only locally observed versions for this source epoch; never synchronize upstream.
func (s *Server) latestKnownVersion(e application.Entry) (string, *time.Time, error) {
	if e.Protocol == nil {
		return "", nil, nil
	}
	versions, err := s.DB.VersionsFor(e.StorageID())
	if err != nil {
		return "", nil, err
	}
	var latest string
	for version := range versions {
		if _, err := e.Protocol.ValidateVersion(version); err != nil {
			continue
		}
		if latest == "" {
			latest = version
			continue
		}
		if order, err := e.Protocol.CompareVersions(version, latest); err == nil && (order > 0 || order == 0 && version < latest) {
			latest = version
		}
	}
	var discovered *time.Time
	if at, err := time.Parse(time.RFC3339, versions[latest]); latest != "" && err == nil && at.Unix() > 0 {
		discovered = &at
	}
	return latest, discovered, nil
}

func publicInstallers(items []application.Installer) []map[string]string {
	out := make([]map[string]string, 0, len(items))
	for _, item := range items {
		label := "Shell"
		if item.Shell == "powershell" {
			label = "PowerShell"
		}
		out = append(out, map[string]string{"file": item.File, "shell": item.Shell, "runner": item.Shell, "label": label})
	}
	return out
}
func (s *Server) publicAPI(w http.ResponseWriter, r *http.Request, publicView config.PublicView) {
	if s.instructionsDocument(w, r, publicView.EffectiveURL) || s.publicCatalogAPI(w, r, publicView.EffectiveURL) {
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/apps/") && strings.HasSuffix(r.URL.Path, "/files") {
		key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/apps/"), "/files")
		entry, ok := s.Registry.Lookup(key)
		if !ok || entry.Provider != application.Hosted {
			fail(w, 404, "Application files not found")
			return
		}
		if r.Method != http.MethodGet || !queryAllowed(r, "page", "limit") {
			fail(w, 400, "Invalid file listing")
			return
		}
		page, valid := positivePage(r.URL.Query().Get("page"), 1)
		limit, validLimit := positivePage(r.URL.Query().Get("limit"), 25)
		if !valid || !validLimit || limit > 100 {
			fail(w, 400, "Invalid file page")
			return
		}
		value, err := s.DB.HostedPage(entry.UID, page, limit)
		if err != nil {
			fail(w, 503, "Files unavailable")
			return
		}
		reply(w, 200, value)
		return
	}

	if r.Method != "GET" {
		fail(w, 405, "Method not allowed")
		return
	}
	if !queryAllowed(r) {
		fail(w, 400, "Invalid query")
		return
	}
	public := publicView.EffectiveURL
	apps, err := s.publicApplications(public)
	if err != nil {
		fail(w, 503, "Application instructions unavailable")
		return
	}
	switch r.URL.Path {
	case "/api/bootstrap":
		settings, err := site.LoadSnapshot(s.DB)
		if err != nil {
			fail(w, 503, "Site settings unavailable")
			return
		}
		version := s.Version
		if version == "" {
			version = "dev"
		}
		publicRevision := publicView.Revision
		identity, _ := json.Marshal([]any{version, settings.Revision, publicRevision, public, apps})
		digest := sha256.Sum256(identity)
		reply(w, 200, map[string]any{"version": version, "os": runtime.GOOS, "arch": runtime.GOARCH, "site": settings.Settings, "apps": apps, "public_origin": public, "revision": hex.EncodeToString(digest[:])})
		return
	case "/api/apps":
		reply(w, 200, apps)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/apps/") {
		id := strings.TrimPrefix(r.URL.Path, "/api/apps/")
		for _, entry := range apps {
			if entry["id"] == id {
				reply(w, 200, entry)
				return
			}
		}
		problem(w, 404, "APPLICATION_NOT_FOUND", "Application not found")
		return
	}
	fail(w, 404, "API endpoint not found")
}
