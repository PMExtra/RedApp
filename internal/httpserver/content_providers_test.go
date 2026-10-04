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

	"github.com/PMExtra/RedApp/internal/store"
)

func TestContentProvidersInstructionsAndBackendCapabilityGates(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	h.createVendor("content")
	info := h.createApp("content", "about", "info", nil)
	hosted := h.createApp("content", "files", "hosted", nil)
	for _, a := range []store.Application{info, hosted} {
		var count int
		if err := h.server.DB.DB.QueryRow(`SELECT COUNT(*) FROM application_sources WHERE app_uid=?`, a.UID).Scan(&count); err != nil || count != 0 {
			t.Fatal("content source created", count, err)
		}
		e, _ := h.server.Registry.Lookup(a.Key)
		if e.Upstream != nil || len(e.Upstreams) != 0 || e.Protocol != nil {
			t.Fatal("content app has upstream", e)
		}
		for _, endpoint := range []string{"status", "sources", "versions", "resources", "cache", "settings"} {
			h.request("GET", "/admin/api/apps/"+a.Key+"/"+endpoint, nil, 404, nil)
		}
		for _, endpoint := range []string{"cleanup/preview", "cache/refresh/preview", "channel/refresh"} {
			h.request("POST", "/admin/api/apps/"+a.Key+"/"+endpoint, map[string]any{}, 404, nil)
		}
	}
	h.request("GET", "/content/about/file.zip", nil, 404, nil)
	h.request("GET", "/admin/api/apps/content/about/files", nil, 404, nil)
	h.request("POST", "/admin/api/vendors/content/apps", map[string]any{"id": "bad", "provider": "info", "name": store.LocalizedText{En: "Bad", ZhCN: "错误"}, "base_url": "https://example.com"}, 400, nil)
	endpoint := "/admin/api/apps/" + info.Key + "/instructions"
	_, headers := h.request("GET", endpoint, nil, 200, nil)
	if headers.Get("ETag") != `"0"` {
		t.Fatal(headers)
	}
	before, _ := h.bootstrap()
	value := map[string]any{"en": "<script>never execute</script>\nUse this app.", "zh-CN": "使用说明\n保留换行"}
	h.request("PUT", endpoint, value, 403, map[string]string{"X-CSRF-Token": "wrong", "If-Match": `"0"`})
	_, headers = h.request("PUT", endpoint, value, 200, map[string]string{"If-Match": `"0"`})
	if headers.Get("ETag") != `"1"` {
		t.Fatal(headers)
	}
	h.request("PUT", endpoint, value, 409, map[string]string{"If-Match": `"0"`})
	after, _ := h.bootstrap()
	if before == after {
		t.Fatal("instructions did not invalidate public bootstrap")
	}
	data, _ := h.request("GET", "/api/bootstrap", nil, 200, nil)
	var boot struct {
		Apps []struct {
			ID           string              `json:"id"`
			Instructions store.LocalizedText `json:"instructions"`
		}
	}
	if err := json.Unmarshal(data, &boot); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range boot.Apps {
		if a.ID == info.Key {
			found = true
			if a.Instructions.En != value["en"] || a.Instructions.ZhCN != value["zh-CN"] {
				t.Fatal(a)
			}
		}
	}
	if !found {
		t.Fatal("public content missing")
	}
	h.request("PUT", endpoint, map[string]any{"en": strings.Repeat("界", 12001), "zh-CN": ""}, 400, map[string]string{"If-Match": `"1"`})
	if updated, _ := h.server.DB.Application(info.Key); updated.Revision != info.Revision || updated.SourceEpoch != info.SourceEpoch {
		t.Fatal("instructions changed application identity")
	}
}
func TestHostedHTTPUploadImportLocalRangeAndRestart(t *testing.T) {
	dir := t.TempDir()
	h := newDirectoryHarness(t, dir)
	h.login(h.password)
	h.createVendor("content")
	a := h.createApp("content", "files", "hosted", nil)
	h.createApp("content", "other", "hosted", nil)
	var sequence int
	upload := func(path, expected, value string, status int) store.HostedFile {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		writer.WriteField("path", path)
		writer.WriteField("expected_id", expected)
		part, _ := writer.CreateFormFile("file", "upload.bin")
		io.WriteString(part, value)
		writer.Close()
		sequence++
		code, data, _ := h.raw("POST", "/admin/api/apps/"+a.Key+fmt.Sprintf("/files?transfer_id=%032x", sequence), &body, writer.FormDataContentType(), nil)
		if code != status {
			t.Fatalf("upload got%d want%d: %s", code, status, data)
		}
		var f store.HostedFile
		if status == 201 {
			if err := json.Unmarshal(data, &f); err != nil {
				t.Fatal(err)
			}
		}
		return f
	}
	first := upload("nested/tool.bin", "", "abcdef", 201)
	data, headers := h.request("GET", "/content/files/nested/tool.bin", nil, 206, map[string]string{"Range": "bytes=1-3"})
	if string(data) != "bcd" || headers.Get("Content-Range") != "bytes 1-3/6" || headers.Get("ETag") == "" {
		t.Fatal(string(data), headers)
	}
	h.request("GET", "/content/files/nested/tool.bin", nil, 304, map[string]string{"If-None-Match": headers.Get("ETag")})
	h.request("HEAD", "/content/files/nested/tool.bin", nil, 200, nil)
	upload("nested/tool.bin", "", "oops", 409)
	replacement := upload("nested/tool.bin", first.ID, "replacement", 201)
	h.request("DELETE", "/admin/api/apps/content/other/files/"+replacement.ID, nil, 404, nil)
	upload("../escape", "", "bad", 400)
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.RawQuery != "token=private-signature" {
			t.Error("signed query not forwarded")
		}
		w.Write([]byte("imported"))
	}))
	defer upstream.Close()
	imported, _ := h.request("POST", "/admin/api/apps/content/files/files/import?transfer_id=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", map[string]any{"path": "remote.zip", "url": upstream.URL + "/asset?token=private-signature"}, 201, nil)
	if bytes.Contains(imported, []byte("private-signature")) {
		t.Fatal("import response leaked URL")
	}
	for range 2 {
		data, _ = h.request("GET", "/content/files/remote.zip", nil, 200, nil)
		if string(data) != "imported" {
			t.Fatal(string(data))
		}
	}
	if requests.Load() != 1 {
		t.Fatal("public download contacted origin", requests.Load())
	}
	data, _ = h.request("GET", "/api/apps/content/files/files?page=99&limit=1", nil, 200, nil)
	var page store.Page[store.HostedFile]
	json.Unmarshal(data, &page)
	if page.Page != 2 || page.Total != 2 || len(page.Items) != 1 {
		t.Fatal(string(data))
	}
	data, _ = h.request("POST", "/admin/api/apps/content/files/files/import?transfer_id=ffffffffffffffffffffffffffffffff", map[string]any{"path": "bad", "url": "https://user:private-password@example.com/a?token=private-signature"}, 502, nil)
	if bytes.Contains(data, []byte("private-")) {
		t.Fatal("error leaked credentials", string(data))
	}
	var count int
	if err := h.server.DB.DB.QueryRow(`SELECT COUNT(*) FROM hosted_files WHERE app_uid=?`, a.UID).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if err := h.server.DB.DB.QueryRow(`SELECT COUNT(*) FROM http_cache_generations`).Scan(&count); err != nil || count != 0 {
		t.Fatal("hosted entered cache cleanup", count, err)
	}
	password := h.password
	h.close()
	h = newDirectoryHarness(t, dir)
	h.login(password)
	data, _ = h.request("GET", "/content/files/nested/tool.bin", nil, 200, nil)
	if string(data) != "replacement" {
		t.Fatal("restart lost file", string(data))
	}
	h.request("DELETE", "/admin/api/apps/content/files", map[string]any{"revision": a.Revision, "confirm_key": a.Key}, 200, nil)
	h.request("GET", "/content/files/remote.zip", nil, 404, nil)
	h.request("GET", "/admin/api/apps/content/files/files", nil, 404, nil)
	h.request("GET", "/content/files/nested/tool.bin", nil, 404, nil)
	if err := h.server.DB.DB.QueryRow(`SELECT count(*) FROM hosted_files WHERE app_uid=?`, a.UID).Scan(&count); err != nil || count != 0 {
		t.Fatal("saved files survived permanent deletion", count, err)
	}

}
func TestNumberedAPIInputAndVersionClamping(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	for _, query := range []string{"page=0", "page=-1", "page=01", "page=1&page=2", "limit=101", "page=1000000001"} {
		h.request("GET", "/admin/api/vendors?"+query, nil, 400, nil)
	}
	entry, _ := h.server.Registry.Lookup("openai/codex")
	for i := 1; i <= 4; i++ {
		if _, err := h.server.DB.DB.Exec(`INSERT INTO app_versions(app_id,version,first_seen_s) VALUES(?,?,?)`, entry.StorageID(), fmt.Sprintf("1.0.%d", i), i); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := h.request("GET", "/admin/api/apps/openai/codex/versions?page=99&limit=3", nil, 200, nil)
	var page store.Page[listedVersion]
	json.Unmarshal(data, &page)
	if page.Total != 4 || page.Page != 2 || len(page.Items) != 1 {
		t.Fatal(string(data))
	}
	data, _ = h.request("GET", "/admin/api/apps/anthropic/claude-code/versions?page=99&limit=3", nil, 200, nil)
	json.Unmarshal(data, &page)
	if page.Total != 0 || page.Page != 1 || len(page.Items) != 0 {
		t.Fatal("app leak", string(data))
	}
	h.request("GET", "/admin/api/apps/openai/codex/versions?page=1&cursor=abc", nil, 400, nil)
}

func TestHostedStreamingUploadExtendsOnlyAuthenticatedBodyDeadline(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
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
		time.Sleep(100 * time.Millisecond)
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

func TestHostedCancelImportClosesUpstreamAndCannotBeReachedThroughInfo(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	h.login(h.password)
	h.createVendor("cancel")
	app := h.createApp("cancel", "files", "hosted", nil)
	h.createApp("cancel", "info", "info", nil)
	started, closed := make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(200)
		w.Write([]byte("temporary"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(closed)
	}))
	defer upstream.Close()
	input := map[string]any{"path": "cancel.bin", "url": upstream.URL}
	h.request("POST", "/admin/api/apps/cancel/info/files/import?transfer_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", input, 404, nil)
	h.request("POST", "/admin/api/apps/cancel/info/cache/refresh", input, 404, nil)
	h.request("POST", "/admin/api/apps/cancel/files/cache/refresh", input, 404, nil)
	h.request("GET", "/cancel/info/file?url="+upstream.URL, nil, 400, nil)
	h.request("GET", "/cancel/files/missing?url="+upstream.URL, nil, 400, nil)
	if count.Load() != 0 {
		t.Fatal("capability bypass contacted an upstream")
	}
	bodyBytes, _ := json.Marshal(input)
	request, err := http.NewRequest("POST", h.http.URL+"/admin/api/apps/cancel/files/files/import?transfer_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", h.csrf)
	type result struct {
		status int
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
		_, e = io.Copy(io.Discard, r.Body)
		finished <- result{r.StatusCode, e}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("import did not start")
	}
	h.request("DELETE", "/admin/api/apps/cancel/files/files/transfers/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil, 200, nil)
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel left upstream alive")
	}
	select {
	case result := <-finished:
		if result.err != nil || result.status != 409 {
			t.Fatal(result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel left import running")
	}
	h.request("GET", "/admin/api/apps/cancel/files/files/transfers/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil, 404, nil)
	h.request("GET", "/cancel/files/cancel.bin", nil, 404, nil)
	files, e := h.server.DB.HostedPage(app.UID, 1, 25)
	if e != nil || files.Total != 0 {
		t.Fatal("cancel published a resource", files, e)
	}
	objects, e := os.ReadDir(filepath.Join(h.server.Dir, "objects", "hosted"))
	if e != nil || len(objects) != 0 {
		t.Fatal("cancel left temporary objects", objects, e)
	}
	if count.Load() != 1 {
		t.Fatal("cancel retried import", count.Load())
	}
}
