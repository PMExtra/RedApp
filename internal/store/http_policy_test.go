package store

import (
	"errors"
	"testing"

	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/pathmatch"
)

func TestHTTPPolicyApplicationCASAndAtomicPersistence(t *testing.T) {
	fault := &commitFault{}
	s := openTest(t, fault.option())
	v, err := s.CreateVendor(directoryVendor("publisher"))
	if err != nil {
		t.Fatal(err)
	}
	app, err := s.CreateApplication(v.ID, directoryApplication("files"))
	if err != nil {
		t.Fatal(err)
	}
	empty, revision, err := s.ReadHTTPPolicy(app.Key)
	if err != nil || revision != app.Revision || empty.Rules == nil || empty.AutoCleanup == nil || !empty.StaleFallback || len(empty.Rules) != 0 {
		t.Fatal(empty, revision, err)
	}
	config := cachepolicy.Empty()
	config.Rules = []cachepolicy.CacheRule{{Match: pathmatch.Spec{Type: "glob", Pattern: "/releases/"}, TTLSeconds: 0}}
	config.AutoCleanup = []cachepolicy.CleanupRule{{Match: pathmatch.Spec{Type: "re2", Pattern: `/releases/.*`}, Basis: "last_access", AgeSeconds: 86400}}
	oldSource, err := s.Source(app.StorageID())
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.SaveHTTPPolicy(app.Key, app.Revision, config)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != app.Revision+1 || updated.SourceEpoch != app.SourceEpoch || updated.UID != app.UID || config.Rules[0].ID != "" {
		t.Fatal("policy save changed identity/epoch/input", updated, config)
	}
	got, revision, err := s.ReadHTTPPolicy(app.Key)
	if err != nil || revision != updated.Revision || len(got.Rules) != 1 || !identity.ValidUID(got.Rules[0].ID) || got.Rules[0].TTLSeconds != 0 {
		t.Fatal(got, revision, err)
	}
	if err = s.CheckSourceActive(app.StorageID(), oldSource.Fence()); !errors.Is(err, ErrSourceInactive) {
		t.Fatal("policy edit failed to fence old work", err)
	}
	if _, err = s.SaveHTTPPolicy(app.Key, app.Revision, config); !errors.Is(err, ErrConflict) {
		t.Fatal("stale policy revision accepted", err)
	}
	fault.armed.Store(true)
	changed := got
	changed.StaleFallback = false
	if _, err = s.SaveHTTPPolicy(app.Key, updated.Revision, changed); err == nil {
		t.Fatal("injected app write failure ignored")
	}
	retained, revision, err := s.ReadHTTPPolicy(app.Key)
	if err != nil || revision != updated.Revision || !retained.StaleFallback || retained.Rules[0].ID != got.Rules[0].ID {
		t.Fatal("partial policy commit", retained, revision, err)
	}
	fault.armed.Store(false)
	changes := applicationChanges(updated)
	changes.BaseURL = "https://replacement.internal/files"
	changes.Enabled = false
	updated, err = s.UpdateApplication(updated.Key, updated.Revision, changes)
	if err != nil {
		t.Fatal(err)
	}
	got, revision, err = s.ReadHTTPPolicy(updated.Key)
	if err != nil || revision != updated.Revision || got.Rules[0].ID != retained.Rules[0].ID {
		t.Fatal("source epoch split stable policy", got, revision, err)
	}
	got.StaleFallback = false
	updated, err = s.SaveHTTPPolicy(updated.Key, updated.Revision, got)
	if err != nil || updated.SourceEpoch != 2 || updated.Enabled {
		t.Fatal("disabled policy write failed or changed source", updated, err)
	}
	got, _, err = s.ReadHTTPPolicy(updated.Key)
	if err != nil || got.StaleFallback || got.Rules[0].ID != retained.Rules[0].ID {
		t.Fatal("explicit false or stable rule ID lost", got, err)
	}
	if err = s.DeleteApplication(updated.Key, updated.Revision); err != nil {
		t.Fatal(err)
	}
	got, revision, err = s.ReadHTTPPolicy(updated.Key)
	if err != nil || revision != updated.Revision+1 || got.StaleFallback {
		t.Fatal("tombstoned policy inspection failed", got, revision, err)
	}
	if _, err = s.SaveHTTPPolicy(updated.Key, revision, config); !errors.Is(err, ErrDirectoryDeleted) {
		t.Fatal("deleted policy changed", err)
	}
}

func TestHTTPPolicyRejectsUnsupportedProvidersAndInvalidRules(t *testing.T) {
	s := openTest(t)
	v, err := s.CreateVendor(directoryVendor("publisher"))
	if err != nil {
		t.Fatal(err)
	}
	input := directoryApplication("codex")
	input.Provider = "codex"
	app, err := s.CreateApplication(v.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.ReadHTTPPolicy(app.Key); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatal(err)
	}
	if _, err = s.SaveHTTPPolicy(app.Key, app.Revision, cachepolicy.Empty()); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatal(err)
	}
	general, err := s.CreateApplication(v.ID, directoryApplication("files"))
	if err != nil {
		t.Fatal(err)
	}
	invalid := cachepolicy.Config{Rules: []cachepolicy.CacheRule{{Match: pathmatch.Spec{Type: "glob", Pattern: "/["}}}}
	if _, err = s.SaveHTTPPolicy(general.Key, general.Revision, invalid); !errors.Is(err, cachepolicy.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	got, revision, err := s.ReadHTTPPolicy(general.Key)
	if err != nil || revision != general.Revision || len(got.Rules) != 0 {
		t.Fatal("invalid save left state", got, revision, err)
	}
}
