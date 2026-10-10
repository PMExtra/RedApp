package claude

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/catalog"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/prewarm"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
	"github.com/PMExtra/RedApp/internal/warmplan"
	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Both key and signature exist only in the test binary. Verification keeps the
// production RSA4096/SHA512, pinned fingerprint, packet, time and digest gates.
func TestSignedClaudePrewarmRealComponentPipeline(t *testing.T) {
	signer, err := openpgp.NewEntity("Prewarm integration fixture", "Test only", "fixture@example.invalid", &packet.Config{RSABits: 4096, DefaultHash: crypto.SHA512})
	if err != nil {
		t.Fatal(err)
	}
	var key bytes.Buffer
	armored, err := armor.Encode(&key, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = signer.Serialize(armored); err != nil {
		t.Fatal(err)
	}
	if err = armored.Close(); err != nil {
		t.Fatal(err)
	}
	pin := hex.EncodeToString(signer.PrimaryKey.Fingerprint)
	payload := []byte("small signed Claude artifact fixture\n")
	sum := sha256.Sum256(payload)
	manifest := Manifest{Version: "1.2.3", Platforms: map[string]Platform{"linux-x64": {Binary: "claude", Checksum: hex.EncodeToString(sum[:]), Size: int64(len(payload))}}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var signature bytes.Buffer
	if err = openpgp.ArmoredDetachSign(&signature, signer, bytes.NewReader(raw), &packet.Config{DefaultHash: crypto.SHA512}); err != nil {
		t.Fatal(err)
	}
	if Verify(raw, signature.Bytes()) == nil {
		t.Fatal("default official trust accepted a test signing key")
	}
	var downloads atomic.Int64
	var badSignature, badDigest atomic.Bool
	upstream, origin := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/1.2.3/manifest.json":
			w.Write(raw)
		case "/1.2.3/manifest.json.sig":
			if badSignature.Load() {
				io.WriteString(w, "invalid signature")
			} else {
				w.Write(signature.Bytes())
			}
		case "/1.2.3/linux-x64/claude":
			downloads.Add(1)
			if badDigest.Load() {
				w.Write(bytes.Repeat([]byte("x"), len(payload)))
			} else {
				w.Write(payload)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	verify := func(raw, sig []byte) error { return verifyPinnedKey(raw, sig, key.Bytes(), pin) }
	for _, scenario := range []string{"success", "invalid-signature", "invalid-binary-digest"} {
		t.Run(scenario, func(t *testing.T) {
			badSignature.Store(scenario == "invalid-signature")
			badDigest.Store(scenario == "invalid-binary-digest")
			downloads.Store(0)
			dir := t.TempDir()
			db, err := store.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer db.DB.Close()
			vendor, err := db.CreateVendor(store.VendorInput{ID: "signed", Name: store.LocalizedText{En: "Signed", ZhCN: "签名"}, Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			app, err := db.CreateApplication(vendor.ID, store.ApplicationInput{ID: "claude", Name: store.LocalizedText{En: "Claude", ZhCN: "Claude"}, Provider: application.ClaudeCode, BaseURL: origin.URL, CacheTTLSeconds: 60, Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			entry := application.Entry{Descriptor: application.Descriptor{ID: app.Key, TrustRevision: 1, Channels: []string{"latest", "stable"}, DefaultChannelTTLSeconds: 60}, Protocol: newProtocol(upstream, verify), Upstream: upstream, Provider: application.ClaudeCode, UID: app.UID, VendorUID: vendor.UID, SourceEpoch: app.SourceEpoch, Revision: app.Revision, VendorRevision: vendor.Revision, RuntimeRevision: app.RuntimeRevision, VendorRuntimeRevision: vendor.RuntimeRevision, Enabled: true}
			registry, err := application.NewRegistry([]application.Entry{entry})
			if err != nil {
				t.Fatal(err)
			}
			metadata := catalog.New(db, registry)
			manager, err := download.NewApplications(dir, db, map[string]*distributor.Client{entry.StorageID(): upstream})
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close()
			worker, err := prewarm.New(db, registry, metadata, manager, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer worker.Close()
			// Verified metadata alone must not count as a cached binary.
			if scenario == "success" {
				release, err := metadata.Release(context.Background(), app.Key, "1.2.3")
				if err != nil || len(release.Artifacts) != 1 {
					t.Fatal(release, err)
				}
				if len(manager.SnapshotFor(entry.StorageID(), "1.2.3")) != 0 || downloads.Load() != 0 {
					t.Fatal("metadata fetched a binary")
				}
			}
			start := func(requestID string) store.PrewarmJob {
				limits := warmplan.DefaultLimits()
				limits.MaxDownloadBytes = int64(len(payload))
				job, err := worker.Start(context.Background(), app.Key, warmplan.Input{RequestID: requestID, Target: "1.2.3", Platforms: []string{"linux-x64"}, Limits: limits}, false)
				if err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					job, err = worker.Status(app.UID, job.ID)
					if err != nil {
						t.Fatal(err)
					}
					if job.State != "running" {
						return job
					}
					time.Sleep(time.Millisecond)
				}
				t.Fatal("prewarm did not finish")
				return job
			}
			job := start(strings.Repeat("a", 32))
			if scenario != "success" {
				if job.State != "completed_with_errors" || job.Succeeded != 0 {
					t.Fatal(job)
				}
				if scenario == "invalid-signature" && downloads.Load() != 0 {
					t.Fatal("invalid signature started binary download")
				}
				for _, view := range manager.SnapshotFor(entry.StorageID(), "1.2.3") {
					if view.State == "complete" {
						t.Fatal("unverified binary became complete")
					}
				}
				return
			}
			if job.State != "completed" || job.Succeeded != 1 || job.Bytes != int64(len(payload)) || downloads.Load() != 1 {
				t.Fatal(job, downloads.Load())
			}
			views := manager.SnapshotFor(entry.StorageID(), "1.2.3")
			if len(views) != 1 || views[0].State != "complete" || views[0].Resource.Hash != hex.EncodeToString(sum[:]) {
				t.Fatal(views)
			}
			items, total, err := db.PrewarmItems(app.UID, job.ID, 1, 25)
			if err != nil || total != 1 || items[0].Status != "downloaded" {
				t.Fatal(items, total, err)
			}
			hit := start(strings.Repeat("b", 32))
			if hit.State != "completed" || hit.Bytes != 0 || hit.Succeeded != 1 || downloads.Load() != 1 {
				t.Fatal(hit, downloads.Load())
			}
			items, _, err = db.PrewarmItems(app.UID, hit.ID, 1, 25)
			if err != nil || items[0].Status != "cached" {
				t.Fatal(items, err)
			}
		})
	}
}
