package httpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/apps/codex"
)

// anyCodexRelease serves fixtureUpstream as a Codex source that has a release
// with one asset ("binary:<version>") for every requested version.
func anyCodexRelease() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		version := "2.0.0"
		if len(parts) >= 3 && parts[0] == "releases" {
			version = parts[1]
		}
		body := []byte("binary:" + version)
		if strings.HasSuffix(r.URL.Path, "release.json") || parts[0] == "channels" {
			digest := sha256.Sum256(body)
			json.NewEncoder(w).Encode(codex.Release{Tag: "rust-v" + version, Assets: []codex.Asset{{Name: "asset.tgz", Digest: "sha256:" + hex.EncodeToString(digest[:]), URL: fixtureUpstream + "/releases/" + version + "/asset.tgz"}}})
			return
		}
		w.Write(body)
	})
}

// cachedReleases creates fixture/codex and downloads one asset of each version.
func cachedReleases(t *testing.T, h *harness, versions ...string) string {
	t.Helper()
	h.upstreamProxy(anyCodexRelease())
	key := h.releaseApp("fixture", "codex", "codex")
	for _, version := range versions {
		if body, _ := h.request("GET", "/"+key+"/releases/"+version+"/asset.tgz", nil, 200, nil); string(body) != "binary:"+version {
			t.Fatal(version, string(body))
		}
	}
	return key
}

func versionNames(items []versionDTO) string {
	names := []string{}
	for _, v := range items {
		names = append(names, v.Version)
	}
	return strings.Join(names, ",")
}

func TestVersionListIsNewestFirstAndCursorBound(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	key := cachedReleases(t, h, "1.0.0", "10.0.0", "2.0.0", "1.10.0")
	base := "/admin/api/apps/" + key + "/versions"
	data, _ := h.request("GET", base+"?limit=3", nil, 200, nil)
	first := decodeJSONBody[cursorPage[versionDTO]](t, data)
	if versionNames(first.Items) != "10.0.0,2.0.0,1.10.0" || first.NextCursor == nil {
		t.Fatal("versions are not newest first", string(data))
	}
	if v := first.Items[0]; v.Requests != 1 || v.DownstreamBytes != int64(len("binary:10.0.0")) || v.FirstSeen.IsZero() {
		t.Fatal("version statistics", v)
	}
	data, _ = h.request("GET", base+"?limit=3&cursor="+url.QueryEscape(*first.NextCursor), nil, 200, nil)
	if last := decodeJSONBody[cursorPage[versionDTO]](t, data); versionNames(last.Items) != "1.0.0" || last.NextCursor != nil {
		t.Fatal("last page", string(data))
	}
	cursor := url.QueryEscape(*first.NextCursor)
	h.expectError("GET", "/admin/api/apps/"+key+"/resources?cursor="+cursor, nil, 400, codeInvalidCursor, nil)
	for _, bad := range []string{"not-a-cursor", "e30", strings.Repeat("A", 2100)} {
		h.expectError("GET", base+"?cursor="+bad, nil, 400, codeInvalidCursor, nil)
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=01", "page=1", "limit=1&limit=2", "cursor="} {
		h.expectError("GET", base+"?"+query, nil, 400, codeInvalidQuery, nil)
	}
	other := h.createApp("fixture", "other", "codex", map[string]any{"base_url": fixtureUpstream})
	h.expectError("GET", "/admin/api/apps/"+other.Key+"/versions?cursor="+cursor, nil, 400, codeInvalidCursor, nil)
	// A new source epoch has its own versions; cursors of the old one are invalid.
	h.patchApp(key, map[string]any{"base_url": fixtureUpstream + "/mirror"})
	h.expectError("GET", base+"?cursor="+cursor, nil, 400, codeInvalidCursor, nil)
	data, _ = h.request("GET", base, nil, 200, nil)
	if page := decodeJSONBody[cursorPage[versionDTO]](t, data); len(page.Items) != 0 || page.NextCursor != nil {
		t.Fatal("new source epoch listed old versions", string(data))
	}
}

