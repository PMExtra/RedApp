package yamlconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go.yaml.in/yaml/v3"
	"io"
	"strings"
)

// Convert a single, bounded YAML document into the same strictly typed JSON
// schema. Do not let YAML coerce numbers into string fields or merge aliases.
func JSON(raw []byte, limit int, filename string) ([]byte, error) {
	if len(raw) > limit {
		return nil, fmt.Errorf("%s: input exceeds %d bytes", filename, limit)
	}
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

type nodeError struct {
	err   error
	line  int
	field string
}

func (e nodeError) Error() string                   { return fmt.Sprintf("line %d field %s: %v", e.line, e.field, e.err) }
func validateYAML(node *yaml.Node, depth int) error { return validateYAMLPath(node, depth, "$") }
func validateYAMLPath(node *yaml.Node, depth int, field string) (err error) {
	defer func() {
		if err != nil {
			if _, ok := err.(nodeError); !ok {
				err = nodeError{err, node.Line, field}
			}
		}
	}()
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
	for i, child := range node.Content {
		next := field
		if node.Kind == yaml.MappingNode {
			next = field + "." + node.Content[i-i%2].Value
		} else if node.Kind == yaml.SequenceNode {
			next = fmt.Sprintf("%s[%d]", field, i)
		}
		if strings.HasPrefix(next, "$.") {
			next = strings.TrimPrefix(next, "$.")
		}
		if err := validateYAMLPath(child, depth+1, next); err != nil {
			return err
		}
	}
	return nil
}
