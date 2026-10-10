package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PMExtra/RedApp/internal/store"
)

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
			value, err = s.store.PatchVendorConfiguration(key, patch)
		} else {
			value, err = s.store.PatchApplicationConfiguration(key, patch)
		}
	} else {
		if kind == "Vendor" {
			value, err = s.store.VendorConfiguration(key)
		} else {
			value, err = s.store.ApplicationConfiguration(key)
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
