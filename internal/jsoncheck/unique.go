package jsoncheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
)

// Unique rejects duplicate keys at every object depth; encoding/json otherwise
// silently accepts the last value. Keys are compared after the same case folding
// encoding/json uses to match struct fields, so "name" and "NAME" cannot both
// reach one field.
func Unique(body []byte) error {
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return errors.New("JSON nesting limit exceeded")
		}
		t, e := d.Token()
		if e != nil {
			return e
		}
		switch t {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return e
				}
				k, ok := key.(string)
				if !ok {
					return errors.New("Invalid JSON object key")
				}
				k = foldKey(k)
				if seen[k] {
					return errors.New("Duplicate JSON field")
				}
				seen[k] = true
				if e = value(depth + 1); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		case json.Delim('['):
			for d.More() {
				if e = value(depth + 1); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		default:
			return nil
		}
	}
	if e := value(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("Unexpected trailing JSON data")
	}
	return nil
}

// foldKey mirrors encoding/json's field-name folding: every rune maps to
// upper(lower(r)), which also equates special folds such as the Kelvin sign and K.
func foldKey(key string) string {
	return strings.Map(func(r rune) rune { return unicode.ToUpper(unicode.ToLower(r)) }, key)
}
