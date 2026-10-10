package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/PMExtra/RedApp/internal/jsoncheck"
	"github.com/PMExtra/RedApp/internal/yamlconfig"
)

func readDeployment(path string, optional bool, c *Deployment) error {
	f, err := os.Open(path)
	if err != nil {
		if optional && os.IsNotExist(err) {
			// A dangling symlink is a broken configuration, not an absent file.
			if _, statErr := os.Lstat(path); os.IsNotExist(statErr) {
				return nil
			}
		}
		return fmt.Errorf("read configuration: %w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return errors.New("configuration must be readable and at most 64 KiB")
	}
	raw, err = yamlconfig.JSON(raw, 64<<10, "deployment")
	if err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	if err := jsoncheck.Unique(raw); err != nil {
		return fmt.Errorf("invalid deployment JSON: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return errors.New("deployment configuration must be an object")
	}
	allowed := map[string]bool{"schema_version": true, "listen": true, "data_dir": true, "trusted_proxies": true, "download_limits": true}
	for key, value := range fields {
		if !allowed[key] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("unknown or null deployment field %q", key)
		}
	}
	if rawLimits, ok := fields["download_limits"]; ok {
		var limits map[string]json.RawMessage
		if err := json.Unmarshal(rawLimits, &limits); err != nil || limits == nil {
			return errors.New("download_limits must be an object")
		}
		for key, value := range limits {
			if key != "max_writers" && key != "max_readers" && key != "max_artifact_bytes" && key != "max_downloads_per_client" || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("unknown or null download limit %q", key)
			}
		}
		// Only the artifact limit accepts a size string. Keep numeric values and
		// every other field under the existing strict JSON type validation.
		if value := bytes.TrimSpace(limits["max_artifact_bytes"]); len(value) > 0 && value[0] == '"' {
			var size string
			if err := json.Unmarshal(value, &size); err != nil {
				return err
			}
			n, err := artifactBytes(size)
			if err != nil {
				return err
			}
			limits["max_artifact_bytes"] = json.RawMessage(strconv.FormatInt(n, 10))
			fields["download_limits"], err = json.Marshal(limits)
			if err != nil {
				return err
			}
			raw, err = json.Marshal(fields)
			if err != nil {
				return err
			}
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(c); err != nil {
		return fmt.Errorf("invalid deployment field type: %w", err)
	}
	return nil
}
