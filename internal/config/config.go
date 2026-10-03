// Package config owns deployment configuration and the public-origin setting.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type DownloadLimits struct {
	MaxActiveWriters int   `json:"max_active_writers"`
	MaxReaders       int   `json:"max_readers"`
	MaxArtifactBytes int64 `json:"max_artifact_bytes"`
}

type Deployment struct {
	SchemaVersion        int            `json:"schema_version"`
	Listen               string         `json:"listen"`
	DataDir              string         `json:"data_dir"`
	AllowedHosts         []string       `json:"allowed_hosts"`
	TrustedProxies       []string       `json:"trusted_proxies"`
	DownloadLimits       DownloadLimits `json:"download_limits"`
	EnvironmentPublicURL string         `json:"-"`
}

const DefaultPath = "/etc/redapp/config.yaml"

// Load reads one selected file, then applies environment and explicit CLI fields.
// An absent default file is optional; manually selected files are required.
// PUBLIC_URL remains a separate environment default for persisted admin settings.
func Load(path string, overrides map[string]string) (Deployment, error) {
	return load(path, DefaultPath, overrides)
}

func load(path, defaultPath string, overrides map[string]string) (Deployment, error) {
	c := Deployment{SchemaVersion: 1, Listen: ":8080", DataDir: "/var/lib/redapp",
		DownloadLimits: DownloadLimits{MaxActiveWriters: 16, MaxReaders: 512, MaxArtifactBytes: 4 << 30}}
	if path == "" {
		path = os.Getenv("REDAPP_CONFIG")
	}
	optional := path == ""
	if optional {
		path = defaultPath
	}
	if err := readDeployment(path, optional, &c); err != nil {
		return c, fmt.Errorf("deployment file %q: %w", path, err)
	}
	if err := validate(&c); err != nil {
		return c, fmt.Errorf("deployment file %q: %w", path, err)
	}
	for _, setting := range []struct{ name, flag string }{
		{"REDAPP_DATA", "data"}, {"REDAPP_LISTEN", "listen"},
		{"REDAPP_ALLOWED_HOSTS", "allowed-hosts"}, {"REDAPP_TRUSTED_PROXIES", "trusted-proxies"},
		{"REDAPP_MAX_ACTIVE_WRITERS", "max-active-writers"}, {"REDAPP_MAX_READERS", "max-readers"},
		{"REDAPP_MAX_ARTIFACT_BYTES", "max-artifact-bytes"},
	} {
		if value, ok := os.LookupEnv(setting.name); ok {
			if err := apply(&c, setting.flag, value); err != nil {
				return c, fmt.Errorf("%s: %w", setting.name, err)
			}
		}
	}
	for name, value := range overrides {
		if err := apply(&c, name, value); err != nil {
			return c, fmt.Errorf("--%s: %w", name, err)
		}
	}
	// Never derive trust from PUBLIC_URL, an arbitrary request Host or a bind IP.
	// A supplied list replaces these local defaults; it is never appended to them.
	if c.AllowedHosts == nil {
		_, port, _ := net.SplitHostPort(c.Listen)
		for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
			c.AllowedHosts = append(c.AllowedHosts, net.JoinHostPort(host, port))
		}
		if port == "80" {
			c.AllowedHosts = append(c.AllowedHosts, "localhost", "127.0.0.1", "[::1]")
		}
	}
	var err error
	c.EnvironmentPublicURL, err = PublicURL(os.Getenv("REDAPP_PUBLIC_URL"))
	if err != nil {
		return c, fmt.Errorf("invalid REDAPP_PUBLIC_URL: %w", err)
	}
	return c, nil
}

func apply(c *Deployment, name, value string) error {
	switch name {
	case "data":
		c.DataDir = value
	case "listen":
		c.Listen = value
	case "allowed-hosts", "trusted-proxies":
		list := []string{}
		if value != "" {
			for _, item := range strings.Split(value, ",") {
				list = append(list, strings.TrimSpace(item))
			}
		}
		if name == "allowed-hosts" {
			c.AllowedHosts = list
		} else {
			c.TrustedProxies = list
		}
	case "max-active-writers", "max-readers", "max-artifact-bytes":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 1 || n > 1<<40 {
			return errors.New("expected a positive decimal integer within the documented limit")
		}
		switch name {
		case "max-active-writers":
			if n > 1024 {
				return errors.New("max_active_writers must be at most 1024")
			}
			c.DownloadLimits.MaxActiveWriters = int(n)
		case "max-readers":
			if n > 65536 {
				return errors.New("max_readers must be at most 65536")
			}
			c.DownloadLimits.MaxReaders = int(n)
		default:
			c.DownloadLimits.MaxArtifactBytes = n
		}
	default:
		return errors.New("unknown deployment option")
	}
	return validate(c)
}

func validate(c *Deployment) error {
	if c.SchemaVersion != 1 {
		return errors.New("unsupported deployment schema_version; expected 1")
	}
	if !filepath.IsAbs(c.DataDir) || strings.ContainsRune(c.DataDir, 0) {
		return errors.New("data_dir must be an absolute path")
	}
	host, port, err := net.SplitHostPort(c.Listen)
	n, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || n < 1 || n > 65535 || host != "" && host != "localhost" && net.ParseIP(host) == nil {
		return errors.New("listen must contain an IP address (or localhost) and a port from 1 to 65535")
	}
	if c.AllowedHosts != nil && (len(c.AllowedHosts) == 0 || len(c.AllowedHosts) > 128) {
		return errors.New("allowed_hosts requires 1 to 128 exact host authorities")
	}
	seen := make(map[string]bool)
	for i, host := range c.AllowedHosts {
		host = strings.ToLower(host)
		if !ValidHost(host) || seen[host] {
			return errors.New("allowed_hosts contains an invalid or duplicate authority")
		}
		c.AllowedHosts[i], seen[host] = host, true
	}
	if len(c.TrustedProxies) > 128 {
		return errors.New("trusted_proxies supports at most 128 CIDRs")
	}
	for _, cidr := range c.TrustedProxies {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return errors.New("trusted_proxies must contain valid CIDRs")
		}
	}
	l := c.DownloadLimits
	if l.MaxActiveWriters < 1 || l.MaxActiveWriters > 1024 || l.MaxReaders < 1 || l.MaxReaders > 65536 || l.MaxArtifactBytes < 1 || l.MaxArtifactBytes > 1<<40 {
		return errors.New("download_limits out of range: writers 1..1024, readers 1..65536, artifact bytes 1..1099511627776")
	}
	return nil
}