func TestResourceListFiltersByVersionWithoutInternalFields(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	key := cachedReleases(t, h, "1.0.0", "2.0.0", "3.0.0")
	base := "/admin/api/apps/" + key + "/resources"
	data, _ := h.request("GET", base+"?limit=2", nil, 200, nil)
	first := decodeJSONBody[cursorPage[resourceDTO]](t, data)
	if len(first.Items) != 2 || first.NextCursor == nil || first.Items[0].ID >= first.Items[1].ID {
		t.Fatal("resources are not ordered by generation ID", string(data))
	}
	for _, private := range []string{h.dir, `"app/`, "upstream.example", `"path"`, "source_fence", "metrics"} {
		if bytes.Contains(data, []byte(private)) {
			t.Fatalf("resource list exposed %q: %s", private, data)
		}
	}
	item := first.Items[0]
	if item.Key != "asset.tgz" || item.State != "complete" || !item.Current || item.Retired || item.Bytes != int64(len("binary:1.0.0")) || item.TotalBytes == nil || item.FinishedAt == nil || item.Error != nil || len(item.SHA256) != 64 {
		t.Fatal("resource fields", item)
	}
	data, _ = h.request("GET", base+"?limit=2&cursor="+url.QueryEscape(*first.NextCursor), nil, 200, nil)
	if last := decodeJSONBody[cursorPage[resourceDTO]](t, data); len(last.Items) != 1 || last.NextCursor != nil || last.Items[0].ID <= first.Items[1].ID {
		t.Fatal("resource continuation", string(data))
	}
	data, _ = h.request("GET", base+"?version=2.0.0", nil, 200, nil)
	if page := decodeJSONBody[cursorPage[resourceDTO]](t, data); len(page.Items) != 1 || page.Items[0].Version != "2.0.0" {
		t.Fatal("version filter", string(data))
	}
	h.expectError("GET", base+"?version=2.0.0&cursor="+url.QueryEscape(*first.NextCursor), nil, 400, codeInvalidCursor, nil)
	h.expectError("GET", "/admin/api/apps/"+key+"/versions?cursor="+url.QueryEscape(*first.NextCursor), nil, 400, codeInvalidCursor, nil)
	h.expectError("GET", base+"?version=v2", nil, 400, codeInvalidQuery, nil)
	data, _ = h.request("GET", base+"?version=9.0.0", nil, 200, nil)
	if page := decodeJSONBody[cursorPage[resourceDTO]](t, data); len(page.Items) != 0 || page.NextCursor != nil {
		t.Fatal(string(data))
	}
	// Disabled applications stay manageable.
	h.setAppEnabled(key, false)
	data, _ = h.request("GET", base, nil, 200, nil)
	if page := decodeJSONBody[cursorPage[resourceDTO]](t, data); len(page.Items) != 3 {
		t.Fatal("disabled application resources", string(data))
	}
}

func TestReleaseListsRequireReleaseProvider(t *testing.T) {
	h := newHarness(t)
	h.login(h.password)
	h.createVendor("content")
	apps := map[string]map[string]any{"info": nil, "hosted": nil, "http-cache": {"base_url": "https://example.com"}}
	for provider, options := range apps {
		a := h.createApp("content", strings.ReplaceAll(provider, "-", ""), provider, options)
		for _, path := range []string{"versions", "resources", "retention/status"} {
			h.expectError("GET", "/admin/api/apps/"+a.Key+"/"+path, nil, 404, codeCapabilityUnsupported, nil)
		}
		h.expectError("POST", "/admin/api/apps/"+a.Key+"/version-cleanup/preview", map[string]string{"minimum_version": "1.0.0"}, 404, codeCapabilityUnsupported, nil)
	}
	h.expectError("GET", "/admin/api/apps/content/missing/versions", nil, 404, codeApplicationNotFound, nil)
	h.expectError("GET", "/admin/api/apps/content/Not_Valid/resources", nil, 400, codeInvalidPath, nil)
}
