package httpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var transferSequence atomic.Int64

func nextTransferID() string { return fmt.Sprintf("%032x", transferSequence.Add(1)) }

// upload sends path, optional expected_id and the file, in the specified order.
func (h *harness) upload(key, path, expectedID, content string, status int) hostedFileDTO {
	h.t.Helper()
	parts := [][3]string{{"path", "", path}}
	if expectedID != "" {
		parts = append(parts, [3]string{"expected_id", "", expectedID})
	}
	parts = append(parts, [3]string{"file", "upload.bin", content})
	body, contentType := multipartForm(h.t, parts...)
	code, data, headers := h.raw("POST", "/admin/api/apps/"+key+"/files?transfer_id="+nextTransferID(), body, contentType, nil)
	if code != status {
		h.t.Fatalf("upload %s: got %d, want %d: %s", path, code, status, data)
	}
	if status != 201 {
		return hostedFileDTO{}
	}
	if headers.Get("ETag") != "" {
		h.t.Fatal("hosted files have no revision", headers)
	}
	return decodeJSONBody[hostedFileDTO](h.t, data)
}

func TestHostedUploadImportReplaceAndRestart(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, withDir(dir))
	h.login(h.password)
	h.createVendor("content")
	a := h.createApp("content", "files", "hosted", nil)
	h.createApp("content", "other", "hosted", nil)
	first := h.upload(a.Key, "nested/tool.bin", "", "abcdef", 201)
	if first.Path != "nested/tool.bin" || first.SizeBytes != 6 || len(first.SHA256) != 64 {
		t.Fatal(first)
	}
	data, headers := h.request("GET", "/content/files/nested/tool.bin", nil, 206, map[string]string{"Range": "bytes=1-3"})
	if string(data) != "bcd" || headers.Get("Content-Range") != "bytes 1-3/6" || headers.Get("ETag") == "" {
		t.Fatal(string(data), headers)
	}
	h.request("GET", "/content/files/nested/tool.bin", nil, 304, map[string]string{"If-None-Match": headers.Get("ETag")})
	h.request("HEAD", "/content/files/nested/tool.bin", nil, 200, nil)
	h.upload(a.Key, "nested/tool.bin", "", "oops", 409)
	replacement := h.upload(a.Key, "nested/tool.bin", first.ID, "replacement", 201)
	if replacement.ID == first.ID {
		t.Fatal("a replacement must get a new ID")
	}
	h.upload(a.Key, "nested/tool.bin", first.ID, "stale", 409)
	h.expectError("DELETE", "/admin/api/apps/content/other/files/"+replacement.ID, nil, 404, codeFileNotFound, nil)
	h.expectError("DELETE", "/admin/api/apps/"+a.Key+"/files/"+first.ID, nil, 404, codeFileNotFound, nil)
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.RawQuery != "token=private-signature" {
			t.Error("signed query not forwarded")
		}
		w.Write([]byte("imported"))
	}))
	defer upstream.Close()
	imported, _ := h.request("POST", "/admin/api/apps/content/files/files/import?transfer_id="+nextTransferID(), map[string]any{"path": "remote.zip", "url": upstream.URL + "/asset?token=private-signature"}, 201, nil)
	if bytes.Contains(imported, []byte("private-signature")) {
		t.Fatal("import response leaked URL")
	}
	for range 2 {
		if data, _ = h.request("GET", "/content/files/remote.zip", nil, 200, nil); string(data) != "imported" {
			t.Fatal(string(data))
		}
	}
	if requests.Load() != 1 {
		t.Fatal("public download contacted origin", requests.Load())
	}
	data, _ = h.request("GET", "/admin/api/apps/"+a.Key+"/files?page=1&limit=1", nil, 200, nil)
	page := decodeJSONBody[pageDTO[hostedFileDTO]](t, data)
	if page.Total != 2 || page.TotalPages != 2 || len(page.Items) != 1 || page.Items[0].ID != replacement.ID {
		t.Fatal("files are listed by path", string(data))
	}
	data, _ = h.request("GET", "/admin/api/apps/"+a.Key+"/files?page=99", nil, 200, nil)
	if page = decodeJSONBody[pageDTO[hostedFileDTO]](t, data); page.Page != 99 || page.Total != 2 || len(page.Items) != 0 {
		t.Fatal(string(data))
	}
	password := h.password
	h.close()
	h = newHarness(t, withDir(dir))
	h.login(password)
	if data, _ = h.request("GET", "/content/files/nested/tool.bin", nil, 200, nil); string(data) != "replacement" {
		t.Fatal("restart lost file", string(data))
	}
	h.request("DELETE", "/admin/api/apps/"+a.Key+"/files/"+replacement.ID, nil, 204, nil)
	h.request("GET", "/content/files/nested/tool.bin", nil, 404, nil)
	h.expectError("DELETE", "/admin/api/apps/"+a.Key+"/files/"+replacement.ID, nil, 404, codeFileNotFound, nil)
	// Deleted applications stay readable and their files can be deleted.
	h.markDeleted(a.Key)
	data, _ = h.request("GET", "/admin/api/apps/"+a.Key+"/files", nil, 200, nil)
	if page = decodeJSONBody[pageDTO[hostedFileDTO]](t, data); len(page.Items) != 1 {
		t.Fatal(string(data))
	}
	h.upload(a.Key, "late.bin", "", "late", 409)
	h.expectError("POST", "/admin/api/apps/"+a.Key+"/files/import?transfer_id="+nextTransferID(), map[string]any{"path": "late.zip", "url": upstream.URL}, 409, codeEntityDeleted, nil)
	h.request("DELETE", "/admin/api/apps/"+a.Key+"/files/"+page.Items[0].ID, nil, 204, nil)
}

