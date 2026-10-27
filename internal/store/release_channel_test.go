package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReleaseChannelRefreshIsAtomic(t *testing.T) {
	s := openTest(t)
	app := storageOf(t, s, "example/editor")
	now := time.Now().UTC().Truncate(time.Second)
	metadata := ReleaseMetadata{AppID: app, Version: "old", Raw: []byte(`{"version":"old"}`), TrustRevision: 1, FetchedAt: now}
	resource := Resource{AppID: app, Version: "old", Key: "windows-x64-exe-user", SourceURL: "https://example.test/old.exe", SHA256: strings.Repeat("a", 64)}
	channel := Channel{AppID: app, Name: "stable", Version: "old", FetchedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := s.PutReleaseChannel(metadata, []Resource{resource}, channel); err != nil {
		t.Fatal(err)
	}
	metadata.Version = "new"
	metadata.Raw = []byte(`{"version":"new"}`)
	resource.Version = "new"
	resource.SourceURL = "https://example.test/new.exe"
	channel.Version = "new"
	if _, err := s.db.Exec(`CREATE TRIGGER reject_channel BEFORE INSERT ON channels BEGIN SELECT RAISE(FAIL,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.PutReleaseChannel(metadata, []Resource{resource}, channel); err == nil {
		t.Fatal("channel write failure ignored")
	}
	current, err := s.Channel(app, "stable")
	if err != nil || current.Version != "old" {
		t.Fatal("old valid channel lost", current, err)
	}
	if _, err = s.Release(app, "new"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("partial release committed", err)
	}
	if _, err = s.Resource(app, "new", resource.Key); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("partial resource committed", err)
	}
	versions, err := s.VersionsFor(app)
	if err != nil || len(versions) != 1 {
		t.Fatal("partial history committed", versions, err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER reject_channel`); err != nil {
		t.Fatal(err)
	}
	if err = s.PutReleaseChannel(metadata, []Resource{resource}, channel); err != nil {
		t.Fatal(err)
	}
	current, err = s.Channel(app, "stable")
	if err != nil || current.Version != "new" {
		t.Fatal(current, err)
	}
	resource.SHA256 = strings.Repeat("b", 64)
	if err = s.PutReleaseChannel(metadata, []Resource{resource}, channel); !errors.Is(err, ErrImmutableRelease) {
		t.Fatal("changed binding accepted", err)
	}
	actual, err := s.Resource(app, "new", resource.Key)
	if err != nil || actual.SHA256 != strings.Repeat("a", 64) {
		t.Fatal(actual, err)
	}
	resource.SHA256 = strings.Repeat("a", 64)
	metadata.Raw = []byte(`{"version":"new","arch":"changed"}`)
	if err = s.PutReleaseChannel(metadata, []Resource{resource}, channel); !errors.Is(err, ErrImmutableRelease) {
		t.Fatal("normalized metadata changed without changing bindings", err)
	}
	saved, err := s.Release(app, "new")
	if err != nil || string(saved.Raw) != `{"version":"new"}` {
		t.Fatal("immutable snapshot changed", string(saved.Raw), err)
	}
	channel.Version = "unrelated"
	if err = s.PutReleaseChannel(metadata, []Resource{resource}, channel); err == nil {
		t.Fatal("mixed channel snapshot accepted")
	}
}

func TestRefreshStatusRetainsSuccessAndIsSourceScoped(t *testing.T) {
	s := openTest(t)
	app := storageOf(t, s, "example/editor")
	if err := s.RecordReleaseRefresh(app, "stable", true); err != nil {
		t.Fatal(err)
	}
	first, err := s.ReleaseRefresh(app, "stable")
	if err != nil || first.Success == nil || first.Failed {
		t.Fatal(first, err)
	}
	if err = s.RecordReleaseRefresh(app, "stable", false); err != nil {
		t.Fatal(err)
	}
	next, err := s.ReleaseRefresh(app, "stable")
	if err != nil || !next.Failed || next.Success == nil || !next.Success.Equal(*first.Success) {
		t.Fatal(next, err)
	}
	if _, err = s.ReleaseRefresh(app, "preview"); !errors.Is(err, ErrNotFound) {
		t.Fatal("status crossed channels", err)
	}
}
