package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactByteSizeBoundaries(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  int64 // zero means the configuration must be rejected
	}{
		{"4294967296", 4 << 30}, {"4gb", 4000000000}, {"4GiB", 4 << 30},
		{" \t1.5 GiB \n", 3 << 29}, {"1B", 1}, {"1TiB", 1 << 40},
		{"", 0}, {"0", 0}, {"-1GiB", 0}, {"0.5B", 0}, {"4unknown", 0},
		{"1099511627777", 0}, {"2TiB", 0},
		{"18446744073709551615", 0}, // uint64-valid, rejected before int64 conversion
		{"16EiB", 0},                // overflow rejected by the library
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := artifactBytes(tc.input)
			if tc.want == 0 {
				if err == nil {
					t.Fatalf("accepted %q as %d", tc.input, got)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("got %d, %v; want %d", got, err, tc.want)
			}
		})
	}
}

func TestArtifactSizeFilesPreserveStrictConfiguration(t *testing.T) {
	cleanEnvironment(t)
	for _, tc := range []struct {
		name, extension, body string
		want                  int64
	}{
		{"json integer", ".json", `{"download_limits":{"max_artifact_bytes":4294967296}}`, 4 << 30},
		{"yaml integer", ".yaml", "download_limits: {max_artifact_bytes: 4294967296}", 4 << 30},
		{"json size", ".json", `{"download_limits":{"max_artifact_bytes":"4gb"}}`, 4000000000},
		{"yaml size", ".yaml", "download_limits: {max_artifact_bytes: '1.5GiB'}", 3 << 29},
		{"json float", ".json", `{"download_limits":{"max_artifact_bytes":1.5}}`, 0},
		{"json null", ".json", `{"download_limits":{"max_artifact_bytes":null}}`, 0},
		{"json duplicate", ".json", `{"download_limits":{"max_artifact_bytes":"4GiB","max_artifact_bytes":1}}`, 0},
		{"json unknown", ".json", `{"download_limits":{"max_artifact_bytes":"4GiB","extra":1}}`, 0},
		{"other field type", ".json", `{"download_limits":{"max_artifact_bytes":"4GiB","max_readers":"16"}}`, 0},
		{"yaml limit", ".yaml", "download_limits: {max_artifact_bytes: '2TiB'}", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config"+tc.extension)
			writeConfig(t, path, tc.body)
			var overrides map[string]string
			if tc.want == 0 {
				// A valid higher-priority size cannot conceal invalid file content.
				overrides = map[string]string{"max-artifact-bytes": "4GiB"}
			}
			c, err := load(path, path, overrides)
			if tc.want == 0 {
				if err == nil || !strings.Contains(err.Error(), path) {
					t.Fatalf("invalid selected file was hidden: %v", err)
				}
			} else if err != nil || c.DownloadLimits != (DownloadLimits{16, 512, tc.want}) {
				t.Fatal(c.DownloadLimits, err)
			}
		})
	}
}