func TestHostedUploadValidatesFieldsOrderAndLimits(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	h.createVendor("content")
	a := h.createApp("content", "files", "hosted", nil)
	if err := h.server.downloads.ConfigureLimits(16, 512, 16); err != nil {
		t.Fatal(err)
	}
	endpoint := "/admin/api/apps/" + a.Key + "/files?transfer_id=" + strings.Repeat("a", 32)
	post := func(code errorCode, parts ...[3]string) {
		t.Helper()
		body, contentType := multipartForm(t, parts...)
		status, data, _ := h.raw("POST", endpoint, body, contentType, nil)
		if status != errorCatalog[code].status || errorCodeOf(t, data) != string(code) {
			t.Fatalf("%v: got %d %s, want %s", parts, status, data, code)
		}
	}
	file := [3]string{"file", "a.bin", "x"}
	post(codeInvalidRequest, file, [3]string{"path", "", "a.bin"})
	post(codeInvalidRequest, [3]string{"expected_id", "", ""}, [3]string{"path", "", "a.bin"}, file)
	post(codeInvalidRequest, [3]string{"path", "", "a.bin"})
	post(codeInvalidRequest, [3]string{"path", "", "a.bin"}, [3]string{"comment", "", "x"}, file)
	post(codeInvalidRequest, [3]string{"path", "", "a.bin"}, [3]string{"file", "", "no file name"})
	post(codeValidationFailed, [3]string{"path", "", strings.Repeat("a", 4097)}, file)
	for _, path := range []string{"../escape", "/absolute", "a//b", "dir/"} {
		post(codeValidationFailed, [3]string{"path", "", path}, file)
	}
	post(codeValidationFailed, [3]string{"path", "", "a.bin"}, [3]string{"expected_id", "", "not-an-id"}, file)
	post(codeArtifactTooLarge, [3]string{"path", "", "a.bin"}, [3]string{"file", "a.bin", strings.Repeat("x", 17)})
	h.expectError("POST", "/admin/api/apps/"+a.Key+"/files?transfer_id="+strings.Repeat("a", 32), map[string]string{"path": "a.bin"}, 415, codeUnsupportedMediaType, nil)
	body, contentType := multipartForm(t, [3]string{"path", "", "a.bin"}, file)
	status, data, _ := h.raw("POST", "/admin/api/apps/"+a.Key+"/files", body, contentType, nil)
	if status != 400 || errorCodeOf(t, data) != string(codeInvalidQuery) {
		t.Fatal("upload without transfer_id", status, string(data))
	}
	for _, input := range []map[string]any{
		{"path": "a.zip", "url": "https://user:secret@example.com/a.zip"},
		{"path": "a.zip", "url": "ftp://example.com/a.zip"},
		{"path": "a.zip", "url": "https://example.com/a.zip#fragment"},
		{"path": "../a.zip", "url": "https://example.com/a.zip"},
	} {
		data, _ := h.request("POST", "/admin/api/apps/"+a.Key+"/files/import?transfer_id="+nextTransferID(), input, 400, nil)
		if errorCodeOf(t, data) != string(codeValidationFailed) || bytes.Contains(data, []byte("secret")) {
			t.Fatal(string(data))
		}
	}
	h.expectError("POST", "/admin/api/apps/"+a.Key+"/files/import?transfer_id="+nextTransferID(), map[string]any{"path": "a.zip", "url": "http://127.0.0.1:9/missing"}, 502, codeImportSourceFailed, nil)
	for provider, options := range map[string]map[string]any{"info": nil, "http-cache": {"base_url": "https://example.com"}} {
		other := h.createApp("content", strings.ReplaceAll(provider, "-", ""), provider, options)
		h.expectError("GET", "/admin/api/apps/"+other.Key+"/files", nil, 404, codeCapabilityUnsupported, nil)
		h.expectError("DELETE", "/admin/api/apps/"+other.Key+"/files/"+strings.Repeat("a", 32), nil, 404, codeCapabilityUnsupported, nil)
	}
	h.expectError("GET", "/admin/api/apps/content/missing/files", nil, 404, codeApplicationNotFound, nil)
	h.expectError("DELETE", "/admin/api/apps/"+a.Key+"/files/not-an-id", nil, 400, codeInvalidPath, nil)
	h.expectError("GET", "/admin/api/apps/"+a.Key+"/files?limit=101", nil, 400, codeInvalidQuery, nil)
}

