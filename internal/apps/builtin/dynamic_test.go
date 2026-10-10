package builtin

import (
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/installers"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/presets"
)

func TestDynamicProviderInstancesAndDirectoryLifecycle(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	vendor, err := db.CreateVendor(store.VendorInput{ID: "enterprise", Name: store.LocalizedText{En: "Enterprise", ZhCN: "企业"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var created []store.Application
	for _, input := range []store.ApplicationInput{
		{ID: "codex-one", Provider: application.Codex, BaseURL: "http://10.0.0.7:8081/mirror", CacheTTLSeconds: 60},
		{ID: "codex-two", Provider: application.Codex, BaseURL: "https://mirror.example/codex", CacheTTLSeconds: 60},
		{ID: "claude", Provider: application.ClaudeCode, CacheTTLSeconds: 60},
		{ID: "files", Provider: application.HttpCache, BaseURL: "http://intranet.example/files", CacheTTLSeconds: 0},
	} {
		input.Name = store.LocalizedText{En: input.ID, ZhCN: input.ID}
		input.Enabled = true
		input, err = normalizeApplication(input)
		if err != nil {
			t.Fatal(err)
		}
		app, err := db.CreateApplication(vendor.ID, input)
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, app)
	}
	pool := distributor.NewPool()
	load := func() *application.Registry {
		t.Helper()
		vendors, err := db.Vendors(true)
		if err != nil {
			t.Fatal(err)
		}
		apps, err := db.Applications(true)
		if err != nil {
			t.Fatal(err)
		}
		registry, err := newDynamic(vendors, apps, pool)
		if err != nil {
			t.Fatal(err)
		}
		return registry
	}
	registry := load()
	first, ok := registry.Lookup("enterprise/codex-one")
	if !ok || first.TemplateID != "openai/codex" || first.Descriptor.ID != "enterprise/codex-one" || first.Upstream.Base.String() != created[0].BaseURL || first.VendorName["zh-CN"] != "企业" {
		t.Fatal("dynamic Codex template/configuration binding failed")
	}
	second, _ := registry.Lookup("enterprise/codex-two")
	if first.Protocol == second.Protocol || first.Upstream == second.Upstream || first.StorageID() == second.StorageID() || first.MetricsID() == second.MetricsID() {
		t.Fatal("same-provider instances shared origin/protocol/cache/metrics state")
	}
	if _, err := installers.Render(first.TemplateID, first.Descriptor.ID, "install.sh", "https://download.example/enterprise/codex-one"); err != nil {
		t.Fatal("dynamic instance could not render reviewed installer", err)
	}
	general, _ := registry.Lookup("enterprise/files")
	if general.Protocol != nil || general.TemplateID != "" || len(general.Descriptor.Installers) != 0 || len(general.Descriptor.Channels) != 0 || general.Descriptor.DefaultChannelTTLSeconds != 0 {
		t.Fatal("GeneralHttp received synthetic immutable-release capabilities")
	}
	// Vendor disable hides its apps without mutating their own enable flags.
	if _, err := db.UpdateVendor(vendor.ID, vendor.Revision, store.VendorChanges{Name: vendor.Name, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	registry = load()
	if _, ok := registry.Lookup(first.Descriptor.ID); ok {
		t.Fatal("disabled vendor applications remained public")
	}
	for _, entry := range registry.Entries() {
		if entry.VendorID == vendor.ID {
			t.Fatalf("application %s of a disabled vendor remained public", entry.Descriptor.ID)
		}
	}
	if entry, ok := registry.LookupAny(first.Descriptor.ID); !ok || entry.VendorRevision != 2 {
		t.Fatal("disabled application or vendor admission revision disappeared")
	}
	// Template-key protection is independent of the provider implementation.
	seed, err := db.Application("openai/codex")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.DeleteApplication(seed.Key, seed.Revision); err == nil {
		t.Fatal("built-in template was deleted")
	}
	if _, err = db.UpdateApplication(seed.Key, seed.Revision, store.ApplicationChanges{Name: seed.Name, Description: seed.Description, Icon: seed.Icon, BaseURL: seed.BaseURL, CacheTTLSeconds: seed.CacheTTLSeconds, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err = db.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	registry = load()
	if _, ok := registry.Lookup(seed.Key); ok {
		t.Fatal("template insertion re-enabled an existing application")
	}
	if _, ok := registry.LookupAny(seed.Key); !ok {
		t.Fatal("disabled template disappeared")
	}
	if empty, err := newDynamic(nil, nil, pool); err != nil || len(empty.Entries()) != 0 {
		t.Fatal("empty runtime directory is invalid", err)
	}
}

func TestRetiredSourceClientsDoNotDependOnPublicRegistry(t *testing.T) {
	pool := distributor.NewPool()
	appUID, vendorUID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	for _, input := range [][2]string{{application.Codex, "http://intranet.example:8081/codex"}, {application.ClaudeCode, "https://intranet.example/claude"}, {application.HttpCache, "http://intranet.example/files"}} {
		client, err := NewScopedSourceClient(input[0], input[1], "", appUID, vendorUID, pool)
		if err != nil || client.Base.String() != input[1] {
			t.Fatal("independent historical source binding unavailable", err)
		}
	}
	if _, err := NewScopedSourceClient("unknown", "https://example/files", "", appUID, vendorUID, pool); err == nil {
		t.Fatal("unregistered provider constructed")
	}
	if _, err := NewScopedSourceClient(application.HttpCache, "https://example/files", "", appUID, vendorUID, nil); err == nil {
		t.Fatal("dynamic source accepted a nil transport pool")
	}
	if _, err := NewScopedSourceClient(application.HttpCache, "https://example/files", "", "", vendorUID, pool); err == nil {
		t.Fatal("source client built without an owner scope")
	}
}

func TestIndependentReleaseInstancesUseTheProviderTemplate(t *testing.T) {
	descriptors, err := reviewedDescriptors()
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalTemplates(descriptors)
	if err != nil {
		t.Fatal(err)
	}
	for provider := range releaseProtocols {
		id := canonical[provider]
		if id == "" {
			t.Fatalf("%s has no reviewed template", provider)
		}
		for _, d := range descriptors {
			if d.ID == id && d.Protocol != releaseProtocols[provider] {
				t.Fatalf("%s maps to %s with protocol %s", provider, id, d.Protocol)
			}
		}
	}
	if _, err = canonicalTemplates(append(descriptors, descriptors[0])); err == nil {
		t.Fatal("two templates for one provider accepted")
	}
}

// normalizeApplication applies provider BaseURL defaults. The API supplies the
// default TTL only when its input field was omitted, so an explicit GeneralHttp
// TTL of zero remains meaningful (always revalidate).
func normalizeApplication(input store.ApplicationInput) (store.ApplicationInput, error) {
	config, err := application.NormalizeConfig(input.Provider, application.ProviderConfig{BaseURL: input.BaseURL, BaseURLs: input.BaseURLs, SourceStrategy: input.SourceStrategy, CacheTTLSeconds: input.CacheTTLSeconds})
	if err != nil {
		return store.ApplicationInput{}, err
	}
	input.BaseURL, input.CacheTTLSeconds = config.BaseURL, config.CacheTTLSeconds
	input.BaseURLs, input.SourceStrategy = config.BaseURLs, config.SourceStrategy
	return input, nil
}

func newDynamic(vendors []store.Vendor, apps []store.Application, pool *distributor.Pool) (*application.Registry, error) {
	entries, err := entriesFromRecords(vendors, apps, pool)
	if err != nil {
		return nil, err
	}
	return application.NewRegistry(entries)
}

// entriesFromRecords builds the entries of persisted records with the reviewed descriptors.
func entriesFromRecords(vendors []store.Vendor, apps []store.Application, pool *distributor.Pool) ([]application.Entry, error) {
	descriptors, err := reviewedDescriptors()
	if err != nil {
		return nil, err
	}
	return entriesFromConfiguration(store.DirectorySnapshot{Vendors: vendors, Applications: apps, ReviewedDescriptors: descriptors}, pool)
}

func reviewedDescriptors() ([]application.Descriptor, error) {
	input := presets.Embedded().Descriptors()
	if err := ValidateDescriptors(input); err != nil {
		return nil, err
	}
	return input, nil
}
