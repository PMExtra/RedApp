package httpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/site"
	"github.com/PMExtra/RedApp/internal/store"
)

// publicURL is the effective public URL (no trailing "/") for links in
// responses: override, environment or the verified request origin.
func (s *Server) publicURL(r *http.Request) string {
	return s.public.View(requestState(r).origin).EffectiveURL
}

type siteSettingsDTO struct {
	Title      localizedText `json:"title"`
	Subtitle   localizedText `json:"subtitle"`
	Disclaimer localizedText `json:"disclaimer"`
}

func siteSettings(v site.Settings) siteSettingsDTO {
	text := func(t site.Text) localizedText { return localizedText{En: t.EN, ZhCN: t.ZHCN} }
	return siteSettingsDTO{Title: text(v.Title), Subtitle: text(v.Subtitle), Disclaimer: text(v.Disclaimer)}
}

type bootstrapDTO struct {
	Version   string          `json:"version"`
	OS        string          `json:"os"`
	Arch      string          `json:"arch"`
	Site      siteSettingsDTO `json:"site"`
	PublicURL string          `json:"public_url"`
	Revision  string          `json:"revision"`
}

func (s *Server) getBootstrap(w http.ResponseWriter, r *http.Request) {
	settings, err := site.LoadSnapshot(s.store)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	taxonomy, err := s.store.TaxonomyPublicRevision()
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	view := s.public.View(requestState(r).origin)
	out := bootstrapDTO{Version: s.version, OS: runtime.GOOS, Arch: runtime.GOARCH, Site: siteSettings(settings.Settings), PublicURL: view.EffectiveURL}
	identity, _ := json.Marshal([]any{out.Version, settings.Revision, view.Revision, view.EffectiveURL, taxonomy})
	digest := sha256.Sum256(identity)
	out.Revision = hex.EncodeToString(digest[:])
	writeOK(w, out)
}

type rankingEntryDTO struct {
	App             publicAppDTO `json:"app"`
	DownloadClients int64        `json:"download_clients"`
}
type homeDTO struct {
	Pinned      []publicAppDTO    `json:"pinned"`
	Ranking     []rankingEntryDTO `json:"ranking"`
	WindowHours int               `json:"window_hours"`
}

func (s *Server) getHome(w http.ResponseWriter, r *http.Request) {
	taxonomy, _, err := s.store.PublicTaxonomy()
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	pins, err := s.store.HomepagePins()
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	scores, err := s.store.DownloadRanking(time.Now(), 20)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	publicURL := s.publicURL(r)
	out := homeDTO{Pinned: []publicAppDTO{}, Ranking: []rankingEntryDTO{}, WindowHours: 168}
	for _, key := range pins.Keys {
		if e, ok := s.registry.Lookup(key); ok {
			app, err := s.publicApp(e, publicURL, taxonomy[e.UID])
			if err != nil {
				s.writeError(w, r, storageError(err))
				return
			}
			out.Pinned = append(out.Pinned, app)
		}
	}
	byUID := map[string]application.Entry{}
	for _, e := range s.registry.Entries() {
		byUID[e.UID] = e
	}
	for _, score := range scores {
		e, ok := byUID[score.UID]
		if !ok || score.Clients < 1 {
			continue
		}
		app, err := s.publicApp(e, publicURL, taxonomy[e.UID])
		if err != nil {
			s.writeError(w, r, storageError(err))
			return
		}
		out.Ranking = append(out.Ranking, rankingEntryDTO{App: app, DownloadClients: score.Clients})
	}
	writeOK(w, out)
}

type catalogPageDTO struct {
	Items      []publicAppDTO     `json:"items"`
	Page       int                `json:"page"`
	Limit      int                `json:"limit"`
	Total      int64              `json:"total"`
	TotalPages int                `json:"total_pages"`
	Categories []categoryCountDTO `json:"categories"`
}

