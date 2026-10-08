package yamlconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go.yaml.in/yaml/v3"
	"io"
	"reflect"
	"strings"
)

// Decode retains JSON's strict field types and adds YAML field/line diagnostics.
func Decode(raw []byte, filename string, limit int, out any) error {
	data, err := JSON(raw, limit, filename)
	if err != nil {
		return fmt.Errorf("%s: %w", filename, err)
	}
	var node yaml.Node
	if err = yaml.Unmarshal(raw, &node); err != nil {
		return err
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s:1: expected an entity object", filename)
	}
	if err = fields(node.Content[0], reflect.TypeOf(out).Elem(), filename, ""); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(out); err != nil {
		return fmt.Errorf("%s: %w", filename, err)
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%s: trailing document", filename)
	}
	return nil
}

func fields(n *yaml.Node, t reflect.Type, file, field string) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	valid := true
	switch t.Kind() {
	case reflect.String:
		valid = n.Kind == yaml.ScalarNode && n.Tag == "!!str"
	case reflect.Int, reflect.Int64:
		valid = n.Kind == yaml.ScalarNode && n.Tag == "!!int"
	case reflect.Bool:
		valid = n.Kind == yaml.ScalarNode && n.Tag == "!!bool"
	case reflect.Struct:
		valid = n.Kind == yaml.MappingNode
	case reflect.Slice:
		valid = n.Kind == yaml.SequenceNode
	}
	if !valid {
		return fmt.Errorf("%s:%d: invalid type for field %s", file, n.Line, field)
	}
	if t.Kind() == reflect.Struct && n.Kind == yaml.MappingNode {
		known := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			key := strings.Split(f.Tag.Get("json"), ",")[0]
			if key != "" && key != "-" {
				known[key] = f.Type
			}
		}
		for i := 0; i < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			next := k.Value
			if field != "" {
				next = field + "." + next
			}
			ft, ok := known[k.Value]
			if !ok {
				return fmt.Errorf("%s:%d: unknown field %s", file, k.Line, next)
			}
			if err := fields(v, ft, file, next); err != nil {
				return err
			}
		}
	} else if t.Kind() == reflect.Slice && n.Kind == yaml.SequenceNode {
		for i, v := range n.Content {
			if err := fields(v, t.Elem(), file, fmt.Sprintf("%s[%d]", field, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// ErrorAt attaches the containing typed field and its source line to validation errors.
func ErrorAt(raw []byte, filename, field string, err error) error {
	var document yaml.Node
	line := 1
	if yaml.Unmarshal(raw, &document) == nil && len(document.Content) == 1 {
		n := document.Content[0]
		for _, part := range strings.Split(field, ".") {
			for i := 0; n.Kind == yaml.MappingNode && i < len(n.Content); i += 2 {
				if n.Content[i].Value == part {
					n = n.Content[i+1]
					line = n.Line
					break
				}
			}
		}
	}
	return fmt.Errorf("%s:%d: field %s: %w", filename, line, field, err)
}
