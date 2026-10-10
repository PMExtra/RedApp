package distributor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/store"
)

func TestConfiguredBaseAndRelativePathBoundaries(t *testing.T) {
	for _, base := range []string{"http://127.0.0.1:8080/files/", "https://internal.example:8443/文件 storage/", "http://[::1]:8080"} {
		c, err := NewPool().NewClient(base, ConfiguredRelease)
		if err != nil {
			t.Fatalf("configured base %q: %v", base, err)
		}
		path, err := c.RelativeURL("子目录/file name.bin")
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(path)
		if err = c.Validate(u); err != nil || !strings.HasSuffix(u.Path, "/子目录/file name.bin") {
			t.Fatalf("decoded path was not preserved: %q, %v", path, err)
		}
	}
	for _, base := range []string{
		"https://user:password@example.com/files", "http://example.com:0/files", "http://example.com:65536/files", "http://example.com:/files", "ftp://example.com/files",
		"http://example.com/files?", "http://example.com/files?x=y", "http://example.com/files#fragment", "http://example.com/files#", "http://example.com/files//", "http://example.com/../files", "http://example.com/%2e%2e/files", "http://example.com/a%2fb", "http://example.com/%252e%252e/files", "http://example.com/files\\escape",
	} {
		if _, err := NewPool().NewClient(base, GeneralHTTP); err == nil {
			t.Fatalf("invalid base accepted: %q", base)
		}
	}
	c, _ := NewPool().NewClient("http://internal.example:8080/root", GeneralHTTP)
	for _, path := range []string{"", "/absolute", "../escape", "a/./b", "a//b", "a\\b", "a?url=https://other.example", "a#fragment", "a/%2e%2e/b", "a\x00b", "https://other.example/file"} {
		if _, err := c.RelativeURL(path); err == nil {
			t.Fatalf("invalid relative path accepted: %q", path)
		}
	}
	for _, source := range []string{"http://other.example:8080/root/file", "http://internal.example:8080/rooted/file", "http://internal.example:8080/root/a%2fb", "http://internal.example:8080/root/../file"} {
		u, _ := url.Parse(source)
		if c.Validate(u) == nil {
			t.Fatalf("initial request escaped configured boundary: %s", source)
		}
	}
	for _, base := range []string{"http://example.com/files", "https://example.com:8443/files"} {
		if _, err := New(base); err == nil {
			t.Fatal("public release constructor became permissive", base)
		}
	}
}

func TestConfiguredHTTPMethodsAndHeaderAllowlist(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/目录/file name" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Errorf("incorrect request: %s, encoding %s", r.URL.Path, r.Header.Get("Accept-Encoding"))
		}
		for key, value := range map[string]string{"Range": "bytes=0-3", "If-Range": `"v1"`, "If-None-Match": `"v0"`, "If-Modified-Since": "Wed, 21 Oct 2015 07:28:00 GMT", "If-Match": `"v1"`, "If-Unmodified-Since": "Wed, 21 Oct 2015 07:28:00 GMT"} {
			if r.Header.Get(key) != value {
				t.Errorf("header %s not preserved", key)
			}
		}
		for _, key := range []string{"Authorization", "Cookie", "Proxy-Authorization", "Origin", "Forwarded", "X-Unrelated"} {
			if r.Header.Get(key) != "" {
				t.Errorf("unapproved header forwarded: %s", key)
			}
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Length", "4")
		if r.Method != http.MethodHead {
			io.WriteString(w, "file")
		}
	}))
	defer upstream.Close()
	c, _ := NewPool().NewClient(upstream.URL+"/files", GeneralHTTP)
	source, _ := c.RelativeURL("目录/file name")
	headers := http.Header{"Range": {"bytes=0-3"}, "If-Range": {`"v1"`}, "If-None-Match": {`"v0"`}, "If-Modified-Since": {"Wed, 21 Oct 2015 07:28:00 GMT"}, "If-Match": {`"v1"`}, "If-Unmodified-Since": {"Wed, 21 Oct 2015 07:28:00 GMT"}, "Authorization": {"secret"}, "Cookie": {"secret"}, "Proxy-Authorization": {"secret"}, "Origin": {"http://untrusted.example"}, "Forwarded": {"host=untrusted"}, "X-Unrelated": {"drop"}}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		resp, err := c.Do(context.Background(), method, source, headers)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.Header.Get("ETag") != `"v1"` || (method == http.MethodGet && string(body) != "file") || (method == http.MethodHead && len(body) != 0) {
			t.Fatalf("method %s failed: %q, %v", method, body, err)
		}
	}
	untrustedTLS := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "untrusted") }))
	defer untrustedTLS.Close()
	configuredTLS, _ := NewPool().NewClient(untrustedTLS.URL+"/files", GeneralHTTP)
	if _, err := configuredTLS.Get(context.Background(), configuredTLS.URL("asset"), nil); err == nil {
		t.Fatal("configured source skipped TLS certificate verification")
	}
	if _, err := c.Do(context.Background(), http.MethodPost, source, nil); err == nil {
		t.Fatal("POST accepted")
	}
	// Clearing a proxy must restore each mode's direct dial behavior, rather
	// than retaining the permissive configured dialer for public clients.
	public, _ := New("https://127.0.0.1/files")
	if _, err := public.Get(context.Background(), public.URL("asset"), nil); err == nil {
		t.Fatal("public client connected to a loopback destination")
	}
}