func TestHostedStreamingUploadExtendsOnlyAuthenticatedBodyDeadline(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	h.createVendor("stream")
	h.createApp("stream", "files", "hosted", nil)
	slow := httptest.NewUnstartedServer(h.server)
	slow.Config.ReadTimeout = 25 * time.Millisecond
	slow.Start()
	defer slow.Close()
	other := *h
	other.http = slow
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	done := make(chan error, 1)
	go func() {
		defer writer.Close()
		err := multipartWriter.WriteField("path", "slow.bin")
		if err != nil {
			done <- err
			return
		}
		part, err := multipartWriter.CreateFormFile("file", "slow.bin")
		if err != nil {
			done <- err
			return
		}
		time.Sleep(100 * time.Millisecond) // longer than the server read timeout
		if _, err = part.Write([]byte("complete")); err == nil {
			err = multipartWriter.Close()
		}
		done <- err
	}()
	code, data, _ := other.raw("POST", "/admin/api/apps/stream/files/files?transfer_id=11111111111111111111111111111111", reader, multipartWriter.FormDataContentType(), nil)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if code != 201 {
		t.Fatal(code, string(data))
	}
	other.request("GET", "/stream/files/slow.bin", nil, 200, nil)
}

func TestHostedTransferProgressCancelAndBusyID(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	h.createVendor("cancel")
	app := h.createApp("cancel", "files", "hosted", nil)
	info := h.createApp("cancel", "info", "info", nil)
	started, closed := make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(200)
		w.Write([]byte("temporary"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(closed)
	}))
	defer upstream.Close()
	transfer := strings.Repeat("a", 32)
	transferPath := "/admin/api/apps/" + app.Key + "/files/transfers/" + transfer
	input := map[string]any{"path": "cancel.bin", "url": upstream.URL}
	h.expectError("POST", "/admin/api/apps/"+info.Key+"/files/import?transfer_id="+transfer, input, 404, codeCapabilityUnsupported, nil)
	h.expectError("GET", "/admin/api/apps/"+info.Key+"/files/transfers/"+transfer, nil, 404, codeCapabilityUnsupported, nil)
	if count.Load() != 0 {
		t.Fatal("capability bypass contacted an upstream")
	}
	h.expectError("GET", transferPath, nil, 404, codeTransferNotFound, nil)
	h.expectError("DELETE", transferPath, nil, 404, codeTransferNotFound, nil)
	bodyBytes, _ := json.Marshal(input)
	request, err := http.NewRequest("POST", h.http.URL+"/admin/api/apps/"+app.Key+"/files/import?transfer_id="+transfer, bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", h.csrf)
	type result struct {
		status int
		body   []byte
		err    error
	}
	finished := make(chan result, 1)
	go func() {
		r, e := h.client.Do(request)
		if e != nil {
			finished <- result{err: e}
			return
		}
		defer r.Body.Close()
		data, e := io.ReadAll(r.Body)
		finished <- result{r.StatusCode, data, e}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("import did not start")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, _ := h.request("GET", transferPath, nil, 200, nil)
		progress := decodeJSONBody[hostedTransferDTO](t, data)
		if progress.ID != transfer || progress.Path != "cancel.bin" || progress.State != "receiving" || progress.TotalBytes == nil || *progress.TotalBytes != 100 {
			t.Fatal("transfer progress", string(data))
		}
		if progress.Bytes == int64(len("temporary")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("progress did not advance", string(data))
		}
		time.Sleep(10 * time.Millisecond)
	}
	body, contentType := multipartForm(t, [3]string{"path", "", "other.bin"}, [3]string{"file", "o.bin", "o"})
	if status, data, _ := h.raw("POST", "/admin/api/apps/"+app.Key+"/files?transfer_id="+transfer, body, contentType, nil); status != 409 || errorCodeOf(t, data) != string(codeTransferIdInUse) {
		t.Fatal("running transfer ID reused", status, string(data))
	}
	h.request("DELETE", transferPath, nil, 204, nil)
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel left upstream alive")
	}
	select {
	case result := <-finished:
		if result.err != nil || result.status != 409 || errorCodeOf(t, result.body) != string(codeTransferCancelled) {
			t.Fatal(result.status, string(result.body), result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel left import running")
	}
	h.expectError("GET", transferPath, nil, 404, codeTransferNotFound, nil)
	h.request("GET", "/cancel/files/cancel.bin", nil, 404, nil)
	files, e := h.server.store.HostedPage(app.UID, 1, 25)
	if e != nil || files.Total != 0 {
		t.Fatal("cancel published a resource", files, e)
	}
	objects, e := os.ReadDir(filepath.Join(h.server.dataDir, "objects", "hosted"))
	if e != nil || len(objects) != 0 {
		t.Fatal("cancel left temporary objects", objects, e)
	}
	if count.Load() != 1 {
		t.Fatal("cancel retried import", count.Load())
	}
}
