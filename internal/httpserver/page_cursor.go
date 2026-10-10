package httpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/PMExtra/RedApp/internal/jsoncheck"
)

// pageCursor is the opaque continuation token of a cursor-paginated list. It
// is bound to the operation and a scope digest (application, source epoch,
// preview, filters), so a token used elsewhere is rejected with INVALID_CURSOR.
// The digest keeps internal identities such as UIDs out of the token.
type pageCursor struct {
	Operation string `json:"op"`
	Scope     string `json:"scope"`
	Last      []byte `json:"last"`
	Prefix    bool   `json:"prefix,omitempty"`
	Skip      int    `json:"skip,omitempty"`
}

// maxCursorLength is the specification's limit for cursors (NextCursor).
const maxCursorLength = 2048

// cursorScope digests the values a cursor is bound to.
func cursorScope(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// encodePageCursor returns the token for c.
func encodePageCursor(c pageCursor) *string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(c); err != nil {
		panic(err) // A struct of strings, bytes and integers always encodes.
	}
	token := base64.RawURLEncoding.EncodeToString(bytes.TrimSuffix(buffer.Bytes(), []byte("\n")))
	return &token
}

// decodePageCursor reads the cursor query parameter for operation and scope.
// It returns nil without a cursor.
func decodePageCursor(r *http.Request, operation, scope string) (*pageCursor, *apiError) {
	token := r.URL.Query().Get("cursor")
	if token == "" {
		return nil, nil
	}
	invalid := newError(codeInvalidCursor, nil, "cursor is not a next_cursor of this list; start again without a cursor")
	if len(token) > maxCursorLength {
		return nil, invalid
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || jsoncheck.Strict(raw) != nil {
		return nil, invalid
	}
	var c pageCursor
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.More() || c.Operation != operation || c.Scope != scope || len(c.Last) == 0 || c.Skip < 0 || (c.Skip > 0) != c.Prefix {
		return nil, invalid
	}
	return &c, nil
}
