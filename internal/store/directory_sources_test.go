package store

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
)

func TestDirectoryMultiSourceSnapshotsAndLegacyEdits(t *testing.T) {
	s := openTest(t)
	v, err := s.CreateVendor(directoryVendor("vendor"))
	if err != nil {
		t.Fatal(err)
	}
	in := directoryApplication("app")
	in.BaseURL = "https://unused.example.test"
	in.BaseURLs = []string{"https://PRIMARY.example.test/%70ackages/", "http://mirror.internal:8080/packages/"}
	originalInput := slices.Clone(in.BaseURLs)
	a, err := s.CreateApplication(v.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"https://primary.example.test/packages", "http://mirror.internal:8080/packages"}
	if a.BaseURL != expected[0] || !slices.Equal(a.BaseURLs, expected) || a.SourceStrategy != "ordered" || !slices.Equal(in.BaseURLs, originalInput) {
		t.Fatal("initial normalization or caller mutation", a, in)
	}
	first, err := s.Source(a.StorageID())
	if err != nil || !slices.Equal(first.BaseURLs, expected) || first.SourceStrategy != "ordered" {
		t.Fatal(first, err)
	}
	releaseFixture(t, s, a.StorageID(), "1.0.0")

	// The legacy update shape must not silently discard secondary sources. Even
	// equivalent URL spelling preserves the source snapshot and selection policy.
	change := applicationChanges(a)
	change.BaseURL += "/"
	change.Name.En = "Updated label"
	change.CacheTTLSeconds = 0
	a, err = s.UpdateApplication(a.Key, a.Revision, change)
	if err != nil || a.SourceEpoch != 1 || !slices.Equal(a.BaseURLs, expected) || a.SourceStrategy != "ordered" {
		t.Fatal(a, err)
	}
	change = applicationChanges(a)
	change.SourceStrategy = "round_robin"
	a, err = s.UpdateApplication(a.Key, a.Revision, change)
	if err != nil || a.SourceEpoch != 2 || !slices.Equal(a.BaseURLs, expected) || a.SourceStrategy != "round_robin" {
		t.Fatal(a, err)
	}
	second, err := s.Source(a.StorageID())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CheckSourceActive(first.StorageID(), first.Fence()); !errors.Is(err, ErrSourceInactive) {
		t.Fatal("old strategy flight remained publishable", err)
	}

	// Order alone changes cache identity. BaseURL is always the canonical first
	// element even if a direct store caller supplies an inconsistent legacy field.
	change = applicationChanges(a)
	change.BaseURLs = []string{expected[1], expected[0]}
	a, err = s.UpdateApplication(a.Key, a.Revision, change)
	if err != nil || a.SourceEpoch != 3 || a.BaseURL != expected[1] || a.SourceStrategy != "round_robin" {
		t.Fatal(a, err)
	}
	third, err := s.Source(a.StorageID())
	if err != nil {
		t.Fatal(err)
	}
	change = applicationChanges(a)
	change.BaseURLs = []string{expected[1], "https://another.example.test/packages"}
	a, err = s.UpdateApplication(a.Key, a.Revision, change)
	if err != nil || a.SourceEpoch != 4 {
		t.Fatal(a, err)
	}
	change = applicationChanges(a)
	change.Enabled = false
	a, err = s.UpdateApplication(a.Key, a.Revision, change)
	if err != nil || a.SourceEpoch != 4 || len(a.BaseURLs) != 2 || a.SourceStrategy != "round_robin" {
		t.Fatal("disable or legacy metadata update lost sources", a, err)
	}
	change = applicationChanges(a)
	change.BaseURL = "https://replacement.example.test/files/"
	a, err = s.UpdateApplication(a.Key, a.Revision, change)
	if err != nil || a.SourceEpoch != 5 || !slices.Equal(a.BaseURLs, []string{"https://replacement.example.test/files"}) || a.SourceStrategy != "round_robin" {
		t.Fatal("explicit legacy base replacement", a, err)
	}
	for _, snapshot := range []SourceRecord{first, second, third} {
		loaded, err := s.Source(snapshot.StorageID())
		if err != nil || loaded.Active || loaded.BaseURL != snapshot.BaseURL || !slices.Equal(loaded.BaseURLs, snapshot.BaseURLs) || loaded.SourceStrategy != snapshot.SourceStrategy || !loaded.CreatedAt.Equal(snapshot.CreatedAt) {
			t.Fatal("historical source changed", snapshot, loaded, err)
		}
		loaded.BaseURLs[0] = "https://mutation.example.test"
		reloaded, err := s.Source(snapshot.StorageID())
		if err != nil || !slices.Equal(reloaded.BaseURLs, snapshot.BaseURLs) {
			t.Fatal("source read leaked mutable storage", reloaded, err)
		}
	}
	if _, err = s.Release(first.StorageID(), "1.0.0"); err != nil {
		t.Fatal("changing source selection removed old data", err)
	}
	if err = s.DeleteApplication(a.Key, a.Revision); err != nil {
		t.Fatal(err)
	}
	sources, err := s.Sources()
	if err != nil || len(sources) != 5 {
		t.Fatal("tombstone lost source snapshots", sources, err)
	}
}

