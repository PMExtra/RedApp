package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func cleanEnvironment(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "REDAPP_") {
			t.Setenv(name, "")
			os.Unsetenv(name)
		}
	}
}

func writeConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestOptionalFileDefaultsAndProxyTrust(t *testing.T) {
	cleanEnvironment(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// Only the default path is discovered, never a sibling in the same directory.
	writeConfig(t, filepath.Join(dir, "redapp.yaml"), "listen: ':9191'\n")
	c, err := load("", path, nil)
	if err != nil || c.SchemaVersion != 1 || c.DataDir != "/var/lib/redapp" || c.Listen != ":8080" || c.DownloadLimits != (DownloadLimits{16, 512, 4 << 30}) {
		t.Fatal(c, err)
	}
	if len(c.TrustedProxies) != 0 {
		t.Fatalf("unexpected default trust: %+v", c)
	}
	t.Setenv("REDAPP_PUBLIC_URL", "https://publication.example")
	t.Setenv("REDAPP_LISTEN", "0.0.0.0:9090")
	c, err = load("", path, nil)
	if err != nil || c.Listen != "0.0.0.0:9090" || len(c.TrustedProxies) != 0 {
		t.Fatalf("listener/public URL expanded trust: %+v %v", c, err)
	}
	if _, err := load(path, path, nil); err == nil {
		t.Fatal("explicit missing file accepted")
	}
	t.Setenv("REDAPP_CONFIG", path)
	if _, err := load("", path, nil); err == nil {
		t.Fatal("environment-selected missing file accepted")
	}
}

func TestOneSelectedFileAndFieldPrecedence(t *testing.T) {
	cleanEnvironment(t)
	dir := t.TempDir()
	defaultPath, envPath, cliPath := filepath.Join(dir, "config.yaml"), filepath.Join(dir, "env.yaml"), filepath.Join(dir, "cli.yaml")
	writeConfig(t, defaultPath, "# automatically loaded YAML\nlisten: ':9091'\ndata_dir: /default-data\ndownload_limits:\n  max_readers: 600\n")
	c, err := load("", defaultPath, nil)
	if err != nil || c.DataDir != "/default-data" || c.Listen != ":9091" || c.DownloadLimits.MaxReaders != 600 {
		t.Fatal(c, err)
	}
	writeConfig(t, envPath, "data_dir: /file-data\ntrusted_proxies: [10.0.0.0/8]\ndownload_limits: {max_readers: 700}\n")
	t.Setenv("REDAPP_CONFIG", envPath)
	c, err = load("", defaultPath, nil)
	if err != nil || c.DataDir != "/file-data" || c.Listen != ":8080" {
		t.Fatalf("selected file merged with default file: %+v %v", c, err)
	}
	// Neither an invalid default file nor an invalid env path is read when CLI selects a file.
	writeConfig(t, defaultPath, "invalid: [")
	writeConfig(t, cliPath, "data_dir: /cli-file-data\ndownload_limits: {max_readers: 800, max_artifact_bytes: '4GiB'}\n")
	t.Setenv("REDAPP_CONFIG", filepath.Join(dir, "missing.yaml"))
	c, err = load(cliPath, defaultPath, nil)
	if err != nil || c.DataDir != "/cli-file-data" || c.DownloadLimits.MaxReaders != 800 || c.DownloadLimits.MaxArtifactBytes != 4<<30 {
		t.Fatal(c, err)
	}
	t.Setenv("REDAPP_DATA", "/environment-data")
	t.Setenv("REDAPP_LISTEN", "127.0.0.1:9191")
	t.Setenv("REDAPP_TRUSTED_PROXIES", "192.0.2.0/24")
	t.Setenv("REDAPP_MAX_WRITERS", "20")
	t.Setenv("REDAPP_MAX_READERS", "900")
	t.Setenv("REDAPP_MAX_ARTIFACT_BYTES", "1GiB")
	c, err = load(cliPath, defaultPath, nil)
	if err != nil || c.DataDir != "/environment-data" || c.Listen != "127.0.0.1:9191" || c.DownloadLimits != (DownloadLimits{20, 900, 1073741824}) || !reflect.DeepEqual(c.TrustedProxies, []string{"192.0.2.0/24"}) {
		t.Fatal(c, err)
	}
	c, err = load(cliPath, defaultPath, map[string]string{"data": "/flag-data", "listen": ":9292", "trusted-proxies": "", "max-writers": "21", "max-readers": "901", "max-artifact-bytes": "1.5GiB"})
	if err != nil || c.DataDir != "/flag-data" || c.Listen != ":9292" || c.DownloadLimits != (DownloadLimits{21, 901, 3 << 29}) || len(c.TrustedProxies) != 0 {
		t.Fatal(c, err)
	}
}

func TestInvalidSelectedYAMLIsNeverIgnored(t *testing.T) {
	cleanEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, "listen: [")
	if _, err := load("", path, nil); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("invalid automatically selected file was ignored: %v", err)
	}
	for name, body := range map[string]string{
		"empty": "", "syntax": "listen: [", "duplicate": "listen: ':8080'\nlisten: ':9090'",
		"nested duplicate": "download_limits: {max_readers: 10, max_readers: 20}",
		"unknown":          "public_origin: https://wrong.example", "null": "data_dir: null",
		"wrong type": "data_dir: 123", "quoted integer": "schema_version: '1'", "float integer": "schema_version: 1.0",
		"removed hosts": "allowed_hosts: [example.com]", "removed writers": "download_limits: {max_active_writers: 16}",
		"invalid proxy": "trusted_proxies: [hostname]", "bounds": "download_limits: {max_readers: 0}",
		"multiple documents": "{}\n---\n{}", "alias": "trusted_proxies: &proxies [127.0.0.1/32]",
		"nested null": "download_limits: {max_readers: null}", "large": strings.Repeat(" ", 65537),
	} {
		t.Run(name, func(t *testing.T) {
			writeConfig(t, path, body)
			// A higher-priority value cannot hide an invalid selected file.
			if _, err := load(path, path, map[string]string{"data": "/override", "max-readers": "100"}); err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("invalid selected file was hidden: %v", err)
			}
		})
	}
}

