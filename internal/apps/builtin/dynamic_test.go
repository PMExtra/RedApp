package builtin

import (
	"testing"

	"github.com/PMExtra/RedApp/installers"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/store"
)

func TestDynamicProviderInstancesAndDirectoryLifecycle(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	seedVendors, seedApps, err := Seeds()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SeedDirectory(seedVendors, seedApps); err != nil {
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
		{ID: "files", Provider: application.GeneralHTTP, BaseURL: "http://intranet.example/files", CacheTTLSeconds: 0},
	} {
		input.Name = store.LocalizedText{En: input.ID, ZhCN: input.ID}
		input.Enabled = true
		input, err = NormalizeApplication(input)
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
		registry, err := NewDynamic(vendors, apps, pool)
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
	if _, ok := registry.Lookup(first.Descriptor.ID); ok || len(registry.Entries()) != 2 {
		t.Fatal("disabled vendor applications remained public")
	}
	if entry, ok := registry.LookupAny(first.Descriptor.ID); !ok || entry.VendorRevision != 2 {
		t.Fatal("disabled application or vendor admission revision disappeared")
	}
	// A deleted seed stays deleted after another seeding attempt.
	seed, err := db.Application("openai/codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteApplication(seed.Key, seed.Revision); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedDirectory(seedVendors, seedApps); err != nil {
		t.Fatal(err)
	}
	registry = load()
	if _, ok := registry.Lookup(seed.Key); ok {
		t.Fatal("one-time seed resurrected a deleted app")
	}
	if _, ok := registry.LookupAny(seed.Key); !ok {
		t.Fatal("deleted source disappeared from management/recovery")
	}
	if empty, err := NewDynamic(nil, nil, pool); err != nil || len(empty.Entries()) != 0 {
		t.Fatal("empty runtime directory is invalid", err)
	}
}

func TestRetiredSourceClientsDoNotDependOnPublicRegistry(t *testing.T) {
	pool := distributor.NewPool()
	for _, input := range [][2]string{{application.Codex, "http://intranet.example:8081/codex"}, {application.ClaudeCode, "https://intranet.example/claude"}, {application.GeneralHTTP, "http://intranet.example/files"}} {
		client, err := NewSourceClient(input[0], input[1], pool)
		if err != nil || client.Base.String() != input[1] {
			t.Fatal("independent historical source binding unavailable", err)
		}
	}
	if _, err := NewSourceClient("unknown", "https://example/files", pool); err == nil {
		t.Fatal("unregistered provider constructed")
	}
	if _, err := NewSourceClient(application.GeneralHTTP, "https://example/files", nil); err == nil {
		t.Fatal("dynamic source accepted independent/nil transport")
	}
}