func TestDirectoryMultiSourceCASAndSnapshotRollback(t *testing.T) {
	fault := &commitFault{}
	s := openTest(t, fault.option())
	v, err := s.CreateVendor(directoryVendor("vendor"))
	if err != nil {
		t.Fatal(err)
	}
	in := directoryApplication("app")
	in.BaseURLs = []string{"https://one.example.test", "https://two.example.test"}
	a, err := s.CreateApplication(v.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	start, results := make(chan struct{}), make(chan error, 2)
	var wg sync.WaitGroup
	for _, strategy := range []string{"random", "round_robin"} {
		change := applicationChanges(a)
		change.SourceStrategy = strategy
		wg.Go(func() {
			<-start
			_, err := s.UpdateApplication(a.Key, a.Revision, change)
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("source CAS winners", success, conflict)
	}
	a, err = s.Application(a.Key)
	if err != nil || a.SourceEpoch != 2 || a.Revision != 2 {
		t.Fatal(a, err)
	}
	before, err := s.Sources()
	if err != nil || len(before) != 2 {
		t.Fatal(before, err)
	}
	// A failure after the new source snapshot and the advanced application
	// pointer are written must roll back both, leaving no orphan future epoch.
	fault.armed.Store(true)
	change := applicationChanges(a)
	change.BaseURLs = []string{"https://three.example.test"}
	if _, err = s.UpdateApplication(a.Key, a.Revision, change); err == nil {
		t.Fatal("injected failure unexpectedly committed")
	}
	after, err := s.Sources()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("partial source snapshot survived failed update", after, err)
	}
	current, err := s.Application(a.Key)
	if err != nil || !reflect.DeepEqual(a, current) {
		t.Fatal("partial source configuration survived failed update", current, err)
	}
}

func TestDirectoryMultiSourceBoundaries(t *testing.T) {
	s := openTest(t)
	v, err := s.CreateVendor(directoryVendor("vendor"))
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*ApplicationInput){
		"explicit empty": func(a *ApplicationInput) { a.BaseURLs = []string{} },
		"too many": func(a *ApplicationInput) {
			for i := range 17 {
				a.BaseURLs = append(a.BaseURLs, fmt.Sprintf("https://source%d.example.test", i))
			}
		},
		"normalized duplicate": func(a *ApplicationInput) {
			a.BaseURLs = []string{"https://SOURCE.example.test/%66iles/", "https://source.example.test/files"}
		},
		"invalid secondary": func(a *ApplicationInput) {
			a.BaseURLs = []string{"https://source.example.test/files", "https://other.example.test/?key=secret"}
		},
		"invalid strategy": func(a *ApplicationInput) { a.SourceStrategy = "fallback" },
		"release list": func(a *ApplicationInput) {
			a.Provider, a.BaseURLs = "codex", []string{a.BaseURL}
		},
		"release strategy": func(a *ApplicationInput) { a.Provider, a.SourceStrategy = "claude-code", "ordered" },
	} {
		t.Run(name, func(t *testing.T) {
			in := directoryApplication("app")
			edit(&in)
			if _, err := s.CreateApplication(v.ID, in); !errors.Is(err, ErrInvalidDirectory) {
				t.Fatal(err)
			}
		})
	}
	if apps, err := s.Applications(true); err != nil || len(apps) != 0 {
		t.Fatal("invalid source list persisted", apps, err)
	}
	if sources, err := s.Sources(); err != nil || len(sources) != 0 {
		t.Fatal("invalid source snapshot persisted", sources, err)
	}
	in := directoryApplication("release")
	in.Provider = "codex"
	release, err := s.CreateApplication(v.ID, in)
	if err != nil || release.BaseURLs != nil || release.SourceStrategy != "" {
		t.Fatal("release single-source compatibility", release, err)
	}
	snapshot, err := s.Source(release.StorageID())
	if err != nil || snapshot.BaseURLs != nil || snapshot.SourceStrategy != "" || snapshot.BaseURL != release.BaseURL {
		t.Fatal(snapshot, err)
	}
}
