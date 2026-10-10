package distributor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/PMExtra/RedApp/internal/store"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func proxyFixture(t *testing.T) (*Client, *store.Store, *httptest.Server) {
	t.Helper()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			if r.Header.Get("If-Range") != "\"trusted\"" {
				t.Error("resume validator lost")
			}
			w.WriteHeader(206)
		}
		io.WriteString(w, "trusted bytes")
	}))
	t.Cleanup(upstream.Close)
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	c, err := New("https://example.com/codex")
	if err != nil {
		t.Fatal(err)
	}
	if err = c.pool.LoadProxy(db); err != nil {
		t.Fatal(err)
	}
	return c, db, upstream
}
func trustFixture(c *Client, upstream *httptest.Server) {
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	c.transports.current.Load().TLSClientConfig = &tls.Config{RootCAs: roots}
}
func connectProxy(t *testing.T, upstream *httptest.Server, auth chan string, connected chan struct{}) *httptest.Server {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || r.Host != "example.com:443" {
			t.Error("unexpected proxy destination")
			http.Error(w, "rejected", 400)
			return
		}
		if auth != nil {
			auth <- r.Header.Get("Proxy-Authorization")
		}
		remote, err := net.Dial("tcp", upstream.Listener.Addr().String())
		if err != nil {
			t.Error(err)
			return
		}
		conn, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			remote.Close()
			return
		}
		buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		buffer.Flush()
		if connected != nil {
			connected <- struct{}{}
		}
		go func() { io.Copy(remote, buffer); remote.Close() }()
		io.Copy(conn, remote)
		conn.Close()
	}))
	t.Cleanup(proxy.Close)
	return proxy
}
func TestHTTPProxyTLSResumeAndPrivateCredentials(t *testing.T) {
	c, db, upstream := proxyFixture(t)
	auth := make(chan string, 4)
	proxy := connectProxy(t, upstream, auth, nil)
	if err := c.pool.SetProxy(ProxyUpdate{Mode: "url", URL: strings.Replace(proxy.URL, "://", "://test-user:private-test-secret@", 1)}, c.pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	transport := c.transports.current.Load()
	if err := c.pool.SetProxy(ProxyUpdate{Mode: "direct"}, 0); err == nil || c.transports.current.Load() != transport || c.pool.Proxy().Revision != 1 {
		t.Fatal("stale proxy update changed the active transport")
	}
	if _, err := c.Get(context.Background(), c.URL("channels/latest"), nil); err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	} else if err.Error() != "Upstream connection failed" {
		t.Fatal("connection error exposed details")
	}
	trustFixture(c, upstream)
	for _, path := range []string{"channels/latest", "releases/0.159.2/release.json", "releases/0.159.2/archive.tgz"} {
		resp, err := c.Get(context.Background(), c.URL(path), nil)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(data) != "trusted bytes" {
			t.Fatal("proxy transfer failed")
		}
	}
	resp, err := c.Get(context.Background(), c.URL("releases/0.159.2/archive.tgz"), http.Header{"Range": []string{"bytes=10-"}, "If-Range": []string{"\"trusted\""}})
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 {
		t.Fatal("resume status lost")
	}
	if got := <-auth; !strings.HasPrefix(got, "Basic ") {
		t.Fatal("proxy authentication missing")
	}
	b, _ := json.Marshal(c.pool.Proxy())
	if !strings.Contains(string(b), "private-test-secret") {
		t.Fatal("administrative URL must preserve userinfo")
	}
	if err = c.pool.SetProxy(ProxyUpdate{Mode: "url", URL: c.pool.Proxy().URL}, c.pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := New(c.Base.String())
	if err = reloaded.pool.LoadProxy(db); err != nil || !strings.Contains(reloaded.pool.Proxy().URL, "private-test-secret") {
		t.Fatal("credentials not persisted")
	}
	// The redacted view round-trips only to the same scheme, user and host.
	saved := c.pool.Proxy()
	redacted := saved.Redacted()
	if b, _ = json.Marshal(redacted); strings.Contains(string(b), "private-test-secret") || !strings.Contains(redacted.URL, "://test-user:****@") {
		t.Fatal("redacted view", string(b))
	}
	for _, moved := range []string{strings.Replace(redacted.URL, "test-user", "other-user", 1), strings.Replace(redacted.URL, "http://", "socks5://", 1), strings.Replace(redacted.URL, "127.0.0.1", "localhost", 1), "http://test-user:****@proxy.example:3128"} {
		if err = c.pool.SetProxy(ProxyUpdate{Mode: "url", URL: moved}, saved.Revision); err == nil || !errors.Is(err, ErrInvalidProxySettings) {
			t.Fatal("redacted password moved to another proxy", moved, err)
		}
	}
	if err = c.pool.SetProxy(ProxyUpdate{Mode: "url", URL: redacted.URL}, saved.Revision); err != nil || c.pool.Proxy().URL != saved.URL || c.pool.Proxy().Revision != saved.Revision+1 {
		t.Fatal("redacted password not kept", err, c.pool.Proxy().Redacted())
	}
	if err = c.pool.SetProxy(ProxyUpdate{Mode: "url", URL: proxy.URL}, c.pool.Proxy().Revision); err != nil || strings.Contains(c.pool.Proxy().URL, "@") {
		t.Fatal("clear failed")
	}
	if err = c.pool.SetProxy(ProxyUpdate{Mode: "direct"}, c.pool.Proxy().Revision); err != nil || c.transports.current.Load().Proxy != nil {
		t.Fatal("empty proxy must be direct")
	}
	t.Setenv("HTTPS_PROXY", proxy.URL)
	if c.transports.current.Load().Proxy != nil {
		t.Fatal("environment proxy inherited")
	}
	for _, target := range []string{"https://attacker.example/codex/channels/latest", "https://example.com/other/path"} {
		if _, err = c.Get(context.Background(), target, nil); err == nil {
			t.Fatal("proxy bypassed upstream whitelist")
		}
	}
}
func TestSOCKS5UsesProxyDNS(t *testing.T) {
	c, _, upstream := proxyFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	hostname := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var header [2]byte
		if _, err = io.ReadFull(conn, header[:]); err != nil {
			return
		}
		methods := make([]byte, int(header[1]))
		io.ReadFull(conn, methods)
		conn.Write([]byte{5, 0})
		var req [4]byte
		io.ReadFull(conn, req[:])
		if req[3] != 3 {
			hostname <- "not-domain"
			return
		}
		var size [1]byte
		io.ReadFull(conn, size[:])
		name := make([]byte, int(size[0]))
		io.ReadFull(conn, name)
		var port [2]byte
		io.ReadFull(conn, port[:])
		if binary.BigEndian.Uint16(port[:]) != 443 {
			hostname <- "bad-port"
			return
		}
		hostname <- string(name)
		remote, err := net.Dial("tcp", upstream.Listener.Addr().String())
		if err != nil {
			return
		}
		defer remote.Close()
		conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1})
		go io.Copy(remote, conn)
		io.Copy(conn, remote)
	}()
	if err = c.pool.SetProxy(ProxyUpdate{Mode: "url", URL: "socks5://" + listener.Addr().String()}, c.pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	trustFixture(c, upstream)
	resp, err := c.Get(context.Background(), c.URL("channels/latest"), nil)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	c.transports.current.Load().CloseIdleConnections()
	if got := <-hostname; got != "example.com" {
		t.Fatalf("SOCKS destination %q", got)
	}
}
func TestProxySwapDoesNotCancelActiveResponse(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		once.Do(func() { close(started); <-release })
		io.WriteString(w, "finished")
	}))
	defer upstream.Close()
	db, _ := store.Open(t.TempDir())
	defer db.DB.Close()
	c, _ := New("https://example.com/codex")
	c.pool.LoadProxy(db)
	first := connectProxy(t, upstream, nil, nil)
	second := connectProxy(t, upstream, nil, nil)
	c.pool.SetProxy(ProxyUpdate{Mode: "url", URL: first.URL}, c.pool.Proxy().Revision)
	trustFixture(c, upstream)
	resp, err := c.Get(context.Background(), c.URL("channels/latest"), nil)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err = c.pool.SetProxy(ProxyUpdate{Mode: "url", URL: second.URL}, c.pool.Proxy().Revision); err != nil {
		t.Fatal(err)
	}
	trustFixture(c, upstream)
	close(release)
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(data) != "finished" {
		t.Fatal("active response cancelled", err)
	}
	next, err := c.Get(context.Background(), c.URL("channels/latest"), nil)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, next.Body)
	next.Body.Close()
	c.transports.current.Load().CloseIdleConnections()
}

