package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
)

// decodeIndex strictly decodes a FileIndex document.
func decodeIndex(t *testing.T, body []byte) fileIndex {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var index fileIndex
	if err := decoder.Decode(&index); err != nil {
		t.Fatal(err, string(body))
	}
	return index
}

type indexRowSummary struct {
	Name, Type string
	Size       int64
	Dated      bool
}

func summarize(index fileIndex) []indexRowSummary {
	out := []indexRowSummary{}
	for _, e := range index.Entries {
		row := indexRowSummary{Name: e.Name, Type: e.Type, Dated: e.Modified != nil}
		if e.Size != nil {
			row.Size = *e.Size
		}
		out = append(out, row)
	}
	return out
}

func TestWantsJSONPrefersHTMLOnTies(t *testing.T) {
	for accept, want := range map[string]bool{
		"":                                  false,
		"*/*":                               false,
		"application/json":                  true,
		"application/json, text/html;q=0.9": true,
		"application/json,text/html":        false,
		"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8": false,
		"application/json;q=0.5, */*;q=0.1":                               true,
		"application/json;q=0":                                            false,
	} {
		r := httptest.NewRequest("GET", "/", nil)
		if accept != "" {
			r.Header.Set("Accept", accept)
		}
		if got := wantsJSON(r); got != want {
			t.Errorf("%q: %v", accept, got)
		}
	}
}

func TestHostedAutoIndexListsDirectories(t *testing.T) {
	h := newHarness(t)
	h.createVendor("acme")
	a := h.createApp("acme", "files", application.Hosted, nil)
	h.login("")
	for path, content := range map[string]string{"readme.txt": "r", "docs/a.txt": "aa", "docs/b/c.txt": "c", "x:y <z>.txt": "zzz"} {
		h.upload(a.Key, path, "", content, 201)
	}
	// Off: the root redirects temporarily to the application page and
	// directories are not listed.
	if code, _, header := h.raw("GET", "/acme/files/", nil, "", nil); code != 307 || header.Get("Location") != "/acme/files" {
		t.Fatal("root without auto_index", code, header)
	}
	h.expectError("GET", "/acme/files/docs/", nil, 400, codeInvalidPath, nil)
	h.expectError("GET", "/acme/files/docs", nil, 404, codeFileNotFound, nil)

	h.patchApp(a.Key, map[string]any{"auto_index": true})
	body, header := h.request("GET", "/acme/files/", nil, 200, map[string]string{"Accept": "application/json"})
	root := decodeIndex(t, body)
	want := []indexRowSummary{{"docs", "directory", 0, true}, {"readme.txt", "file", 1, true}, {"x:y <z>.txt", "file", 3, true}}
	if root.Path != "/" || root.Stale || root.Truncated || !reflect.DeepEqual(summarize(root), want) || header.Get("Vary") != "Accept" {
		t.Fatalf("root listing %+v %v", summarize(root), header)
	}
	code, page, header := h.raw("GET", "/acme/files/", nil, "", map[string]string{"Accept": "text/html,*/*;q=0.8", "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8"})
	html := string(page)
	if code != 200 || !strings.HasPrefix(header.Get("Content-Type"), "text/html") || header.Get("Content-Security-Policy") != fileIndexPolicy {
		t.Fatal("HTML listing headers", code, header)
	}
	for _, fragment := range []string{`href="./docs/"`, `href="./readme.txt"`, `x:y &lt;z&gt;.txt`, `href="/acme/files"`, "返回应用", `lang="zh-CN"`} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("HTML listing lacks %s:\n%s", fragment, html)
		}
	}
	if strings.Count(html, "<script") != 1 || strings.Count(html, "<style") != 1 || strings.Contains(html, `href="../"`) {
		t.Fatal("HTML listing has extra code or a parent link at the root", html)
	}
	if code, _, header = h.raw("GET", "/acme/files/docs", nil, "", nil); code != 307 || header.Get("Location") != "/acme/files/docs/" {
		t.Fatal("directory without slash", code, header)
	}
	body, _ = h.request("GET", "/acme/files/docs/", nil, 200, map[string]string{"Accept": "application/json"})
	docs := decodeIndex(t, body)
	if docs.Path != "/docs/" || !reflect.DeepEqual(summarize(docs), []indexRowSummary{{"b", "directory", 0, true}, {"a.txt", "file", 2, true}}) {
		t.Fatalf("docs listing %+v", summarize(docs))
	}
	if _, page, _ = h.raw("GET", "/acme/files/docs/b/", nil, "", nil); !strings.Contains(string(page), `href="../"`) {
		t.Fatal("subdirectory page lacks its parent link")
	}
	h.expectError("GET", "/acme/files/missing/", nil, 404, codeFileNotFound, nil)
	if body, _ = h.request("GET", "/acme/files/readme.txt", nil, 200, nil); string(body) != "r" {
		t.Fatal("file served wrongly", string(body))
	}
	if code, page, _ = h.raw("HEAD", "/acme/files/", nil, "", nil); code != 200 || len(page) != 0 {
		t.Fatal("HEAD listing", code, len(page))
	}
}

