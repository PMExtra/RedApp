package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/jsoncheck"
)

// checkQuery enforces a route's query allow-list: every parameter must be
// declared, appear once and be non-empty.
func checkQuery(r *http.Request, allowed []string) *apiError {
	if r.URL.RawQuery == "" {
		return nil
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return newError(codeInvalidQuery, nil, "Malformed query string")
	}
	for key, list := range values {
		if !slices.Contains(allowed, key) {
			return newError(codeInvalidQuery, nil, "Unknown query parameter "+strconv.Quote(key))
		}
		if len(list) != 1 {
			return newError(codeInvalidQuery, nil, "Repeated query parameter "+strconv.Quote(key))
		}
		if list[0] == "" {
			return newError(codeInvalidQuery, nil, "Empty query parameter "+strconv.Quote(key))
		}
	}
	return nil
}

// queryInt reads an optional integer query parameter in [min, max].
func queryInt(r *http.Request, name string, fallback, min, max int) (int, *apiError) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max || strconv.Itoa(n) != raw {
		return 0, newError(codeInvalidQuery, nil, name+" must be an integer between "+strconv.Itoa(min)+" and "+strconv.Itoa(max))
	}
	return n, nil
}

// pageQuery reads the page-style pagination parameters.
func pageQuery(r *http.Request, defaultLimit int) (page, limit int, e *apiError) {
	if page, e = queryInt(r, "page", 1, 1, 1000000000); e != nil {
		return
	}
	limit, e = queryInt(r, "limit", defaultLimit, 1, 100)
	return
}

// queryText reads an optional free-text parameter: trimmed, valid UTF-8 and at
// most maxRunes characters.
func queryText(r *http.Request, name string, maxRunes int) (string, *apiError) {
	raw := r.URL.Query().Get(name)
	if !utf8.ValidString(raw) || utf8.RuneCountInString(raw) > maxRunes {
		return "", newError(codeInvalidQuery, nil, name+" must be valid text of at most "+strconv.Itoa(maxRunes)+" characters")
	}
	return strings.TrimSpace(raw), nil
}

// decodeJSON strictly decodes an application/json request body into v. The
// route's x-max-body-bytes limit is already applied to r.Body.
func decodeJSON(r *http.Request, v any) *apiError {
	return decodeJSONWith(r, v, jsoncheck.Strict)
}

// decodeJSONNullable is decodeJSON for request schemas that accept null.
func decodeJSONNullable(r *http.Request, v any) *apiError {
	return decodeJSONWith(r, v, func(b []byte) error {
		if !utf8.Valid(b) {
			return errors.New("JSON is not valid UTF-8")
		}
		return jsoncheck.Unique(b)
	})
}

func decodeJSONWith(r *http.Request, v any, check func([]byte) error) *apiError {
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) {
		return newError(codeUnsupportedMediaType, nil, "Content-Type must be application/json")
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return newError(codePayloadTooLarge, nil, "Request body exceeds "+strconv.FormatInt(tooLarge.Limit, 10)+" bytes")
		}
		return newError(codeInvalidRequest, err, "Request body could not be read")
	}
	if err = check(body); err != nil {
		return newError(codeInvalidRequest, err, "Request body is not valid strict JSON")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		return newError(codeInvalidRequest, err, "Request body does not match the expected schema")
	}
	if d.More() {
		return newError(codeInvalidRequest, nil, "Unexpected trailing request data")
	}
	return nil
}

var ifMatchPattern = regexp.MustCompile(`^"[1-9][0-9]{0,17}"$`)

// ifMatch parses the required If-Match revision ("<positive integer>"). Weak
// validators, lists and * are rejected.
func ifMatch(r *http.Request) (int64, *apiError) {
	values := r.Header.Values("If-Match")
	if len(values) != 1 || !ifMatchPattern.MatchString(values[0]) {
		return 0, newError(codeIfMatchRequired, nil, `If-Match must be the quoted revision from the last read, for example "7"`)
	}
	revision, err := strconv.ParseInt(strings.Trim(values[0], `"`), 10, 64)
	if err != nil {
		return 0, newError(codeIfMatchRequired, nil, "If-Match revision is out of range")
	}
	return revision, nil
}

// revisionConflict is the response when an If-Match revision or identity guard is stale.
func revisionConflict(cause error) *apiError {
	return newError(codeRevisionConflict, cause, "Changed by someone else; reload before saving")
}