func TestRedirectModesAndCredentialBoundary(t *testing.T) {
	pool := NewPool()
	general, _ := pool.NewClient("https://source.example/files", GeneralHTTP)
	release, _ := pool.NewClient("https://source.example/files", ConfiguredRelease)
	httpSource, _ := pool.NewClient("http://source.example/files", GeneralHTTP)
	for _, tc := range []struct {
		name, target string
		client       *Client
		allowed      bool
	}{
		{"general same root", "https://source.example/files/next", general, true},
		{"same-host HTTPS upgrade", "https://source.example/files/next", httpSource, true},
		{"cross-host HTTPS upgrade", "https://cdn.example/files/next", httpSource, true},
		{"cross-host HTTP denied", "http://cdn.example/files/next", httpSource, false},
		{"general outside root", "https://source.example/other/next", general, false},
		{"general CDN", "https://cdn.example/download/file", general, true},
		{"release CDN", "https://cdn.example/download/file", release, false},
		{"downgrade", "http://cdn.example/download/file", general, false},
		{"redirect query", "https://cdn.example/file?token=secret", general, false},
		{"redirect credentials", "https://user:secret@cdn.example/file", general, false},
		{"redirect traversal", "https://cdn.example/a/%2e%2e/file", general, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initial, _ := http.NewRequest(http.MethodGet, tc.client.URL("start"), nil)
			next, _ := http.NewRequest(http.MethodGet, tc.target, nil)
			for _, header := range []string{"Authorization", "Proxy-Authorization", "Cookie", "Cookie2", "Referer"} {
				next.Header.Set(header, "sensitive")
			}
			err := tc.client.checkRedirect(next, []*http.Request{initial})
			if (err == nil) != tc.allowed {
				t.Fatalf("redirect result %v, expected allowed %v", err, tc.allowed)
			}
			if !sameOrigin(initial.URL, next.URL) {
				for _, header := range []string{"Authorization", "Proxy-Authorization", "Cookie", "Cookie2", "Referer"} {
					if next.Header.Get(header) != "" {
						t.Fatalf("cross-origin %s retained", header)
					}
				}
			}
		})
	}
	// Run the actual net/http redirect machinery with an injected RoundTripper.
	calls := 0
	general.HTTP = &http.Client{Transport: compressionTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://cdn.example/file"}}, Body: http.NoBody, Request: r}, nil
		}
		if r.URL.Host != "cdn.example" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("redirect request incorrect")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("file")), Request: r}, nil
	})}
	resp, err := general.Get(context.Background(), general.URL("start"), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls != 2 {
		t.Fatal("HTTPS cross-origin redirect not followed")
	}
	calls = 0
	general.HTTP.Transport = compressionTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://cdn.example/loop"}}, Body: http.NoBody, Request: r}, nil
	})
	if _, err = general.Get(context.Background(), general.URL("start"), nil); err == nil {
		t.Fatal("unbounded redirect loop")
	}
	if calls != 5 {
		t.Fatalf("expected initial request plus four redirects; got %d requests", calls)
	}
}

func TestPoolProxyIndependentOfApplicationsAndSharedAcrossModes(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	pool := NewPool()
	if err = pool.LoadProxy(db); err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") == "" {
			t.Error("shared proxy credentials missing")
		}
		io.WriteString(w, "proxied")
	}))
	defer proxy.Close()
	if err = pool.SetProxy(ProxyUpdate{Mode: "url", URL: strings.Replace(proxy.URL, "://", "://proxy-user:proxy-secret@", 1)}, pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	general, _ := pool.NewClient("http://internal.invalid/files", GeneralHTTP)
	release, _ := pool.NewClient("http://other.internal.invalid/releases", ConfiguredRelease)
	for _, c := range []*Client{general, release} {
		if c.pool != pool {
			t.Fatal("client proxy state differs from owner")
		}
		resp, err := c.Get(context.Background(), c.URL("asset"), nil)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != "proxied" {
			t.Fatal("new client did not inherit proxy")
		}
	}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "direct") }))
	defer local.Close()
	configured, _ := pool.NewClient(local.URL+"/files", ConfiguredRelease)
	public, _ := pool.NewClient("https://127.0.0.1/files", PublicRelease)
	before := pool.transports.Load()
	if err = pool.SetProxy(ProxyUpdate{Mode: "direct"}, pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	after := pool.transports.Load()
	if before == after || after.public.Proxy != nil || after.configured.Proxy != nil || pool.Proxy().URL != "" {
		t.Fatal("proxy modes or settings did not switch together")
	}
	if err = pool.SetProxy(ProxyUpdate{Mode: "url", URL: proxy.URL}, 0); err == nil || pool.transports.Load() != after {
		t.Fatal("stale CAS replaced shared transport")
	}
	// A configured client created before the update regains its private direct
	// transport once the global proxy has been cleared.
	if _, err = public.Get(context.Background(), public.URL("asset"), nil); err == nil {
		t.Fatal("public client regained configured dialer after proxy clear")
	}
	resp, err := configured.Get(context.Background(), configured.URL("asset"), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}
