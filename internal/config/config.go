// Package config owns deployment configuration and the public-origin setting.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/PMExtra/RedApp/internal/jsoncheck"
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

// Load validates configuration without touching the data directory. Only the
// explicitly supported PUBLIC_URL environment default is consulted.
func Load(path string) (Deployment, error) {
	var c Deployment
	if path == "" {
		return c, errors.New("--config requires an explicit JSON configuration file")
	}
	f, err := os.Open(path)
	if err != nil {
		return c, fmt.Errorf("read deployment configuration: %w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return c, errors.New("deployment configuration must be readable and at most 64 KiB")
	}
	if err := jsoncheck.Unique(raw); err != nil {
		return c, fmt.Errorf("invalid deployment JSON: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return c, errors.New("deployment configuration must be a JSON object")
	}
	allowed := map[string]bool{"schema_version": true, "listen": true, "data_dir": true, "allowed_hosts": true, "trusted_proxies": true, "download_limits": true}
	for key, value := range fields {
		if !allowed[key] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return c, fmt.Errorf("unknown or null deployment field %q", key)
		}
	}
	for _, key := range []string{"schema_version", "data_dir", "allowed_hosts"} {
		if _, ok := fields[key]; !ok {
			return c, fmt.Errorf("deployment field %q is required", key)
		}
	}
	if rawLimits, ok := fields["download_limits"]; ok {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawLimits, &fields); err != nil || fields == nil {
			return c, errors.New("download_limits must be an object")
		}
		for key, value := range fields {
			if key != "max_active_writers" && key != "max_readers" && key != "max_artifact_bytes" || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return c, fmt.Errorf("unknown or null download limit %q", key)
			}
		}
	}
	c.Listen = ":8080"
	c.DownloadLimits = DownloadLimits{MaxActiveWriters: 16, MaxReaders: 512, MaxArtifactBytes: 4 << 30}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, fmt.Errorf("invalid deployment configuration: %w", err)
	}
	if c.SchemaVersion != 1 {
		return c, errors.New("unsupported deployment schema_version; expected 1")
	}
	if !filepath.IsAbs(c.DataDir) || strings.ContainsRune(c.DataDir, 0) {
		return c, errors.New("data_dir must be an absolute path")
	}
	host, port, err := net.SplitHostPort(c.Listen)
	n, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || n < 1 || n > 65535 || host != "" && host != "localhost" && net.ParseIP(host) == nil {
		return c, errors.New("listen must contain an IP address (or localhost) and a port from 1 to 65535")
	}
	if len(c.AllowedHosts) == 0 || len(c.AllowedHosts) > 128 {
		return c, errors.New("allowed_hosts requires 1 to 128 exact host authorities")
	}
	seen := make(map[string]bool)
	for i, host := range c.AllowedHosts {
		host = strings.ToLower(host)
		if !ValidHost(host) || seen[host] {
			return c, errors.New("allowed_hosts contains an invalid or duplicate authority")
		}
		c.AllowedHosts[i], seen[host] = host, true
	}
	if len(c.TrustedProxies) > 128 {
		return c, errors.New("trusted_proxies supports at most 128 CIDRs")
	}
	for _, cidr := range c.TrustedProxies {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return c, errors.New("trusted_proxies must contain valid CIDRs")
		}
	}
	l := c.DownloadLimits
	if l.MaxActiveWriters < 1 || l.MaxActiveWriters > 1024 || l.MaxReaders < 1 || l.MaxReaders > 65536 || l.MaxArtifactBytes < 1 || l.MaxArtifactBytes > 1<<40 {
		return c, errors.New("download_limits out of range: writers 1..1024, readers 1..65536, artifact bytes 1..1099511627776")
	}
	c.EnvironmentPublicURL, err = PublicURL(os.Getenv("REDAPP_PUBLIC_URL"))
	if err != nil {
		return c, fmt.Errorf("invalid REDAPP_PUBLIC_URL: %w", err)
	}
	return c, nil
}
