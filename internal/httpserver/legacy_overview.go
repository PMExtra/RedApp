package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/PMExtra/RedApp/internal/config"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/history"
	"github.com/PMExtra/RedApp/internal/site"
)

// legacyOverviewAdmin is the pre-contract dispatcher of migration package 3
// (HTTP cache administration, overview, events, history, settings). Return
// false unconditionally once the package is migrated, then delete this file
// together with legacy.go.
func (s *Server) legacyOverviewAdmin(w http.ResponseWriter, r *http.Request, app, endpoint, requestOrigin string) bool {
	if s.cacheAPI(w, r, app, endpoint) {
		return true
	}
	if app == "" && endpoint == "settings/homepage" {
		s.homepageAPI(w, r)
		return true
	}
	public := s.public.View(requestOrigin).EffectiveURL
	switch r.Method {
	case http.MethodGet:
		switch endpoint {
		case "status":
			var result map[string]any
			var err error
			if app == "" {
				result, err = s.status(public)
			} else {
				result, err = s.appStatus(app, public)
			}
			if err != nil {
				fail(w, 503, "Failed to read status")
				return true
			}
			for _, key := range []string{"resources", "events", "versions", "version_stats", "application_versions"} {
				delete(result, key)
			}
			reply(w, 200, result)
			return true
		case "events":
			s.eventList(w, r, app)
			return true
		case "history":
			s.legacyHistory(w, r, app)
			return true
		}
		if app != "" {
			return false
		}
		switch endpoint {
		case "settings/site":
			v, err := site.LoadSnapshot(s.store)
			if err != nil {
				fail(w, 503, "Site settings unavailable")
				return true
			}
			revisionReply(w, v.Revision, v)
			return true
		case "settings/proxy":
			v := s.pool.Proxy().Redacted()
			revisionReply(w, v.Revision, v)
			return true
		case "settings/public-url":
			v := s.public.View(requestOrigin)
			revisionReply(w, v.Revision, v)
			return true
		}
	case http.MethodPut:
		if app != "" || (endpoint != "settings/site" && endpoint != "settings/proxy" && endpoint != "settings/public-url") {
			return false
		}
		rev, err := expectedRevision(r)
		if err != nil {
			problem(w, 400, "INVALID_REQUEST", err.Error())
			return true
		}
		s.legacyPutSettings(w, r, endpoint, rev, requestOrigin)
		return true
	}
	return false
}

func (s *Server) legacyHistory(w http.ResponseWriter, r *http.Request, app string) {
	key, window := r.URL.Query().Get("metric"), r.URL.Query().Get("range")
	validMetric := false
	if app == "" {
		_, validMetric = history.Find(key)
	} else {
		for _, definition := range history.AppDefinitions() {
			if definition.Key == key {
				validMetric = true
				break
			}
		}
	}
	if !validMetric || (window != "24h" && window != "7d" && window != "30d") {
		fail(w, 400, "Invalid history metric or range")
		return
	}
	var series history.Series
	var err error
	if app == "" {
		series, err = s.history.Query(key, window, time.Now().UTC())
	} else {
		entry, _ := s.registry.LookupAny(app)
		series, err = s.history.QueryFor(entry.MetricsID(), key, window, time.Now().UTC())
	}
	if err != nil {
		fail(w, 503, "Failed to read metric history")
		return
	}
	reply(w, 200, series)
}

func (s *Server) legacyPutSettings(w http.ResponseWriter, r *http.Request, endpoint string, rev int64, requestOrigin string) {
	switch endpoint {
	case "settings/site":
		var input site.Settings
		if decode(w, r, &input) != nil || input.Validate() != nil {
			fail(w, 400, "Invalid site settings")
			return
		}
		v, e := site.SaveCAS(s.store, input, rev)
		if e != nil {
			settingsError(w, e)
			return
		}
		revisionReply(w, v.Revision, v)
	case "settings/proxy":
		var input distributor.ProxyUpdate
		if decode(w, r, &input) != nil {
			fail(w, 400, "Invalid proxy settings")
			return
		}
		if e := s.pool.SetProxy(input, rev); e != nil {
			if errors.Is(e, distributor.ErrInvalidProxySettings) {
				fail(w, 400, "Invalid proxy settings")
				return
			}
			settingsError(w, e)
			return
		}
		v := s.pool.Proxy().Redacted()
		revisionReply(w, v.Revision, v)
	case "settings/public-url":
		var input map[string]json.RawMessage
		if decode(w, r, &input) != nil || len(input) != 1 || input["override_url"] == nil {
			fail(w, 400, "Expected override_url")
			return
		}
		var value *string
		if json.Unmarshal(input["override_url"], &value) != nil {
			fail(w, 400, "Invalid public URL")
			return
		}
		if value != nil {
			if clean, e := config.PublicURL(*value); e != nil || clean == "" {
				fail(w, 400, "Invalid public URL")
				return
			}
		}
		if _, e := s.public.Set(value, rev); e != nil {
			settingsError(w, e)
			return
		}
		v := s.public.View(requestOrigin)
		revisionReply(w, v.Revision, v)
	}
}
