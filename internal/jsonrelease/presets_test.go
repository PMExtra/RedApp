package jsonrelease_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/PMExtra/RedApp/internal/jsonrelease"
	"github.com/PMExtra/RedApp/internal/releaseconfig"
	"github.com/PMExtra/RedApp/presets"
	"io"
	"net/http"
	"strings"
	"testing"
)

// These are synthetic API/sidecar fixtures, NOT captured official responses.
func TestIDEConfigurationsUseOneQueryEngine(t *testing.T) {
	for _, app := range presets.Embedded().Apps {
		if app.Spec.Provider != "json-release" {
			continue
		}
		t.Run(app.Key(), func(t *testing.T) {
			config := *app.Spec.JSONRelease
			fetchedArtifacts := false
			fetch := func(_ context.Context, raw string, allow func(string) bool) (*http.Response, error) {
				if !allow(raw) {
					return nil, fmt.Errorf("unauthorized fixture request")
				}
				var body string
				switch {
				case strings.HasPrefix(raw, "https://update.code.visualstudio.com/"):
					encoded, _ := json.Marshal(map[string]any{"productVersion": "1.2.3", "version": "abc123", "url": "https://vscode.download.prss.microsoft.com/dbazure/download/stable/abc/setup.zip", "sha256hash": strings.Repeat("a", 64)})
					body = string(encoded)
				case strings.HasPrefix(raw, "https://data.services.jetbrains.com/"):
					request := config.Channels["stable"].Requests[0]
					code := request.Context["product"]
					prefix := map[string]string{"IIU": "idea", "PCP": "python", "CL": "cpp", "WS": "webstorm", "PS": "webide", "GO": "go", "RD": "rider", "DG": "datagrip", "RM": "ruby", "RR": "rustrover", "RSU": "resharper"}[code]
					base := "https://download.jetbrains.com/" + prefix + "/"
					encoded, _ := json.Marshal(map[string]any{code: []any{map[string]any{"version": "2026.2", "build": "262.1", "downloads": map[string]any{"windows": map[string]any{"link": base + "setup.exe", "checksumLink": base + "setup.exe.sha256", "size": 123}, "macM1": map[string]any{"link": base + "setup.dmg", "checksumLink": base + "setup.dmg.sha256", "size": 456}, "thirdPartyLibrariesJson": map[string]any{"link": "https://untrusted.example/libraries.json"}}}}})
					if code == "RSU" {
						encoded, _ = json.Marshal(map[string]any{code: []any{map[string]any{"version": "2026.2", "build": "262.1", "downloads": map[string]any{"windowsZIP": map[string]any{"link": base + "suite.zip", "checksumLink": base + "suite.zip.sha256", "size": 789}}}}})
					}
					body = string(encoded)
				case strings.HasSuffix(raw, ".sha256"):
					parts := strings.Split(strings.TrimSuffix(raw, ".sha256"), "/")
					body = strings.Repeat("a", 64) + "  " + parts[len(parts)-1] + "\n"
				default:
					fetchedArtifacts = true
					return nil, fmt.Errorf("unexpected installer download")
				}
				request, _ := http.NewRequest(http.MethodGet, raw, nil)
				return &http.Response{StatusCode: 200, Header: http.Header{}, Request: request, ContentLength: int64(len(body)), Body: io.NopCloser(strings.NewReader(body))}, nil
			}
			result, err := (jsonrelease.Discoverer{Query: jsonrelease.NewRunner().Run, Fetch: fetch}).Discover(context.Background(), config, "stable")
			if err != nil {
				t.Fatal(err)
			}
			expected := 2
			if app.Key() == "jetbrains/dotultimate" {
				expected = 1
			}
			if app.Key() == "microsoft/vscode" {
				expected = len(config.Channels["stable"].Requests)
			}
			if len(result.Artifacts) != expected || fetchedArtifacts {
				t.Fatal("wrong package mapping", len(result.Artifacts), fetchedArtifacts)
			}
			for _, a := range result.Artifacts {
				if strings.Contains(a.URL, "libraries") || (a.Arch == "unknown" && app.Key() != "jetbrains/dotultimate") || a.SHA256 != strings.Repeat("a", 64) {
					t.Fatal(a)
				}
			}
		})
	}
}

