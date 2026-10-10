package httpserver

import (
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
		p.server.registry.Publish(p.registry)
	}
	if p.downloads != nil {
		p.downloads.PublishWith(publish)
	} else {
		publish()
	}
}

// configurePublication installs the prepare/CAS/publish coordinator used by
// every configuration writer (API handlers and direct Store writers). Client
// construction performs local validation only.
func (s *Server) configurePublication() {
	s.store.SetDistributionValidation(builtin.ValidateDescriptors)
	s.store.SetConfigurationPrepare(func(candidate store.DirectorySnapshot) (store.ConfigurationPublication, error) {
		proxy, err := s.pool.PrepareConfiguration(candidate)
		if err != nil {
			return nil, err
		}
		prepared := false
		defer func() {
			if !prepared {
				proxy.Abort()
			}
		}()
		entries, err := builtin.EntriesFromConfiguration(candidate, s.pool)
		if err != nil {
			return nil, err
		}
		registry, err := application.PrepareRegistry(entries)
		if err != nil {
			return nil, err
		}
		clients := map[string]*distributor.Client{}
		for _, source := range candidate.Sources {
			client, err := builtin.NewScopedSourceClient(source.Provider, source.BaseURL, candidate.ProviderDefaults[source.Provider], source.AppUID, candidate.ProxyScopes[source.AppUID].VendorUID, s.pool)
			if err != nil {
				return nil, err
			}
			clients[source.StorageID()] = client
		}
		result := &preparedDirectory{server: s, registry: registry, proxy: proxy}
		result.downloads, err = s.downloads.PrepareUpstreams(clients)
		if err != nil {
			return nil, err
		}
		prepared = true
		if s.configurationCheck != nil {
			if err = s.configurationCheck(candidate); err != nil {
				result.Abort()
				return nil, err
			}
		}
		return result, nil
	})
}
