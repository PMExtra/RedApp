package application

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/distributor"
)

func generalEntry(t *testing.T, id, uid string) Entry {
	t.Helper()
	client, err := distributor.NewPool().NewClient("http://intranet.example/files", distributor.GeneralHTTP)
	if err != nil {
		t.Fatal(err)
	}
	return Entry{
		Descriptor: Descriptor{ID: id, Name: Localized{"en": "Files", "zh-CN": "文件"}, DefaultChannelTTLSeconds: 0},
		UID:        uid, Provider: HttpCache, Revision: 1, VendorRevision: 1, SourceEpoch: 1, Enabled: true, Upstream: client,
	}
}

func TestRegistrySnapshotLifecycleAndIsolation(t *testing.T) {
	a := generalEntry(t, "example/first", "11111111111111111111111111111111")
	b := generalEntry(t, "example/second", "22222222222222222222222222222222")
	r, err := NewRegistry([]Entry{a, b})
	if err != nil {
		t.Fatal(err)
	}
	admitted, ok := r.Lookup(a.Descriptor.ID)
	if !ok || admitted.Protocol != nil || admitted.StorageID() == b.StorageID() || admitted.MetricsID() == b.MetricsID() {
		t.Fatal("GeneralHttp instances did not retain independent non-release identities")
	}
	if op, err := admitted.ParsePath("nested/包 file.zip"); err != nil || op.Kind != HTTPOperation || op.Resource != "nested/包 file.zip" {
		t.Fatalf("general path: %#v, %v", op, err)
	}
	for _, path := range []string{"", "../file", "folder/../file", "//host/file", "file?url=http://elsewhere", "a%2fb", "a\\b"} {
		if _, err := admitted.ParsePath(path); err == nil {
			t.Fatalf("unsafe general path accepted %q", path)
		}
	}
	// Neither the caller's input nor a returned descriptor may mutate a snapshot.
	a.Descriptor.Name["en"] = "input changed"
	admitted.Descriptor.Name["en"] = "output changed"
	got, _ := r.Lookup(a.Descriptor.ID)
	if got.Descriptor.Name["en"] != "Files" {
		t.Fatal("registry exposed mutable descriptor maps")
	}
	oldStorage := got.StorageID()
	a.SourceEpoch, a.Revision, a.Enabled = 2, 2, false
	deleted := time.Now()
	b.DeletedAt = &deleted
	if err := r.Replace([]Entry{a, b}); err != nil {
		t.Fatal(err)
	}
	if len(r.Entries()) != 0 || len(r.AllEntries()) != 2 || admitted.SourceEpoch != 1 {
		t.Fatal("disabled/deleted visibility or admitted immutable entry changed")
	}
	if _, ok := r.LookupAny(a.Descriptor.ID); !ok {
		t.Fatal("management lost disabled application")
	}
	if _, ok := r.LookupStorage(oldStorage); ok {
		t.Fatal("retired source remained an active lookup")
	}
	a.Enabled = true
	if err := r.Replace([]Entry{a}); err != nil {
		t.Fatal(err)
	}
	current, ok := r.LookupStorage(a.StorageID())
	if !ok || current.MetricsID() != admitted.MetricsID() || current.StorageID() == admitted.StorageID() {
		t.Fatal("source change did not isolate data while retaining metrics identity")
	}
	if err := r.Replace([]Entry{a, a}); err == nil {
		t.Fatal("duplicate snapshot accepted")
	}
	if len(r.Entries()) != 1 {
		t.Fatal("failed replacement damaged the active snapshot")
	}
	if err := r.Replace(nil); err != nil || len(r.AllEntries()) != 0 {
		t.Fatal("empty directory is not supported", err)
	}
}

func TestRegistryPublicationIsAtomicForReaders(t *testing.T) {
	a := generalEntry(t, "example/first", "11111111111111111111111111111111")
	b := generalEntry(t, "example/second", "22222222222222222222222222222222")
	r, err := NewRegistry([]Entry{a, b})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errors := make(chan string, 8)
	for range 4 {
		wg.Go(func() {
			for range 100 {
				entries := r.Entries()
				if len(entries) != 2 || entries[0].Revision != entries[1].Revision {
					errors <- "reader observed a partial registry publication"
					return
				}
			}
		})
	}
	for revision := int64(2); revision <= 100; revision++ {
		a.Revision, b.Revision = revision, revision
		if err := r.Replace([]Entry{a, b}); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestProviderConfigurationsHaveExplicitCapabilities(t *testing.T) {
	definitions := Definitions()
	if len(definitions) != 5 || definitions[0].Capabilities.Files || !definitions[0].Capabilities.Instructions || !definitions[1].Capabilities.HostedFiles || !definitions[2].Capabilities.TimeCleanup {
		t.Fatal("unexpected provider capability contract")
	}
	for _, provider := range []string{Codex, ClaudeCode} {
		config, err := NormalizeConfig(provider, ProviderConfig{CacheTTLSeconds: 60})
		definition, _ := ProviderDefinition(provider)
		if err != nil || config.BaseURL != definition.DefaultBaseURL {
			t.Fatal("release default URL unavailable", provider, err)
		}
	}
	for _, config := range []ProviderConfig{{BaseURL: "http://10.0.0.7:8081/releases/", CacheTTLSeconds: 0}, {BaseURL: "https://intranet.example/releases", CacheTTLSeconds: 86400}} {
		if _, err := NormalizeConfig(HttpCache, config); err != nil {
			t.Fatal("enterprise source rejected", err)
		}
	}
	for i, config := range []ProviderConfig{{}, {BaseURL: "file:///tmp/releases"}, {BaseURL: "https://user:secret@example/files"}, {BaseURL: "https://example/files?url=x"}, {BaseURL: "https://example/a/../b"}, {BaseURL: "https://example/files", CacheTTLSeconds: -1}} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if _, err := NormalizeConfig(HttpCache, config); err == nil {
				t.Fatal("invalid provider input accepted")
			}
		})
	}
}
