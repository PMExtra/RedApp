package httpserver

import (
	"errors"
	"net/http"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/store"
)

type homepageSettingsDTO struct {
	PinnedAppKeys []string            `json:"pinned_app_keys"`
	PinnedApps    []homepagePinnedDTO `json:"pinned_apps"`
	Revision      int64               `json:"revision"`
}

type homepagePinnedDTO struct {
	Key   string         `json:"key"`
	Name  *localizedText `json:"name"`
	Icon  *string        `json:"icon"`
	State string         `json:"state"`
}

// homepageDocument adds the display data of every pin so administrators can
// recognize and remove pins that are not shown publicly.
func (s *Server) homepageDocument(v store.HomepagePins) homepageSettingsDTO {
	out := homepageSettingsDTO{PinnedAppKeys: []string{}, PinnedApps: []homepagePinnedDTO{}, Revision: v.Revision}
	for _, key := range v.Keys {
		out.PinnedAppKeys = append(out.PinnedAppKeys, key)
		pin := homepagePinnedDTO{Key: key, State: "missing"}
		if e, ok := s.registry.LookupAny(key); ok {
			name, icon := fromLocalized(e.Descriptor.Name), publicIcon(e.Descriptor.Icon)
			pin.Name, pin.Icon = &name, &icon
			switch {
			case e.DeletedAt != nil:
				pin.State = "deleted"
			case !e.Active():
				pin.State = "disabled"
			default:
				pin.State = "published"
			}
		}
		out.PinnedApps = append(out.PinnedApps, pin)
	}
	return out
}

func (s *Server) getHomepageSettings(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.HomepagePins()
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	writeRevision(w, http.StatusOK, v.Revision, s.homepageDocument(v))
}

func (s *Server) replaceHomepageSettings(w http.ResponseWriter, r *http.Request) {
	revision, e := ifMatch(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	var input struct {
		PinnedAppKeys *[]string `json:"pinned_app_keys"`
	}
	if e = decodeJSON(r, &input); e != nil {
		s.writeError(w, r, e)
		return
	}
	if input.PinnedAppKeys == nil {
		s.fail(w, r, codeInvalidRequest, nil, "pinned_app_keys is required")
		return
	}
	keys := *input.PinnedAppKeys
	if len(keys) > 100 {
		s.fail(w, r, codeValidationFailed, nil, "pinned_app_keys has at most 100 applications")
		return
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if _, err := application.ParseKey(key); err != nil || seen[key] {
			s.fail(w, r, codeValidationFailed, nil, "pinned_app_keys must be distinct application keys (vendor/app)")
			return
		}
		seen[key] = true
	}
	s.directoryMu.Lock()
	defer s.directoryMu.Unlock()
	saved, err := s.store.SaveHomepagePins(store.HomepagePins{Keys: keys, Revision: revision})
	switch {
	case errors.Is(err, store.ErrConflict):
		s.writeError(w, r, revisionConflict(err))
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrDirectoryDeleted), errors.Is(err, store.ErrInvalidDirectory):
		s.fail(w, r, codeValidationFailed, nil, "Every pinned application must exist and not be deleted")
	case err != nil:
		s.writeError(w, r, storageError(err))
	default:
		writeRevision(w, http.StatusOK, saved.Revision, s.homepageDocument(saved))
	}
}
