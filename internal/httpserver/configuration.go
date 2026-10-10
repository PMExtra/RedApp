package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
)

type preparedDirectory struct {
	server    *Server
	registry  *application.PreparedRegistry
	downloads *download.UpstreamPublication
	proxy     *distributor.ProxyPublication
}

func (p *preparedDirectory) Abort() {
	if p.downloads != nil {
		p.downloads.Abort()
	}
	if p.proxy != nil {
		p.proxy.Abort()
	}
}
func (p *preparedDirectory) Publish() {
	publish := func() {
		if p.proxy != nil {
			p.proxy.Publish()
		}
		p.server.Registry.Publish(p.registry)
	}
	if p.downloads != nil {
		p.downloads.PublishWith(publish)
	} else {
		publish()
	}
}

// ConfigurePublication installs the same prepare/CAS/publish coordinator for API
// and direct Store writers. Client construction performs local validation only.
func (s *Server) ConfigurePublication() {
	s.configurationOnce.Do(func() {
		if s.DB == nil || s.Registry == nil || s.Pool == nil {
			return
		}
		s.DB.SetDistributionValidation(builtin.ValidateDescriptors)
		s.DB.SetConfigurationPrepare(func(candidate store.DirectorySnapshot) (store.ConfigurationPublication, error) {
			proxy, err := s.Pool.PrepareConfiguration(candidate)
			if err != nil {
				return nil, err
			}
			prepared := false
			defer func() {
				if !prepared {
					proxy.Abort()
				}
			}()
			entries, err := builtin.EntriesFromConfiguration(candidate, s.Pool)
			if err != nil {
				return nil, err
			}
			registry, err := application.PrepareRegistry(entries)
			if err != nil {
				return nil, err
			}
			clients := map[string]*distributor.Client{}
			for _, source := range candidate.Sources {
				client, err := builtin.NewScopedSourceClient(source.Provider, source.BaseURL, candidate.ProviderDefaults[source.Provider], source.AppUID, candidate.ProxyScopes[source.AppUID].VendorUID, s.Pool)
				if err != nil {
					return nil, err
				}
				clients[source.StorageID()] = client
			}
			result := &preparedDirectory{server: s, registry: registry, proxy: proxy}
			if s.Downloads != nil {
				result.downloads, err = s.Downloads.PrepareUpstreams(clients)
				if err != nil {
					return nil, err
				}
			}
			if s.testConfigurationPrepare != nil {
				if err = s.testConfigurationPrepare(candidate); err != nil {
					prepared = true
					result.Abort()
					return nil, err
				}
			}
			prepared = true
			return result, nil
		})
	})
}

func (s *Server) configurationAPI(w http.ResponseWriter, r *http.Request, kind, key string) {
	if !queryAllowed(r) {
		fail(w, 400, "Unexpected query")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPatch {
		fail(w, 405, "Method not allowed")
		return
	}
	var value store.Configuration
	var err error
	if r.Method == http.MethodPatch {
		var patch store.ConfigurationPatch
		if decodeLimit(w, r, &patch, 256<<10) != nil {
			fail(w, 400, "Invalid configuration patch")
			return
		}
		if r.Header.Get("If-Match") != "" {
			revision, e := expectedRevision(r)
			if e != nil || revision != patch.Revision {
				fail(w, 400, "If-Match and body revision must match")
				return
			}
		}
		s.directoryMu.Lock()
		defer s.directoryMu.Unlock()
		for path, raw := range patch.Set {
			if path == "icon" || strings.HasPrefix(path, "localized_icons.") {
				var icon string
				if json.Unmarshal(raw, &icon) != nil || s.validateDirectoryIcon(icon) != nil {
					fail(w, 400, "Invalid stored icon")
					return
				}
			}
		}
		if kind == "Vendor" {
			value, err = s.DB.PatchVendorConfiguration(key, patch)
		} else {
			value, err = s.DB.PatchApplicationConfiguration(key, patch)
		}
	} else {
		if kind == "Vendor" {
			value, err = s.DB.VendorConfiguration(key)
		} else {
			value, err = s.DB.ApplicationConfiguration(key)
		}
	}
	if err != nil {
		directoryError(w, err)
		return
	}
	revisionReply(w, value.Revision, redactConfiguration(value))
}

// UnmarshalJSON retains which leaves the legacy request explicitly supplied.
// DisallowUnknownFields is preserved even though this type has a custom decoder.
func (in *directoryInput) UnmarshalJSON(raw []byte) error {
	type plain directoryInput
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode((*plain)(in)); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &in.explicit); err != nil {
		return err
	}
	for _, value := range in.explicit {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("null directory fields are unsupported")
		}
	}
	return nil
}
func (in directoryInput) configurationFields(kind string) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	allowed := map[string]bool{"name": true, "description": true, "icon": true, "categories": kind == "App", "tags": kind == "App", "base_url": kind == "App", "base_urls": kind == "App", "source_strategy": kind == "App", "cache_ttl_seconds": kind == "App", "localized_icons": kind == "Vendor"}
	for key, raw := range in.explicit {
		if key == "revision" || key == "enabled" {
			continue
		}
		if !allowed[key] {
			return nil, fmt.Errorf("%w: field %s cannot be edited", store.ErrInvalidDirectory, key)
		}
		if key == "name" || key == "description" || key == "localized_icons" {
			var langs map[string]json.RawMessage
			if json.Unmarshal(raw, &langs) != nil {
				return nil, store.ErrInvalidDirectory
			}
			for lang, value := range langs {
				if lang != "en" && lang != "zh-CN" {
					return nil, store.ErrInvalidDirectory
				}
				out[key+"."+lang] = value
			}
		} else {
			out[key] = raw
		}
	}
	return out, nil
}

func encodeJSON(value any) json.RawMessage { raw, _ := json.Marshal(value); return raw }
