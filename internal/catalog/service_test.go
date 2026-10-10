package catalog_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/claude"
	"github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/catalog"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
)

func descriptor(id string, channels ...string) application.Descriptor {
	return application.Descriptor{ID: id, TrustRevision: 1, DefaultChannelTTLSeconds: 60, Channels: channels}
}
func openStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	return db
}
func registry(t *testing.T, entries ...application.Entry) *application.Registry {
	t.Helper()
	r, err := application.NewRegistry(entries)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func releaseJSON(base, digest string) []byte {
	b, _ := json.Marshal(codex.Release{Tag: "rust-v1.2.3", Assets: []codex.Asset{{Name: "asset.tgz", Digest: "sha256:" + digest, URL: base + "/releases/1.2.3/asset.tgz"}}})
	return b
}

// One shared contract covers coalescing, app-scoped channel state, immutable
// authorization and conservative cleanup; a third app reuses an existing protocol.
func TestSharedCatalogIsolationAndImmutableBinding(t *testing.T) {
	var base string
	digest := sha256.Sum256([]byte("artifact"))
	hash := hex.EncodeToString(digest[:])
	var requests atomic.Int32
	var fail, changed atomic.Bool
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/channels/latest" && r.URL.Path != "/releases/1.2.3/release.json" {
			t.Errorf("unexpected eager or untrusted request %s", r.URL.Path)
		}
		if fail.Load() {
			http.Error(w, "unavailable", 503)
			return
		}
		h := hash
		if changed.Load() {
			h = strings.Repeat("a", 64)
		}
		w.Write(releaseJSON(base, h))
	}))
	base = client.Base.String()
	ids := []string{"openai/codex", "example/third"}
	entries := []application.Entry{}
	for _, id := range ids {
		entries = append(entries, application.Entry{Descriptor: descriptor(id, "latest"), Protocol: codex.NewProtocol(client), Upstream: client})
	}
	reg := registry(t, entries...)
	db := openStore(t)
	service := catalog.New(db, reg)
	var wg sync.WaitGroup
	for range 30 {
		for _, id := range ids {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				if _, err := service.Release(context.Background(), id, "latest"); err != nil {
					t.Error(err)
				}
			}(id)
		}
	}
	wg.Wait()
	if requests.Load() != 2 {
		t.Fatalf("app-scoped coalescing/cache requests=%d", requests.Load())
	}
	resources := []download.Resource{}
	for _, id := range ids {
		r, err := service.Authorize(context.Background(), id, "1.2.3", "asset.tgz")
		if err != nil {
			t.Fatal(err)
		}
		resources = append(resources, r)
		versions, err := db.VersionsFor(id)
		if err != nil || len(versions) != 1 {
			t.Fatalf("discovery scope: %v %v", versions, err)
		}
	}
	if resources[0].ID == resources[1].ID || resources[0].Source != resources[1].Source {
		t.Fatal("logical ownership merged across identical upstream blobs")
	}
	if _, err := service.Authorize(context.Background(), ids[0], "1.2.3", "missing.tgz"); !errors.Is(err, application.ErrNotFound) {
		t.Fatal("unknown artifact did not return not found", err)
	}
	channel, err := db.Channel(ids[0], "latest")
	if err != nil {
		t.Fatal(err)
	}
	channel.FetchedAt = time.Now().Add(-time.Hour)
	channel.ExpiresAt = time.Now().Add(-time.Minute)
	if err = db.PutChannel(channel); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	if _, err = service.Release(context.Background(), ids[0], "latest"); !errors.Is(err, application.ErrUpstream) || errors.Is(err, application.ErrUntrusted) {
		t.Fatal("stale channel served after upstream failure", err)
	}
	after, _ := db.Channel(ids[0], "latest")
	if !after.FetchedAt.Equal(channel.FetchedAt.Truncate(time.Second)) && !after.FetchedAt.Equal(channel.FetchedAt) {
		t.Fatal("failed fetch refreshed expiry")
	}
	if _, err = service.Release(context.Background(), ids[1], "latest"); err != nil {
		t.Fatal("channel failure crossed apps", err)
	}
	fail.Store(false)
	changed.Store(true)
	if _, err = service.Release(context.Background(), ids[0], "latest"); !errors.Is(err, application.ErrUntrusted) {
		t.Fatal("changed immutable release accepted", err)
	}
	r, err := service.Authorize(context.Background(), ids[0], "1.2.3", "asset.tgz")
	if err != nil || r.Hash != hash || r.ID != resources[0].ID {
		t.Fatal("conflict destroyed old authorized binding", err)
	}
	views := []download.View{}
	for _, r := range resources {
		views = append(views, download.View{Generation: download.Generation{Resource: r}})
	}
	spoof := resources[1]
	spoof.Labels = map[string]string{"app": ids[0], "version": "1.0.0"}
	views = append(views, download.View{Generation: download.Generation{Resource: spoof}})
	selected, unknown, err := service.Candidates(ids[0], "2.0.0", views)
	if err != nil || len(selected) != 1 || !selected[resources[0].ID] || len(unknown) != 0 {
		t.Fatalf("cleanup used labels or crossed scope: %v %v %v", selected, unknown, err)
	}
}

