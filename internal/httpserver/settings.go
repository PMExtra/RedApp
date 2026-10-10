package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/PMExtra/RedApp/internal/config"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/site"
	"github.com/PMExtra/RedApp/internal/store"
)

// settingsWriteError maps a failed CAS save of a settings document.
func settingsWriteError(err error) *apiError {
	if errors.Is(err, store.ErrConflict) {
		return revisionConflict(err)
	}
	return storageError(err)
}

// ---------------------------------------------------------------- site

// siteSettingsStateDTO is SiteSettingsState: the public site texts and their revision.
type siteSettingsStateDTO struct {
	siteSettingsDTO
	Revision int64 `json:"revision"`
}

func siteDocument(v site.Snapshot) siteSettingsStateDTO {
	return siteSettingsStateDTO{siteSettingsDTO: siteSettings(v.Settings), Revision: v.Revision}
}

func (s *Server) getSiteSettings(w http.ResponseWriter, r *http.Request) {
	v, err := site.LoadSnapshot(s.store)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	writeRevision(w, http.StatusOK, v.Revision, siteDocument(v))
}

// localizedInput is a LocalizedText request value whose languages are required.
type localizedInput struct {
	En   *string `json:"en"`
	ZhCN *string `json:"zh-CN"`
}

func (t *localizedInput) text() (site.Text, bool) {
	if t == nil || t.En == nil || t.ZhCN == nil {
		return site.Text{}, false
	}
	return site.Text{EN: *t.En, ZHCN: *t.ZhCN}, true
}

func (s *Server) replaceSiteSettings(w http.ResponseWriter, r *http.Request) {
	revision, e := ifMatch(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	var input struct {
		Title      *localizedInput `json:"title"`
		Subtitle   *localizedInput `json:"subtitle"`
		Disclaimer *localizedInput `json:"disclaimer"`
	}
	if e = decodeJSON(r, &input); e != nil {
		s.writeError(w, r, e)
		return
	}
	title, okTitle := input.Title.text()
	subtitle, okSubtitle := input.Subtitle.text()
	disclaimer, okDisclaimer := input.Disclaimer.text()
	if !okTitle || !okSubtitle || !okDisclaimer {
		s.fail(w, r, codeInvalidRequest, nil, "title, subtitle and disclaimer are required in en and zh-CN")
		return
	}
	value := site.Settings{Title: title, Subtitle: subtitle, Disclaimer: disclaimer}
	if err := value.Validate(); err != nil {
		s.fail(w, r, codeValidationFailed, nil, "Title needs 1 to 80 characters, subtitle at most 160 and disclaimer at most 500, without control characters other than newline and tab")
		return
	}
	saved, err := site.SaveCAS(s.store, value, revision)
	if err != nil {
		s.writeError(w, r, settingsWriteError(err))
		return
	}
	writeRevision(w, http.StatusOK, saved.Revision, siteDocument(saved))
}

// ---------------------------------------------------------------- public URL

type publicURLStateDTO struct {
	OverrideURL    *string `json:"override_url"`
	EnvironmentURL *string `json:"environment_url"`
	EffectiveURL   string  `json:"effective_url"`
	Source         string  `json:"source"`
	Revision       int64   `json:"revision"`
}

func (s *Server) writePublicURL(w http.ResponseWriter, r *http.Request) {
	v := s.public.View(requestState(r).origin)
	writeRevision(w, http.StatusOK, v.Revision, publicURLStateDTO{OverrideURL: v.OverrideURL, EnvironmentURL: v.EnvironmentURL, EffectiveURL: v.EffectiveURL, Source: v.Source, Revision: v.Revision})
}

func (s *Server) getPublicUrlSettings(w http.ResponseWriter, r *http.Request) {
	s.writePublicURL(w, r)
}

func (s *Server) replacePublicUrlSettings(w http.ResponseWriter, r *http.Request) {
	revision, e := ifMatch(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	var input struct {
		OverrideURL json.RawMessage `json:"override_url"`
	}
	if e = decodeJSONNullable(r, &input); e != nil {
		s.writeError(w, r, e)
		return
	}
	if input.OverrideURL == nil {
		s.fail(w, r, codeInvalidRequest, nil, "override_url is required; use null to clear it")
		return
	}
	var override *string
	if err := json.Unmarshal(input.OverrideURL, &override); err != nil {
		s.fail(w, r, codeInvalidRequest, nil, "override_url must be a string or null")
		return
	}
	if override != nil {
		if clean, err := config.PublicURL(*override); err != nil || clean == "" || len(*override) > 2048 {
			s.fail(w, r, codeValidationFailed, nil, "override_url must be an http(s) origin without credentials, path, query or fragment")
			return
		}
	}
	if _, err := s.public.Set(override, revision); err != nil {
		s.writeError(w, r, settingsWriteError(err))
		return
	}
	s.writePublicURL(w, r)
}

// ---------------------------------------------------------------- global proxy

type globalProxyStateDTO struct {
	Mode     string `json:"mode"`
	URL      string `json:"url,omitempty"`
	DNS      string `json:"dns"`
	Revision int64  `json:"revision"`
}

func (s *Server) writeGlobalProxy(w http.ResponseWriter) {
	v := s.pool.Proxy().Redacted()
	writeRevision(w, http.StatusOK, v.Revision, globalProxyStateDTO{Mode: v.Mode, URL: v.URL, DNS: v.DNS, Revision: v.Revision})
}

func (s *Server) getGlobalProxySettings(w http.ResponseWriter, r *http.Request) {
	s.writeGlobalProxy(w)
}

func (s *Server) replaceGlobalProxySettings(w http.ResponseWriter, r *http.Request) {
	revision, e := ifMatch(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	var input struct {
		Mode *string `json:"mode"`
		URL  *string `json:"url"`
	}
	if e = decodeJSON(r, &input); e != nil {
		s.writeError(w, r, e)
		return
	}
	if input.Mode == nil {
		s.fail(w, r, codeInvalidRequest, nil, "mode is required")
		return
	}
	update := distributor.ProxyUpdate{Mode: *input.Mode}
	switch {
	case *input.Mode != "direct" && *input.Mode != "url":
		s.fail(w, r, codeValidationFailed, nil, "mode must be direct or url")
		return
	case (*input.Mode == "url") != (input.URL != nil):
		s.fail(w, r, codeValidationFailed, nil, "url is required with and only allowed with mode url")
		return
	case input.URL != nil:
		update.URL = *input.URL
	}
	err := s.pool.SetProxy(update, revision)
	switch {
	case errors.Is(err, networkproxy.ErrRedactedMismatch):
		s.fail(w, r, codeProxyRedactedMismatch, nil, "The redacted password **** can only be kept for the saved proxy scheme, user and host")
	case errors.Is(err, distributor.ErrInvalidProxySettings):
		s.fail(w, r, codeValidationFailed, nil, "url must be an http, https or socks5 proxy with an explicit port and no path, query or fragment")
	case err != nil:
		s.writeError(w, r, settingsWriteError(err))
	default:
		s.writeGlobalProxy(w)
	}
}
