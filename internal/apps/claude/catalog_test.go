package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func fixture(t *testing.T) ([]byte, []byte) {
	t.Helper()
	raw, e := os.ReadFile("testdata/manifest.json")
	if e != nil {
		t.Fatal(e)
	}
	sig, e := os.ReadFile("testdata/manifest.json.sig")
	if e != nil {
		t.Fatal(e)
	}
	return raw, sig
}
func TestPinnedOfficialSignature(t *testing.T) {
	raw, sig := fixture(t)
	if e := Verify(raw, sig); e != nil {
		t.Fatal(e)
	}
	for _, r := range [][]byte{append(append([]byte{}, raw...), '\n'), bytes.Replace(raw, []byte("2.1.285"), []byte("2.1.286"), 1), nil} {
		if Verify(r, sig) == nil {
			t.Fatal("tampered manifest accepted")
		}
	}
	bad := append([]byte{}, sig...)
	bad[len(bad)/2] ^= 1
	if Verify(raw, bad) == nil || Verify(raw, nil) == nil || verifyWithKey(raw, sig, []byte("untrusted key")) == nil {
		t.Fatal("untrusted signature accepted")
	}
}
func TestCatalogSignedMetadataLazyAndPersistent(t *testing.T) {
	raw, sig := fixture(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/latest", "/stable":
			io.WriteString(w, "2.1.285\n")
		case "/2.1.285/manifest.json":
			w.Write(raw)
		case "/2.1.285/manifest.json.sig":
			w.Write(sig)
		default:
			t.Errorf("unexpected eager or unauthorized request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	client := &distributor.Client{Base: u, HTTP: server.Client()}
	db, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	c := New(db, client)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, e := c.Get(context.Background(), "stable")
			if e != nil || !bytes.Equal(m.Raw, raw) {
				t.Errorf("metadata: %v", e)
			}
		}()
	}
	wg.Wait()
	if requests.Load() != 3 {
		t.Fatalf("not singleflight/lazy: %d", requests.Load())
	}
	m, e := New(db, client).Get(context.Background(), "2.1.285")
	if e != nil || !bytes.Equal(m.Raw, raw) || Verify(m.Raw, m.Signature) != nil {
		t.Fatalf("signed bytes not preserved: %v", e)
	}
	r, e := c.Authorize(context.Background(), "2.1.285", "linux-x64", "claude")
	if e != nil || r.Hash != "33dad1ec615a2e08cc78b494f05c110e49916de2c79d78ec8799ebf46b233d29" || r.Labels["app"] != ID {
		t.Fatalf("authorization: %+v %v", r, e)
	}
	for _, p := range []string{"../linux-x64", "other", "linux-x64/../../"} {
		if _, e = c.Authorize(context.Background(), "2.1.285", p, "claude"); e == nil {
			t.Fatal("bad platform")
		}
	}
	if _, e = c.Authorize(context.Background(), "2.1.285", "linux-x64", "claude.zst"); e == nil {
		t.Fatal("unsigned compressed resource")
	}
	if _, e = c.Authorize(context.Background(), "2.1.285", "linux-x64", "../claude"); e == nil {
		t.Fatal("bad filename")
	}
	if requests.Load() != 3 {
		t.Fatal("authorization fetched binaries")
	}
	versions, _ := db.VersionsFor(ID)
	codex, _ := db.Versions()
	if len(versions) != 1 || len(codex) != 0 {
		t.Fatal("version history crossed applications")
	}
	// Corrupt persisted bytes: never reuse them as authority, refetch and verify.
	m.Raw = []byte(`{"version":"2.1.285"}`)
	db.Put("claude-metadata", "2.1.285", m)
	if _, e = c.Get(context.Background(), "2.1.285"); e != nil || requests.Load() != 5 {
		t.Fatalf("corrupt cache: %v count %d", e, requests.Load())
	}
}
func TestRejectUnsignedMetadataBeforeAuthorizing(t *testing.T) {
	raw, sig := fixture(t)
	raw = bytes.Replace(raw, []byte("33dad1ec"), []byte("00dad1ec"), 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/2.1.285/manifest.json" {
			w.Write(raw)
		} else if r.URL.Path == "/2.1.285/manifest.json.sig" {
			w.Write(sig)
		} else {
			t.Fatal("artifact fetched before signature verification")
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	db, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	c := New(db, &distributor.Client{Base: u, HTTP: server.Client()})
	if _, e = c.Authorize(context.Background(), "2.1.285", "linux-x64", "claude"); e == nil {
		t.Fatal("tampered metadata authorized")
	}
	items, _ := db.List("claude-metadata")
	versions, _ := db.VersionsFor(ID)
	if len(items) != 0 || len(versions) != 0 {
		t.Fatal("invalid metadata published")
	}
}
func TestManifestBoundsAndCleanupIsolation(t *testing.T) {
	raw, _ := fixture(t)
	var m map[string]any
	json.Unmarshal(raw, &m)
	for _, v := range []string{"../2.1.285", "2.1.285/../../", "02.1.285", "2.1.285\n", "2.1.285?x", "999999999999999999999.1.2"} {
		if ValidVersion(v) {
			t.Fatal(v)
		}
	}
	for _, change := range []func(){func() { m["version"] = "2.1.286" }, func() {
		m["platforms"] = map[string]any{"linux-x64": map[string]any{"binary": "../../secret", "checksum": "00", "size": 1}}
	}} {
		change()
		b, _ := json.Marshal(m)
		if _, e := parse(b, "2.1.285"); e == nil {
			t.Fatal("invalid metadata")
		}
	}
	if _, e := parse([]byte(`{"version":"2.1.285","version":"2.1.285"}`), "2.1.285"); e == nil {
		t.Fatal("duplicate keys")
	}
	views := []download.View{}
	for _, a := range []string{ID, "codex", "", "unknown"} {
		views = append(views, download.View{Generation: download.Generation{Resource: download.Resource{ID: a, Labels: map[string]string{"app": a, "version": "2.1.284"}}}})
	}
	ids, unknown, e := (&Catalog{}).Candidates("2.1.285", views)
	if e != nil || len(ids) != 1 || !ids[ID] || len(unknown) != 0 {
		t.Fatalf("cleanup escaped scope: %v %v %v", ids, unknown, e)
	}
}

func TestChannelTTLIsSeparatePersistentAndDoesNotServeStaleOnFailure(t *testing.T) {
	raw, sig := fixture(t)
	var failed atomic.Bool
	var latest, stable, manifests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			latest.Add(1)
			if failed.Load() {
				http.Error(w, "unavailable", 503)
				return
			}
			io.WriteString(w, "2.1.285")
		case "/stable":
			stable.Add(1)
			io.WriteString(w, "2.1.285")
		case "/2.1.285/manifest.json":
			manifests.Add(1)
			w.Write(raw)
		case "/2.1.285/manifest.json.sig":
			w.Write(sig)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	client := &distributor.Client{Base: u, HTTP: server.Client()}
	c := New(db, client)
	for _, target := range []string{"latest", "stable", "latest"} {
		if _, err = c.Get(context.Background(), target); err != nil {
			t.Fatal(err)
		}
	}
	if err = c.SetTTL(180); err != nil {
		t.Fatal(err)
	}
	c = New(db, client)
	if c.LatestTTLSeconds() != 180 {
		t.Fatal("TTL not persisted")
	}
	db.Put("claude-channel", "latest", channel{Version: "2.1.285"})
	failed.Store(true)
	if _, err = c.Get(context.Background(), "latest"); err == nil {
		t.Fatal("expired alias served stale metadata")
	}
	if _, err = c.Get(context.Background(), "stable"); err != nil {
		t.Fatal("failure crossed channels", err)
	}
	if _, err = c.Get(context.Background(), "2.1.285"); err != nil {
		t.Fatal("version expired", err)
	}
	if latest.Load() != 2 || stable.Load() != 1 || manifests.Load() != 1 {
		t.Fatal("channel/version caching is inconsistent")
	}
}

func TestManifestRejectsInvalidPlatformFields(t *testing.T) {
	raw, _ := fixture(t)
	for _, mutate := range []func(*Manifest){
		func(m *Manifest) { m.Platforms["unreviewed-platform"] = m.Platforms["linux-x64"] },
		func(m *Manifest) {
			p := m.Platforms["linux-x64"]
			p.Binary = "claude.exe"
			m.Platforms["linux-x64"] = p
		},
		func(m *Manifest) { p := m.Platforms["linux-x64"]; p.Checksum = ""; m.Platforms["linux-x64"] = p },
		func(m *Manifest) {
			p := m.Platforms["linux-x64"]
			p.Checksum = strings.Repeat("z", 64)
			m.Platforms["linux-x64"] = p
		},
		func(m *Manifest) { p := m.Platforms["linux-x64"]; p.Size = 0; m.Platforms["linux-x64"] = p },
		func(m *Manifest) { p := m.Platforms["linux-x64"]; p.Size = 1 << 40; m.Platforms["linux-x64"] = p },
	} {
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		mutate(&m)
		body, _ := json.Marshal(m)
		if _, err := parse(body, "2.1.285"); err == nil {
			t.Fatal("invalid platform accepted")
		}
	}
}