func TestSignedEnvelopePersistenceAndFailureBoundary(t *testing.T) {
	raw, err := os.ReadFile("../apps/claude/testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile("../apps/claude/testdata/manifest.json.sig")
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	var tamper, missingSignature atomic.Bool
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/latest", "/stable":
			w.Write([]byte("2.1.285\n"))
		case "/2.1.285/manifest.json":
			b := raw
			if tamper.Load() {
				b = append(append([]byte(nil), raw...), '\n')
			}
			w.Write(b)
		case "/2.1.285/manifest.json.sig":
			if missingSignature.Load() {
				http.NotFound(w, r)
				return
			}
			w.Write(sig)
		default:
			http.NotFound(w, r)
		}
	}))
	reg := registry(t, application.Entry{Descriptor: descriptor("anthropic/claude-code", "latest", "stable"), Protocol: claude.NewProtocol(client), Upstream: client})
	db := openStore(t)
	service := catalog.New(db, reg)
	release, err := service.Release(context.Background(), "anthropic/claude-code", "latest")
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 {
		t.Fatal("manifest eagerly fetched an artifact", requests.Load())
	}
	service = catalog.New(db, reg)
	for _, name := range []string{"manifest.json", "manifest.json.sig"} {
		out, err := service.Represent(context.Background(), "anthropic/claude-code", application.Operation{Kind: application.MetadataOperation, Target: release.Version, Name: name}, "https://mirror.invalid/anthropic/claude-code")
		expected := raw
		if name == "manifest.json.sig" {
			expected = sig
		}
		if err != nil || !bytes.Equal(out.Body, expected) {
			t.Fatal("persisted signed bytes changed", err)
		}
	}
	if requests.Load() != 3 {
		t.Fatal("immutable metadata cache was not persistent")
	}
	if _, err = service.Authorize(context.Background(), "anthropic/claude-code", release.Version, "linux-x64/claude"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Release(context.Background(), "anthropic/claude-code", "2.1.286"); !errors.Is(err, application.ErrNotFound) {
		t.Fatal("missing release mapped to upstream failure", err)
	}
	// Corrupt signed bytes on disk: cached parsing cannot bypass signature checks.
	if _, err = db.DB.Exec("UPDATE release_metadata SET raw=? WHERE app_id=?", append(append([]byte(nil), raw...), '\n'), "anthropic/claude-code"); err != nil {
		t.Fatal(err)
	}
	tamper.Store(true)
	if _, err = service.Authorize(context.Background(), "anthropic/claude-code", release.Version, "linux-x64/claude"); !errors.Is(err, application.ErrUntrusted) {
		t.Fatal("untrusted metadata authorized an artifact", err)
	}
	tamper.Store(false)
	missingSignature.Store(true)
	if _, err = service.Release(context.Background(), "anthropic/claude-code", release.Version); !errors.Is(err, application.ErrUpstream) {
		t.Fatal("missing signature disguised as missing artifact", err)
	}
}

func TestCanceledWaiterDoesNotCancelSharedFetch(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	var once sync.Once
	var base string
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		<-finish
		w.Write(releaseJSON(base, strings.Repeat("a", 64)))
	}))
	base = client.Base.String()
	reg := registry(t, application.Entry{Descriptor: descriptor("example/app", "latest"), Protocol: codex.NewProtocol(client), Upstream: client})
	service := catalog.New(openStore(t), reg)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := service.Release(ctx, "example/app", "latest"); result <- err }()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(finish)
	if _, err := service.Release(context.Background(), "example/app", "latest"); err != nil {
		t.Fatal("one canceled request poisoned shared fetch", err)
	}
}
