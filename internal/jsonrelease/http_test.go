package jsonrelease

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/releaseconfig"
)

// Synthetic upstream data: this is not a verification of a vendor endpoint.
func TestDiscoverEmbeddedQueryOverBoundedHTTP(t *testing.T) {
	digest := strings.Repeat("a", 64)
	var origin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			if r.URL.Query().Get("platform") != "windows" {
				t.Error("request query missing")
			}
			fmt.Fprintf(w, `{"productVersion":"1.2.3","version":"abcdef","url":%q,"sha256hash":%q}`, origin+"/files/setup.exe", digest)
		case "/files/setup.exe":
			t.Error("preview downloaded artifact")
			w.WriteHeader(500)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	origin = server.URL
	client, err := distributor.NewPool().NewClient(origin, distributor.ConfiguredRelease)
	if err != nil {
		t.Fatal(err)
	}
	config := releaseconfig.Config{DefaultChannel: "stable", RefreshSeconds: 300,
		Sources:  releaseconfig.Sources{Metadata: []releaseconfig.Source{{Origin: origin, Prefix: "/"}}, Artifacts: []releaseconfig.Source{{Origin: origin, Prefix: "/files/"}}},
		Channels: map[string]releaseconfig.Channel{"stable": {Requests: []releaseconfig.Request{{URL: origin + "/api?platform=windows", Context: map[string]string{"key": "win-x64-user", "os": "windows", "arch": "x64"}}}, Query: `{version:.body.productVersion,build:.body.version,artifacts:[{key:.context.key,os:.context.os,arch:.context.arch,format:"exe",scope:"user",url:.body.url,sha256:.body.sha256hash}]}`}},
	}
	release, err := NewDiscoverer(NewRunner(), client).Discover(context.Background(), config, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "1.2.3" || release.Build != "abcdef" || len(release.Artifacts) != 1 || release.Artifacts[0].SHA256 != digest {
		t.Fatalf("%+v", release)
	}
	snapshot, err := NewSnapshot(release)
	if err != nil {
		t.Fatal(err)
	}
	path, err := snapshot.DownloadPath("microsoft/vscode", "win-x64-user")
	if err != nil || !strings.HasPrefix(path, "/microsoft/vscode/releases/r-") {
		t.Fatal(path, err)
	}
}
