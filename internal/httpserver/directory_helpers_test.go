package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/url"
	"strings"
	"testing"
)

// Test helpers for the directory, configuration, notes, category and
// exchange administrator API.

// ifMatch returns the If-Match header for revision.
func ifMatchHeader(revision int64) map[string]string {
	return map[string]string{"If-Match": etag(revision)}
}

// getJSON reads a 200 JSON document into T.
func getJSON[T any](h *harness, path string) T {
	h.t.Helper()
	data, _ := h.request("GET", path, nil, 200, nil)
	return decodeJSONBody[T](h.t, data)
}

// adminVendor reads the vendor document.
func (h *harness) adminVendor(id string) vendorDTO {
	h.t.Helper()
	return getJSON[vendorDTO](h, "/admin/api/vendors/"+id)
}

// adminApp reads the application document.
func (h *harness) adminApp(key string) appDTO {
	h.t.Helper()
	return getJSON[appDTO](h, "/admin/api/apps/"+key)
}

// setEnabled changes the lifecycle state of a vendor ("vendors/<id>") or an
// application ("apps/<key>") through the API.
func (h *harness) setEnabled(path string, enabled bool) {
	h.t.Helper()
	_, headers := h.request("GET", "/admin/api/"+path, nil, 200, nil)
	h.request("PATCH", "/admin/api/"+path, map[string]any{"enabled": enabled}, 200, map[string]string{"If-Match": headers.Get("ETag")})
}

// deleteApp permanently deletes an application and requires status.
func (h *harness) deleteApp(key, uid string, revision int64, status int) []byte {
	h.t.Helper()
	data, _ := h.request("DELETE", "/admin/api/apps/"+key+"?confirm_uid="+url.QueryEscape(uid), nil, status, ifMatchHeader(revision))
	return data
}

// configuration reads a configuration document ("vendors/<id>" or "apps/<key>").
func (h *harness) configuration(path string) configurationDTO {
	h.t.Helper()
	return getJSON[configurationDTO](h, "/admin/api/"+path+"/configuration")
}

// patchConfiguration patches the configuration at its current revision.
func (h *harness) patchConfiguration(path string, body map[string]any, status int) []byte {
	h.t.Helper()
	current := h.configuration(path)
	data, _ := h.request("PATCH", "/admin/api/"+path+"/configuration", body, status, ifMatchHeader(current.Revision))
	return data
}

// multipartForm builds a multipart/form-data body from (name, filename, content) parts.
func multipartForm(t *testing.T, parts ...[3]string) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, p := range parts {
		var w io.Writer
		var err error
		if p[1] != "" {
			w, err = form.CreateFormFile(p[0], p[1])
		} else {
			w, err = form.CreateFormField(p[0])
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(w, p[2]); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, form.FormDataContentType()
}

// uploadIcon posts an icon file and requires status; it returns the icon path on 201.
func (h *harness) uploadIcon(content string, status int) string {
	h.t.Helper()
	body, contentType := multipartForm(h.t, [3]string{"file", "untrusted-name.svg", content})
	code, data, _ := h.raw("POST", "/admin/api/icons", body, contentType, nil)
	if code != status {
		h.t.Fatalf("icon upload got %d, want %d: %s", code, status, data)
	}
	if code != 201 {
		return ""
	}
	return decodeJSONBody[storedIconDTO](h.t, data).Icon
}

// svgIcon is a static SVG of one color.
func svgIcon(color string) string {
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><rect width="24" height="24" fill="` + color + `"/></svg>`
}

// exchangeUpload posts a configuration package with optional choices and
// requires status.
func exchangeUpload(h *harness, raw []byte, choices any, status int) []byte {
	h.t.Helper()
	parts := [][3]string{{"file", "identity-ignored.zip", string(raw)}}
	if choices != nil {
		encoded, err := json.Marshal(choices)
		if err != nil {
			h.t.Fatal(err)
		}
		parts = append(parts, [3]string{"choices", "", string(encoded)})
	}
	body, contentType := multipartForm(h.t, parts...)
	code, data, _ := h.raw("POST", "/admin/api/configuration/import/preview", body, contentType, nil)
	if code != status {
		h.t.Fatalf("preview status %d, want %d: %s", code, status, data)
	}
	return data
}

// exportPackage exports a selection ("vendor:<id>" or "app:<key>") and returns the ZIP.
func (h *harness) exportPackage(mode string, credentials bool, selection ...string) []byte {
	h.t.Helper()
	items := []map[string]any{}
	for _, s := range selection {
		kind, key, _ := strings.Cut(s, ":")
		items = append(items, map[string]any{"kind": kind, "key": key})
	}
	data, headers := h.request("POST", "/admin/api/configuration/export", map[string]any{"selection": items, "mode": mode, "include_notes": false, "include_proxy_credentials": credentials}, 200, nil)
	if headers.Get("Content-Type") != "application/zip" {
		h.t.Fatal("export is not a ZIP", headers)
	}
	return data
}

// executeImport executes a preview and requires status.
func (h *harness) executeImport(id string, trust bool, status int) []byte {
	h.t.Helper()
	data, _ := h.request("POST", "/admin/api/configuration/import/"+id+"/execute", map[string]any{"trust_instructions": trust}, status, nil)
	return data
}

// expectCode requires an Error document with code.
func expectCode(t *testing.T, data []byte, code errorCode) {
	t.Helper()
	if got := errorCodeOf(t, data); got != string(code) {
		t.Fatalf("error code %s, want %s: %s", got, code, data)
	}
}