func TestSiblingSharesProxyUpdatesButKeepsOriginBoundary(t *testing.T) {
	c, _, upstream := proxyFixture(t)
	sibling, err := c.Sibling("https://example.com/claude")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		proxy := connectProxy(t, upstream, nil, nil)
		if err = c.pool.SetProxy(ProxyUpdate{Mode: "url", URL: proxy.URL}, c.pool.Proxy().Revision); err != nil {
			t.Fatal(err)
		}
		trustFixture(c, upstream)
		for _, client := range []*Client{c, sibling} {
			resp, err := client.Get(context.Background(), client.URL("manifest.json"), nil)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if string(body) != "trusted bytes" {
				t.Fatal("shared proxy not applied")
			}
		}
		if _, err = sibling.Get(context.Background(), c.URL("manifest.json"), nil); err == nil {
			t.Fatal("sibling accepted foreign path")
		}
		proxy.Close()
	}
}

func TestLegacyServerReadsExactEncodedURLWithoutRewrite(t *testing.T) {
	c, db, _ := proxyFixture(t)
	if _, err := db.CompareAndSwapSetting("global", "", "upstream_proxy", 0, map[string]string{"server": "http://user%40name:p%3Aa%2Fss@proxy.example:3128"}); err != nil {
		t.Fatal(err)
	}
	if err := c.pool.LoadProxy(db); err != nil {
		t.Fatal(err)
	}
	want := "http://user%40name:p%3Aa%2Fss@proxy.example:3128"
	if c.pool.Proxy().URL != want || c.pool.Proxy().Revision != 1 {
		t.Fatal(c.pool.Proxy())
	}
	var stored map[string]any
	rev, err := db.ReadSetting("global", "", "upstream_proxy", &stored)
	if err != nil || rev != 1 || len(stored) != 2 || stored["mode"] != "url" || stored["url"] != want {
		t.Fatal("dual credential representation survived", stored, err)
	}
	if err = c.pool.LoadProxy(db); err != nil || c.pool.Proxy().Revision != 1 {
		t.Fatal("migration repeated", err)
	}
	for _, bad := range []string{"http://u:p%0Ass@proxy.example:3128", "http://proxy.example:3128?", "http://proxy.example:3128#"} {
		if err = c.pool.SetProxy(ProxyUpdate{Mode: "url", URL: bad}, 1); err == nil {
			t.Fatal("invalid URL accepted", bad)
		}
	}
	if c.pool.Proxy().URL != want || c.pool.Proxy().Revision != 1 {
		t.Fatal("failed update altered transport")
	}
}
