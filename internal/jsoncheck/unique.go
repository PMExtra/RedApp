package jsoncheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Unique rejects duplicate keys at every object depth; encoding/json otherwise
// silently accepts the last value. Keys are compared after the same case folding
// encoding/json uses to match struct fields, so "name" and "NAME" cannot both
// reach one field.
func Unique(body []byte) error { return check(body, 32, false) }

// Strict is the request-body check of the HTTP contract: Unique plus invalid
// UTF-8 (which encoding/json would silently replace), nesting deeper than 64
// and every JSON null. Bodies whose schema accepts null must use Unique.
func Strict(body []byte) error {
	if !utf8.Valid(body) {
		return errors.New("JSON is not valid UTF-8")
	}
	return check(body, 64, true)
}

func check(body []byte, maxDepth int, rejectNull bool) error {
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > maxDepth {
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
		case nil:
			if rejectNull {
				return errors.New("JSON null is not allowed")
			}
			return nil
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
