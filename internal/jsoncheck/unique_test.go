package jsoncheck

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUniqueRejectsKeysThatEncodingJSONWouldMerge(t *testing.T) {
	for name, body := range map[string]string{
		"exact":         `{"name":1,"name":2}`,
		"ascii case":    `{"name":1,"NAME":2}`,
		"nested":        `{"outer":{"Value":1,"vALUE":2}}`,
		"in array":      `[{"a":1},{"b":1,"B":2}]`,
		"kelvin sign":   "{\"k\":1,\"K\":2}",
		"long s":        "{\"s\":1,\"ſ\":2}",
		"trailing data": `{"a":1} {}`,
		"too deep":      strings.Repeat("[", 34) + strings.Repeat("]", 34),
	} {
		t.Run(name, func(t *testing.T) {
			if err := Unique([]byte(body)); err == nil {
				t.Fatalf("Unique(%s) accepted the document", body)
			}
		})
	}
}

func TestUniqueAcceptsDistinctKeys(t *testing.T) {
	for _, body := range []string{`{"a":1,"b":{"a":2}}`, `[{"a":1},{"a":2}]`, `{"name":"x","names":"y"}`, `"text"`} {
		if err := Unique([]byte(body)); err != nil {
			t.Fatalf("Unique(%s) = %v", body, err)
		}
	}
}

// The folding rule must agree with how encoding/json assigns keys to struct fields.
func TestUniqueFoldingMatchesStructFieldMatching(t *testing.T) {
	for _, tc := range []struct{ key, field string }{{"K", "k"}, {"K", "k"}, {"ſ", "s"}, {"NaMe", "name"}} {
		var target map[string]string
		var fields struct {
			K    string `json:"k"`
			S    string `json:"s"`
			Name string `json:"name"`
		}
		body := []byte(`{"` + tc.key + `":"set"}`)
		if err := json.Unmarshal(body, &fields); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(fields)
		if err := json.Unmarshal(raw, &target); err != nil {
			t.Fatal(err)
		}
		if target[tc.field] != "set" {
			t.Fatalf("encoding/json did not match key %q to field %q: %v", tc.key, tc.field, target)
		}
		if foldKey(tc.key) != foldKey(tc.field) {
			t.Fatalf("foldKey(%q)=%q differs from foldKey(%q)=%q", tc.key, foldKey(tc.key), tc.field, foldKey(tc.field))
		}
	}
}