// CL and /cpp/ are configured candidates, not live-verified API observations.
// Exercise every declared platform and both source roles without fetching packages.
func TestCLionPresetPlatformsAndSourceBoundaries(t *testing.T) {
	for _, app := range presets.Embedded().Apps {
		if app.Key() != "jetbrains/clion" {
			continue
		}
		c := *app.Spec.JSONRelease
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
		request := c.Channels["stable"].Requests[0]
		if request.Context["product"] != "CL" || request.URL != "https://data.services.jetbrains.com/products/releases?code=CL&latest=true&type=release" {
			t.Fatal(request)
		}
		downloads := map[string]any{}
		expected := map[string]string{"windows": "windows-x64-exe", "windowsARM64": "windows-arm64-exe", "linux": "linux-x64-tar", "linuxARM64": "linux-arm64-tar", "mac": "macos-x64-dmg", "macM1": "macos-arm64-dmg"}
		for key := range expected {
			downloads[key] = map[string]any{"link": "https://download.jetbrains.com/cpp/" + key, "checksumLink": "https://download.jetbrains.com/cpp/" + key + ".sha256", "size": 123}
		}
		downloads["thirdPartyLibrariesJson"] = map[string]any{"link": "https://untrusted.example/libraries.json"}
		downloads["futureUnknownPlatform"] = map[string]any{"link": "https://untrusted.example/new-package"}
		input, err := json.Marshal(map[string]any{"context": request.Context, "body": map[string]any{"CL": []any{map[string]any{"version": "2026.2", "build": "262.1", "downloads": downloads}}}})
		if err != nil {
			t.Fatal(err)
		}
		output, err := jsonrelease.NewRunner().Run(context.Background(), c.Channels["stable"].Query, input)
		if err != nil {
			t.Fatal(err)
		}
		result, err := jsonrelease.DecodeRelease(output, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Artifacts) != len(expected) {
			t.Fatal(result)
		}
		keys := map[string]bool{}
		for _, artifact := range result.Artifacts {
			keys[artifact.Key] = true
		}
		for _, key := range expected {
			if !keys[key] {
				t.Fatal("missing normalized platform", key)
			}
		}
		if err = c.AuthorizeArtifacts(result); err != nil {
			t.Fatal(err)
		}
		for _, origin := range []string{"https://download.jetbrains.com", "https://download-cdn.jetbrains.com"} {
			for _, role := range [][]releaseconfig.Source{c.Sources.Artifacts, c.Sources.Checksums} {
				if !releaseconfig.Allows(role, origin+"/cpp/CLion-fixture.tar.gz") || !releaseconfig.Allows(role, origin+"/cpp/CLion-fixture.tar.gz.sha256") {
					t.Fatal("expected source denied", origin)
				}
				for _, path := range []string{"/idea/setup.exe", "/python/setup.exe", "/cpp/../idea/setup.exe", "/cpp/%2e%2e/idea/setup.exe", "/cpp-other/setup.exe"} {
					if releaseconfig.Allows(role, origin+path) {
						t.Fatal("source boundary escaped", origin+path)
					}
				}
				if releaseconfig.Allows(role, "https://untrusted.example/cpp/setup.exe") {
					t.Fatal("untrusted source allowed")
				}
			}
		}
		return
	}
	t.Fatal("CLion preset missing")
}

func TestExpandedPresetInventoryAndSourcePolicies(t *testing.T) {
	var ideQuery string
	for _, app := range presets.Embedded().Apps {
		if app.Key() == "jetbrains/clion" {
			ideQuery = app.Spec.JSONRelease.Channels["stable"].Query
		}
	}
	expected := map[string]string{"idea": "idea", "pycharm": "python", "clion": "cpp", "webstorm": "webstorm", "phpstorm": "webide", "goland": "go", "rider": "rider", "datagrip": "datagrip", "rubymine": "ruby", "rustrover": "rustrover", "dotultimate": "resharper"}
	count := 0
	for _, app := range presets.Embedded().Apps {
		if app.Spec.Provider != "json-release" {
			continue
		}
		count++
		if app.Key() == "microsoft/vscode" {
			continue
		}
		prefix, ok := expected[app.Metadata.ID]
		if !ok {
			t.Fatal("unexpected candidate", app.Key())
		}
		delete(expected, app.Metadata.ID)
		config := app.Spec.JSONRelease
		if app.Metadata.ID != "dotultimate" && (ideQuery == "" || config.Channels["stable"].Query != ideQuery) {
			t.Fatal("IDE must reuse the six-platform fixture-validated transform", app.Key())
		}
		for _, role := range [][]releaseconfig.Source{config.Sources.Artifacts, config.Sources.Checksums} {
			for _, origin := range []string{"https://download.jetbrains.com", "https://download-cdn.jetbrains.com"} {
				if !releaseconfig.Allows(role, origin+"/"+prefix+"/fixture.zip") || !releaseconfig.Allows(role, origin+"/"+prefix+"/fixture.zip.sha256") {
					t.Fatal(app.Key(), origin)
				}
				for _, bad := range []string{origin + "/other/fixture.zip", origin + "/" + prefix + "/../other/fixture.zip", "https://untrusted.example/" + prefix + "/fixture.zip"} {
					if releaseconfig.Allows(role, bad) {
						t.Fatal("source escaped", app.Key(), bad)
					}
				}
			}
		}
	}
	if count != 12 || len(expected) != 0 {
		t.Fatal(count, expected)
	}
}

func TestDotUltimateRejectsWebInstallerAndKeepsWholeArchive(t *testing.T) {
	for _, app := range presets.Embedded().Apps {
		if app.Key() != "jetbrains/dotultimate" {
			continue
		}
		config := app.Spec.JSONRelease
		request := config.Channels["stable"].Requests[0]
		for _, test := range []struct {
			key, filename string
			valid         bool
		}{{"windowsWeb", "web.exe", false}, {"windowsWebLP", "web.exe", false}, {"toolboxWindows", "setup.exe", false}, {"windowsOffline", "suite.zip", false}, {"windowsZIP", "suite.zip", true}, {"windowsZIP", "suite.exe", false}, {"windowsZIP", "payload.dat", false}} {
			input, _ := json.Marshal(map[string]any{"context": request.Context, "body": map[string]any{request.Context["product"]: []any{map[string]any{"version": "2026.2", "build": "262.1", "downloads": map[string]any{test.key: map[string]any{"link": "https://download.jetbrains.com/resharper/" + test.filename, "checksumLink": "https://download.jetbrains.com/resharper/" + test.filename + ".sha256", "size": 789}}}}}})
			output, err := jsonrelease.NewRunner().Run(context.Background(), config.Channels["stable"].Query, input)
			if !test.valid {
				if err == nil {
					t.Fatal("unsafe candidate accepted", test)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			result, err := jsonrelease.DecodeRelease(output, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Artifacts) != 1 || result.Artifacts[0].Key != "windows-offline-suite" || !strings.HasSuffix(result.Artifacts[0].URL, "/"+test.filename) || result.Artifacts[0].Arch != "unknown" {
				t.Fatal(result)
			}
		}
		return
	}
	t.Fatal("missing dotUltimate candidate")
}
