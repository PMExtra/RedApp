package config

import (
	"errors"
	"sync"

	"github.com/PMExtra/RedApp/internal/store"
)

type publicSetting struct {
	OverrideURL *string `json:"override_url"`
}

type PublicView struct {
	OverrideURL    *string `json:"override_url"`
	EnvironmentURL *string `json:"environment_url"`
	EffectiveURL   string  `json:"effective_url"`
	Source         string  `json:"source"`
	Revision       int64   `json:"revision"`
}

type PublicSettings struct {
	mu          sync.RWMutex
	db          *store.Store
	override    *string
	environment string
	revision    int64
}

func LoadPublicSettings(db *store.Store, environment string) (*PublicSettings, error) {
	environment, err := PublicURL(environment)
	if err != nil {
		return nil, err
	}
	p := &PublicSettings{db: db, environment: environment}
	if db == nil {
		return p, nil
	}
	var saved publicSetting
	p.revision, err = db.ReadPublicURLSetting(&saved)
	if err != nil {
		return nil, err
	}
	if saved.OverrideURL != nil {
		value, err := PublicURL(*saved.OverrideURL)
		if err != nil || value == "" {
			return nil, errors.New("stored public URL override is invalid")
		}
		p.override = &value
	}
	return p, nil
}

// View snapshots all fields under one lock. The fallback must already have
// passed the request-host and trusted-proxy checks; it is never cached globally.
func (p *PublicSettings) View(verifiedRequestOrigin string) PublicView {
	p.mu.RLock()
	defer p.mu.RUnlock()
	v := PublicView{EffectiveURL: verifiedRequestOrigin, Source: "request", Revision: p.revision}
	if p.environment != "" {
		value := p.environment
		v.EnvironmentURL, v.EffectiveURL, v.Source = &value, value, "environment"
	}
	if p.override != nil {
		value := *p.override
		v.OverrideURL, v.EffectiveURL, v.Source = &value, value, "override"
	}
	return v
}

func (p *PublicSettings) Set(override *string, expected int64) (int64, error) {
	var value *string
	if override != nil {
		clean, err := PublicURL(*override)
		if err != nil || clean == "" {
			return 0, errors.New("override_url must be a nonempty HTTP(S) origin or null")
		}
		value = &clean
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.db == nil {
		return 0, errors.New("public URL settings are unavailable")
	}
	revision, err := p.db.SavePublicURLSetting(expected, publicSetting{OverrideURL: value})
	if err != nil {
		return 0, err
	}
	p.override, p.revision = value, revision
	return revision, nil
}