func (s *Server) listCatalog(w http.ResponseWriter, r *http.Request) {
	q, e := queryText(r, "q", 128)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	page, limit, e := pageQuery(r, 24)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	vendor, category := r.URL.Query().Get("vendor"), r.URL.Query().Get("category")
	if vendor != "" && !identity.ValidVendor(vendor) {
		s.fail(w, r, codeInvalidQuery, nil, "vendor must be a vendor ID")
		return
	}
	if category != "" && !identity.ValidSlug(category) {
		s.fail(w, r, codeInvalidQuery, nil, "category must be a category ID")
		return
	}
	if vendor != "" {
		if _, ok := s.publishedVendor(w, r, vendor); !ok {
			return
		}
	}
	taxonomy, counts, err := s.store.PublicTaxonomy()
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	apps, err := s.store.ApplicationCategoryPage(vendor, page, limit, q, category)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	publicURL := s.publicURL(r)
	out := catalogPageDTO{Items: []publicAppDTO{}, Page: apps.Page, Limit: apps.Limit, Total: apps.Total, TotalPages: apps.TotalPages, Categories: make([]categoryCountDTO, 0, len(counts))}
	for _, a := range apps.Items {
		if entry, ok := s.registry.Lookup(a.Key); ok {
			app, err := s.publicApp(entry, publicURL, taxonomy[entry.UID])
			if err != nil {
				s.writeError(w, r, storageError(err))
				return
			}
			out.Items = append(out.Items, app)
		}
	}
	for _, c := range counts {
		out.Categories = append(out.Categories, categoryCountDTO{ID: c.ID, Name: fromStoreText(c.Name), Count: c.Count})
	}
	writeOK(w, out)
}

type searchHitDTO struct {
	Kind           string         `json:"kind"`
	Key            string         `json:"key"`
	Name           localizedText  `json:"name"`
	Icon           string         `json:"icon"`
	LocalizedIcons *localizedText `json:"localized_icons"`
}

func (s *Server) searchCatalog(w http.ResponseWriter, r *http.Request) {
	q, e := queryText(r, "q", 128)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	hits := []searchHitDTO{}
	if q == "" {
		writeOK(w, map[string][]searchHitDTO{"items": hits})
		return
	}
	vendors, err := s.store.SearchVendors(q, 4)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	for _, v := range vendors {
		dto := publicVendorFromStore(v)
		hits = append(hits, searchHitDTO{Kind: "vendor", Key: v.ID, Name: dto.Name, Icon: dto.Icon, LocalizedIcons: &dto.LocalizedIcons})
	}
	apps, err := s.store.ApplicationPage("", 1, 6, q, "enabled")
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	for _, a := range apps.Items {
		if entry, ok := s.registry.Lookup(a.Key); ok {
			hits = append(hits, searchHitDTO{Kind: "app", Key: a.Key, Name: fromLocalized(entry.Descriptor.Name), Icon: publicIcon(entry.Descriptor.Icon)})
		}
	}
	writeOK(w, map[string][]searchHitDTO{"items": hits})
}

// publishedVendor reads an enabled, not deleted vendor or writes VENDOR_NOT_FOUND.
func (s *Server) publishedVendor(w http.ResponseWriter, r *http.Request, id string) (store.Vendor, bool) {
	v, err := s.store.Vendor(id)
	if err != nil && !isNotFound(err) {
		s.writeError(w, r, storageError(err))
		return v, false
	}
	if err != nil || !v.Enabled || v.DeletedAt != nil {
		s.fail(w, r, codeVendorNotFound, nil, "Vendor not found")
		return v, false
	}
	return v, true
}

func (s *Server) getPublicVendor(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("vendor")
	if !identity.ValidVendor(id) {
		s.fail(w, r, codeInvalidPath, nil, "Invalid vendor ID")
		return
	}
	if v, ok := s.publishedVendor(w, r, id); ok {
		writeOK(w, publicVendorFromStore(v))
	}
}

// publishedApp resolves {vendor}/{app} to a published application or writes
// INVALID_PATH / APPLICATION_NOT_FOUND.
func (s *Server) publishedApp(w http.ResponseWriter, r *http.Request) (application.Entry, bool) {
	key := r.PathValue("vendor") + "/" + r.PathValue("app")
	if _, err := application.ParseKey(key); err != nil {
		s.fail(w, r, codeInvalidPath, nil, "Invalid application identity")
		return application.Entry{}, false
	}
	e, ok := s.registry.Lookup(key)
	if !ok {
		s.fail(w, r, codeApplicationNotFound, nil, "Application not found")
	}
	return e, ok
}

func (s *Server) getPublicApp(w http.ResponseWriter, r *http.Request) {
	e, ok := s.publishedApp(w, r)
	if !ok {
		return
	}
	taxonomy, _, err := s.store.PublicTaxonomy()
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	app, err := s.publicApp(e, s.publicURL(r), taxonomy[e.UID])
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	writeOK(w, app)
}

func (s *Server) listPublicHostedFiles(w http.ResponseWriter, r *http.Request) {
	e, ok := s.publishedApp(w, r)
	if !ok {
		return
	}
	if e.Provider != application.Hosted {
		s.fail(w, r, codeCapabilityUnsupported, nil, "This application does not host files")
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
