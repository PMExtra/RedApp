package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/PMExtra/RedApp/internal/jsoncheck"
	"go.yaml.in/yaml/v3"
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
	if !strings.EqualFold(filepath.Ext(path), ".json") {
		raw, err = yamlJSON(raw)
		if err != nil {
			return fmt.Errorf("invalid YAML: %w", err)
		}
	}
	if err := jsoncheck.Unique(raw); err != nil {
		return fmt.Errorf("invalid deployment JSON: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return errors.New("deployment configuration must be an object")
	}
	allowed := map[string]bool{"schema_version": true, "listen": true, "data_dir": true, "allowed_hosts": true, "trusted_proxies": true, "download_limits": true}
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
			if key != "max_active_writers" && key != "max_readers" && key != "max_artifact_bytes" || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
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

// Convert a single, bounded YAML document into the same strictly typed JSON
// schema. Do not let YAML coerce numbers into string fields or merge aliases.
func yamlJSON(raw []byte) ([]byte, error) {
	d := yaml.NewDecoder(bytes.NewReader(raw))
	var document yaml.Node
	if err := d.Decode(&document); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := d.Decode(&extra); err != io.EOF {
		return nil, errors.New("expected exactly one YAML document")
	}
	if err := validateYAML(&document, 0); err != nil {
		return nil, err
	}
	var value any
	if err := document.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func validateYAML(node *yaml.Node, depth int) error {
	if depth > 32 {
		return errors.New("configuration nesting exceeds 32 levels")
	}
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return errors.New("YAML anchors and aliases are not supported")
	}
	if node.Kind == yaml.MappingNode {
		if node.Tag != "!!map" {
			return errors.New("custom YAML tags are not supported")
		}
		seen := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return errors.New("YAML mapping keys must be strings; merge keys are not supported")
			}
			if seen[key.Value] {
				return fmt.Errorf("duplicate YAML field %q at line %d", key.Value, key.Line)
			}
			seen[key.Value] = true
		}
	}
	if node.Kind == yaml.SequenceNode && node.Tag != "!!seq" {
		return errors.New("custom YAML tags are not supported")
	}
	if node.Kind == yaml.ScalarNode {
		switch node.Tag {
		case "!!str", "!!int", "!!bool":
		default:
			return fmt.Errorf("unsupported YAML value or null at line %d", node.Line)
		}
	}
	for _, child := range node.Content {
		if err := validateYAML(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}