func TestHTTPCacheAutoIndexListsTheSource(t *testing.T) {
	var failing atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/files/":
			if !strings.HasPrefix(r.Header.Get("Accept"), "application/json") {
				t.Errorf("directory request did not prefer JSON: %q", r.Header.Get("Accept"))
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"name":"dir","type":"directory","mtime":"Wed, 28 Oct 2026 10:00:00 GMT"},{"name":"a.bin","type":"file","mtime":"Wed, 28 Oct 2026 10:00:00 GMT","size":3},{"name":"a:b","type":"file","size":1},{"name":"..","type":"directory"},{"name":"link","type":"other"}]`)
		case "/files/dir":
			http.Redirect(w, r, "/files/dir/", http.StatusMovedPermanently)
		case "/files/dir/":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, `<a href="../">..</a><a href="b.bin">b.bin</a><a href="sub/">sub/</a><a href="https://other.example/x">x</a><a href="/files/elsewhere">e</a><a href="b.bin?x=1">q</a>`)
		case "/files/a.bin":
			io.WriteString(w, "abc")
		case "/files/forbidden/":
			w.WriteHeader(http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	h := newHarness(t)
	h.createVendor("acme")
	a := h.createApp("acme", "mirror", application.HttpCache, map[string]any{"base_url": upstream.URL + "/files", "cache_ttl_seconds": 0})
	h.patchApp(a.Key, map[string]any{"auto_index": true})
	json := map[string]string{"Accept": "application/json"}
	body, _ := h.request("GET", "/acme/mirror/", nil, 200, json)
	root := decodeIndex(t, body)
	if root.Stale || !reflect.DeepEqual(summarize(root), []indexRowSummary{{"dir", "directory", 0, true}, {"a.bin", "file", 3, true}, {"a:b", "file", 1, false}}) {
		t.Fatalf("root listing %+v", summarize(root))
	}
	if code, _, header := h.raw("GET", "/acme/mirror/dir", nil, "", nil); code != 307 || header.Get("Location") != "/acme/mirror/dir/" {
		t.Fatal("source directory without slash", code, header)
	}
	if counters, _ := h.store.Counters(); counters["download_errors"] != 0 {
		t.Fatal("a directory redirect counted as a failed download", counters)
	}
	body, _ = h.request("GET", "/acme/mirror/dir/", nil, 200, json)
	if dir := decodeIndex(t, body); !reflect.DeepEqual(summarize(dir), []indexRowSummary{{"sub", "directory", 0, false}, {"b.bin", "file", 0, false}}) {
		t.Fatalf("HTML source listing %+v", summarize(dir))
	}
	h.expectError("GET", "/acme/mirror/forbidden/", nil, 502, codeIndexUnsupported, nil)
	h.expectError("GET", "/acme/mirror/missing/", nil, 404, codeFileNotFound, nil)
	if body, _ = h.request("GET", "/acme/mirror/a.bin", nil, 200, nil); string(body) != "abc" {
		t.Fatal("file through the cache", string(body))
	}

	// The source fails: the last listing is served as stale; a directory never
	// listed cannot be listed.
	failing.Store(true)
	body, _ = h.request("GET", "/acme/mirror/", nil, 200, json)
	if stale := decodeIndex(t, body); !stale.Stale || len(stale.Entries) != 3 {
		t.Fatalf("stale listing %+v", stale)
	}
	if _, page, _ := h.raw("GET", "/acme/mirror/", nil, "", map[string]string{"Accept-Language": "en"}); !strings.Contains(string(page), "The source is unavailable") {
		t.Fatal("stale page lacks its notice")
	}
	h.expectError("GET", "/acme/mirror/never/", nil, 502, codeUpstreamUnavailable, nil)
}

// The administrator API reads and saves auto_index like any other path.
func TestAutoIndexThroughTheConfigurationAPI(t *testing.T) {
	h := newHarness(t)
	h.createVendor("acme")
	a := h.createApp("acme", "files", application.Hosted, nil)
	h.login("")
	path := "/admin/api/apps/" + a.Key + "/configuration"
	before := getJSON[configurationDTO](h, path)
	if value, ok := before.Effective["auto_index"]; !ok || value != false {
		t.Fatalf("effective configuration lacks auto_index: %v", before.Effective)
	}
	body, _ := h.request("PATCH", path, map[string]any{"set": map[string]any{"auto_index": true}}, 200, map[string]string{"If-Match": `"` + strconv.FormatInt(before.Revision, 10) + `"`})
	if after := decodeJSONBody[configurationDTO](t, body); after.Effective["auto_index"] != true {
		t.Fatalf("auto_index not saved: %v", after.Effective)
	}
	if code, _, _ := h.raw("GET", "/acme/files/", nil, "", nil); code != 200 {
		t.Fatal("listing not enabled", code)
	}
}