func TestInvalidEnvironmentAndFlagsFailBeforeDataAccess(t *testing.T) {
	cleanEnvironment(t)
	path := filepath.Join(t.TempDir(), "absent.yaml")
	for _, tc := range []struct{ env, flag, value string }{
		{"REDAPP_DATA", "data", "relative"}, {"REDAPP_LISTEN", "listen", ":0"},
		{"REDAPP_TRUSTED_PROXIES", "trusted-proxies", "hostname"},
		{"REDAPP_MAX_WRITERS", "max-writers", "1025"}, {"REDAPP_MAX_READERS", "max-readers", "65537"}, {"REDAPP_MAX_ARTIFACT_BYTES", "max-artifact-bytes", "not-a-number"},
	} {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv(tc.env, tc.value)
			if _, err := load("", path, nil); err == nil || !strings.Contains(err.Error(), tc.env) {
				t.Fatal(err)
			}
		})
		if _, err := load("", path, map[string]string{tc.flag: tc.value}); err == nil || !strings.Contains(err.Error(), "--"+tc.flag) {
			t.Fatal(err)
		}
	}
}

func TestRemovedDeploymentOptionsHaveNoAliases(t *testing.T) {
	cleanEnvironment(t)
	path := filepath.Join(t.TempDir(), "absent.yaml")
	for _, flag := range []string{"allowed-hosts", "max-active-writers"} {
		if _, err := load("", path, map[string]string{flag: "16"}); err == nil || !strings.Contains(err.Error(), "unknown deployment option") {
			t.Fatalf("removed option %s accepted: %v", flag, err)
		}
	}
	// Removed environment names are not consulted or translated into new fields.
	t.Setenv("REDAPP_ALLOWED_HOSTS", "*")
	t.Setenv("REDAPP_MAX_ACTIVE_WRITERS", "23")
	c, err := load("", path, nil)
	if err != nil || c.DownloadLimits.MaxWriters != 16 {
		t.Fatalf("removed environment names affected defaults: %+v %v", c, err)
	}
	t.Setenv("REDAPP_MAX_WRITERS", "24")
	c, err = load("", path, nil)
	if err != nil || c.DownloadLimits.MaxWriters != 24 {
		t.Fatalf("new writer environment name not applied: %+v %v", c, err)
	}
}
