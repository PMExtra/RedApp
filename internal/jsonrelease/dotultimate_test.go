package jsonrelease_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/PMExtra/RedApp/internal/jsonrelease"
	"github.com/PMExtra/RedApp/presets"
)

// Captured from the official RSU release API and its windowsZIP SHA256 sidecar
// on 2026-10-27. Keep the real download keys: synthetic offline fields hid a bug.
// Only metadata and the sidecar were fetched; the archive was not downloaded.
func TestDotUltimateRecordedReleaseDiscovery(t *testing.T) {
	metadata, err := os.ReadFile("testdata/dotultimate-release.json")
	if err != nil {
		t.Fatal(err)
	}
	checksum, err := os.ReadFile("testdata/dotultimate.zip.sha256")
	if err != nil {
		t.Fatal(err)
	}
	const metadataURL = "https://data.services.jetbrains.com/products/releases?code=RSU&latest=true&type=release"
	const archiveURL = "https://download.jetbrains.com/resharper/dotUltimate.2026.2.3.1/JetBrains.dotUltimate.2026.2.3.1.zip"
	for _, app := range presets.Embedded().Apps {
		if app.Key() != "jetbrains/dotultimate" {
			continue
		}
		config := *app.Spec.JSONRelease
		if len(config.Prewarm) != 0 {
			t.Fatal("unexpected automatic prewarm")
		}
		fetch := func(_ context.Context, raw string, allow func(string) bool) (*http.Response, error) {
			if !allow(raw) {
				return nil, fmt.Errorf("unauthorized source: %s", raw)
			}
			var body []byte
			switch raw {
			case metadataURL:
				body = metadata
			case archiveURL + ".sha256":
				body = checksum
			default:
				return nil, fmt.Errorf("unexpected request (no package downloads): %s", raw)
			}
			request, err := http.NewRequest(http.MethodGet, raw, nil)
			if err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Request: request, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))}, nil
		}
		result, err := (jsonrelease.Discoverer{Query: jsonrelease.NewRunner().Run, Fetch: fetch}).Discover(context.Background(), config, "stable")
		if err != nil {
			t.Fatal(err)
		}
		if result.Version != "2026.2.3.1" || len(result.Artifacts) != 1 {
			t.Fatal(result)
		}
		a := result.Artifacts[0]
		if a.Key != "windows-offline-suite" || a.OS != "windows" || a.Arch != "unknown" || a.Format != "zip" || a.Variant != "offline-suite" || a.URL != archiveURL {
			t.Fatal("wrong offline suite mapping", a)
		}
		if a.Size == nil || *a.Size != 4777835851 {
			t.Fatal("whole archive size was lost", a)
		}
		if a.ChecksumFilename != "JetBrains.dotUltimate.2026.2.3.1.zip" || a.SHA256 != "fda4c82dc0f86309a080abd703823c7045ce7ba39e0f4bb67bbde661b990082b" {
			t.Fatal("wrong archive checksum", a)
		}
		return
	}
	t.Fatal("missing dotUltimate preset")
}
