package httpserver

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/PMExtra/RedApp/internal/store"
)

type fieldOriginDTO struct {
	Source  string `json:"source"`
	Differs *bool  `json:"differs_from_template"`
}

type proxyEffectiveDTO struct {
	Mode        string `json:"mode"`
	URL         string `json:"url,omitempty"`
	SourceScope string `json:"source_scope"`
	SourceID    string `json:"source_id"`
	DNS         string `json:"dns"`
}

// configurationDTO is VendorConfiguration or AppConfiguration. defaults,
// overrides and effective are spec documents projected to the fields of the
// entity kind and provider, with proxy passwords redacted.
type configurationDTO struct {
	Revision        int64                     `json:"revision"`
	TemplateRef     *string                   `json:"template_ref"`
	TemplateHash    *string                   `json:"template_hash"`
	TemplateMissing bool                      `json:"template_missing"`
	Defaults        map[string]any            `json:"defaults"`
	Overrides       map[string]any            `json:"overrides"`
	Effective       map[string]any            `json:"effective"`
	Fields          map[string]fieldOriginDTO `json:"fields"`
	ProxyEffective  proxyEffectiveDTO         `json:"proxy_effective"`
}

var vendorSpecFields = []string{"name", "description", "icon", "localized_icons", "proxy"}
var appSpecFields = []string{"name", "description", "icon", "provider", "proxy", "categories", "tags", "instructions", "base_url", "base_urls", "source_strategy", "cache_ttl_seconds", "http_policy", "retention", "prewarm"}

// specDocument keeps the spec fields of the kind that apply to provider
// ("" for vendors). Lists that are absent or null become []. A nil spec
// (no template) stays nil.
func specDocument(spec store.Object, provider string, sparse bool) map[string]any {
	if spec == nil {
		return nil
	}
	fields := vendorSpecFields
	if provider != "" {
		fields = appSpecFields
	}
	out := map[string]any{}
	for _, field := range fields {
		value, ok := spec[field]
		if provider != "" && !store.AppPathApplies(provider, field) {
			continue
		}
		if !sparse && (field == "categories" || field == "tags" || field == "base_urls") && value == nil {
			value, ok = []any{}, true
		}
		if ok && value != nil {
			out[field] = value
		}
	}
	if proxy, ok := out["proxy"]; ok {
		out["proxy"] = redactProxyValue(proxy)
	}
	return out
}

func configurationDocument(c store.Configuration, provider string) configurationDTO {
	out := configurationDTO{Revision: c.Revision, TemplateRef: c.TemplateRef, TemplateHash: c.TemplateHash, TemplateMissing: c.TemplateMissing,
		Defaults: specDocument(c.Defaults, provider, false), Overrides: specDocument(c.Overrides, provider, true), Effective: specDocument(c.Effective, provider, false),
		Fields: map[string]fieldOriginDTO{}}
	if out.Overrides == nil {
		out.Overrides = map[string]any{}
	}
	for path, origin := range c.Fields {
		if provider == "" || store.AppPathApplies(provider, path) {
			out.Fields[path] = fieldOriginDTO{Source: origin.Source, Differs: origin.Differs}
		}
	}
	proxy := c.ProxyEffective.Config.Redacted()
	out.ProxyEffective = proxyEffectiveDTO{Mode: proxy.Mode, URL: proxy.URL, SourceScope: c.ProxyEffective.SourceScope, SourceID: c.ProxyEffective.SourceID, DNS: c.ProxyEffective.DNS}
	return out
}

func (s *Server) getVendorConfiguration(w http.ResponseWriter, r *http.Request) {
	id, e := vendorParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	c, err := s.store.VendorConfiguration(id)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeVendorNotFound))
		return
	}
	writeRevision(w, http.StatusOK, c.Revision, configurationDocument(c, ""))
}

func (s *Server) getAppConfiguration(w http.ResponseWriter, r *http.Request) {
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
	c, err := s.store.ApplicationConfiguration(key)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeApplicationNotFound))
		return
	}
	writeRevision(w, http.StatusOK, c.Revision, configurationDocument(c, a.Provider))
}

type configurationPatchRequest struct {
	Set           map[string]json.RawMessage `json:"set"`
	Unset         []string                   `json:"unset"`
	NewCategories []string                   `json:"new_categories"`
}

// configurationPatch reads If-Match and a VendorConfigurationPatch or
// AppConfigurationPatch body with the given set paths.
func (s *Server) configurationPatch(r *http.Request, paths []string, app bool) (store.ConfigurationPatch, *apiError) {
	revision, e := ifMatch(r)
	if e != nil {
		return store.ConfigurationPatch{}, e
	}
	var in configurationPatchRequest
	if e = decodeJSON(r, &in); e != nil {
		return store.ConfigurationPatch{}, e
	}
	if !app && in.NewCategories != nil {
		return store.ConfigurationPatch{}, newError(codeInvalidRequest, nil, "Unknown field new_categories")
	}
	for path, raw := range in.Set {
		if !slices.Contains(paths, path) {
			return store.ConfigurationPatch{}, newError(codeInvalidRequest, nil, "Unknown configuration path "+path)
		}
		if path == "icon" || strings.HasPrefix(path, "localized_icons.") {
			var icon string
			if json.Unmarshal(raw, &icon) != nil {
				return store.ConfigurationPatch{}, newError(codeValidationFailed, nil, path+" must be an icon path")
			}
			if e = s.validateIcon(path, icon); e != nil {
				return store.ConfigurationPatch{}, e
			}
		}
	}
	seen := map[string]bool{}
	for _, path := range in.Unset {
		if !slices.Contains(paths, path) || seen[path] {
			return store.ConfigurationPatch{}, newError(codeValidationFailed, nil, "unset contains an unknown or repeated path "+path)
		}
		if _, ok := in.Set[path]; ok {
			return store.ConfigurationPatch{}, newError(codeValidationFailed, nil, path+" cannot be both set and unset")
		}
		seen[path] = true
	}
	return store.ConfigurationPatch{Revision: revision, Set: in.Set, Unset: in.Unset, NewCategories: in.NewCategories}, nil
}

var vendorConfigurationPaths = []string{"name.en", "name.zh-CN", "description.en", "description.zh-CN", "icon", "localized_icons.en", "localized_icons.zh-CN", "proxy"}
var appConfigurationPaths = []string{"name.en", "name.zh-CN", "description.en", "description.zh-CN", "icon", "proxy", "instructions.en", "instructions.zh-CN", "base_url", "base_urls", "source_strategy", "cache_ttl_seconds", "http_policy.rules", "http_policy.auto_cleanup", "http_policy.stale_fallback", "retention", "prewarm", "categories", "tags"}

func (s *Server) patchVendorConfiguration(w http.ResponseWriter, r *http.Request) {
	id, e := vendorParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	patch, e := s.configurationPatch(r, vendorConfigurationPaths, false)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	c, err := s.store.PatchVendorConfiguration(id, patch)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeVendorNotFound))
		return
	}
	writeRevision(w, http.StatusOK, c.Revision, configurationDocument(c, ""))
}

func (s *Server) patchAppConfiguration(w http.ResponseWriter, r *http.Request) {
	key, e := appParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	patch, e := s.configurationPatch(r, appConfigurationPaths, true)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	a, err := s.store.Application(key)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeApplicationNotFound))
		return
	}
	c, err := s.store.PatchApplicationConfiguration(key, patch)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeApplicationNotFound))
		return
	}
	writeRevision(w, http.StatusOK, c.Revision, configurationDocument(c, a.Provider))
}
