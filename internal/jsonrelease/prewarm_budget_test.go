package jsonrelease_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/catalog"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/jsonrelease"
	"github.com/PMExtra/RedApp/internal/prewarm"
	"github.com/PMExtra/RedApp/internal/releaseconfig"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/store/storetest"
	"github.com/PMExtra/RedApp/internal/warmplan"
)

func TestPrewarmReleaseReadBudget(t *testing.T) {
	for _, known := range []bool{false, true} {
		for _, tc := range []struct {
			name      string
			delta     int64
			badHash   bool
			truncated bool
		}{
			{"under", 1, false, false}, {"exact", 0, false, false}, {"over", -1, false, false}, {"bad-digest", 0, true, false}, {"truncated", 0, false, true},
		} {
			t.Run(fmt.Sprintf("%s/known=%t", tc.name, known), func(t *testing.T) {
				ctx := context.Background()
				body := []byte("isolated installer fixture")
				hash := fmt.Sprintf("%x", sha256.Sum256(body))
				var origin string
				var downloaded atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/metadata":
						size := ""
						if known {
							size = fmt.Sprintf(",\"size\":%d", len(body))
						}
						expectedHash := hash
						if tc.badHash {
							expectedHash = strings.Repeat("0", 64)
						}
						fmt.Fprintf(w, `{"version":"1.2.3","build":"abc","artifacts":[{"key":"windows-x64-user","os":"windows","arch":"x64","format":"exe","scope":"user","url":%q,"sha256":%q%s}]}`, origin+"/files/setup.exe", expectedHash, size)
					case "/files/setup.exe":
						downloaded.Add(1)
						if known {
							w.Header().Set("Content-Length", fmt.Sprint(len(body)))
						} else {
							w.(http.Flusher).Flush()
						}
						if tc.truncated {
							w.Write(body[:len(body)-1])
						} else {
							w.Write(body)
						}
					default:
						w.WriteHeader(404)
					}
				}))
				defer upstream.Close()
				origin = upstream.URL
				dir := t.TempDir()
				db, err := store.Open(dir)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				vendor, err := db.CreateVendor(store.VendorInput{ID: "fixture", Name: store.LocalizedText{En: "Fixture", ZhCN: "测试"}, Enabled: true})
				if err != nil {
					t.Fatal(err)
				}
				config := releaseconfig.Config{DefaultChannel: "stable", RefreshSeconds: 60, Channels: map[string]releaseconfig.Channel{"stable": {Query: ".body", Requests: []releaseconfig.Request{{URL: origin + "/metadata"}}}}, Sources: releaseconfig.Sources{Metadata: []releaseconfig.Source{{Origin: origin, Prefix: "/"}}, Artifacts: []releaseconfig.Source{{Origin: origin, Prefix: "/files/"}}}, Prewarm: []releaseconfig.PrewarmSelection{{Channel: "stable", Keys: []string{"windows-x64-user"}}}}
				app, err := db.CreateApplication(vendor.ID, store.ApplicationInput{ID: "editor", Name: vendor.Name, Provider: application.JSONRelease, JSONRelease: &config, Enabled: true})
				if err != nil {
					t.Fatal(err)
				}
				client, err := distributor.NewPool().NewClient(origin, distributor.ConfiguredRelease)
				if err != nil {
					t.Fatal(err)
				}
				protocol, err := jsonrelease.NewProtocol(config, jsonrelease.NewDiscoverer(jsonrelease.NewRunner(), client))
				if err != nil {
					t.Fatal(err)
				}
				artifactClient, err := client.WithReleaseSources(func(raw string) bool { return releaseconfig.Allows(config.Sources.Artifacts, raw) })
				if err != nil {
					t.Fatal(err)
				}
				entry := application.Entry{UID: app.UID, Revision: app.Revision, VendorRevision: vendor.Revision, RuntimeRevision: app.RuntimeRevision, VendorRuntimeRevision: vendor.RuntimeRevision, SourceEpoch: app.SourceEpoch, Enabled: true, Provider: application.JSONRelease, Protocol: protocol, Upstream: artifactClient, Descriptor: application.Descriptor{ID: app.Key, TrustRevision: 1, DefaultChannelTTLSeconds: 60, Channels: []string{"latest", "stable"}}}
				registry, err := application.NewRegistry([]application.Entry{entry})
				if err != nil {
					t.Fatal(err)
				}
				service := catalog.New(db, registry)
				manager, err := download.NewApplications(dir, db, registry.Upstreams())
				if err != nil {
					t.Fatal(err)
				}
				defer manager.Close()
				worker, err := prewarm.New(db, registry, service, manager, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer worker.Close()

				limits := warmplan.DefaultLimits()
				limits.MaxDownloadBytes = int64(len(body)) + tc.delta
				job, _, err := worker.Start(ctx, app.Key, warmplan.Input{RequestID: strings.Repeat("a", 32), Target: "stable", ArtifactKeys: []string{"windows-x64-user"}, Limits: limits}, false)
				if err != nil {
					t.Fatal(err)
				}
				wait, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				job, err = worker.Wait(wait, app.UID, job.ID)
				if err != nil {
					t.Fatal(err)
				}
				if tc.badHash || tc.truncated {
					if job.Succeeded != 0 {
						t.Fatal("unverified bytes counted as success", job)
					}
				} else if tc.delta < 0 {
					if job.State != "limited" || job.Succeeded != 0 {
						t.Fatal("oversize marked complete", job)
					}
				} else if job.State != "completed" || job.Succeeded != 1 || job.Bytes != int64(len(body)) {
					t.Fatal("complete verified file rejected", job)
				}
				if job.Bytes > limits.MaxDownloadBytes+1 {
					t.Fatal("unbounded boundary probe", job)
				}
				sql := storetest.Open(t, dir)
				var complete int
				if err := sql.QueryRow("SELECT count(*) FROM generations WHERE phase='complete'").Scan(&complete); err != nil {
					t.Fatal(err)
				}
				if (tc.badHash || tc.truncated) && complete != 0 {
					t.Fatal("invalid digest published")
				}
				if !tc.badHash && !tc.truncated && tc.delta >= 0 {
					if downloaded.Load() != 1 {
						t.Fatal("unexpected repeated package request", downloaded.Load())
					}
					var bytes int64
					var digest string
					if err := sql.QueryRow("SELECT size_bytes,sha256 FROM blobs").Scan(&bytes, &digest); err != nil {
						t.Fatal(err)
					}
					if bytes != int64(len(body)) || digest != hash || complete != 1 {
						t.Fatal("partial or wrong blob published", bytes, digest, complete)
					}
				}
			})
		}
	}
}
