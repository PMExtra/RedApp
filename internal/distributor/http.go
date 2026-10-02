// Package distributor implements a fixed upstream HTTP boundary; it knows no application metadata.
package distributor

import (
	"context"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/store"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Client struct {
	Base        *url.URL
	HTTP        *http.Client
	proxyMu     sync.Mutex
	proxyConfig proxyConfig
	proxyStore  *store.Store
	transports  *transportSwitch
}

func New(base string) (*Client, error) {
	u, e := url.Parse(base)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" || u.Port() != "" {
		return nil, errors.New("BASE_URL must be an HTTPS base URL without port, query, or credentials")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	c := &Client{Base: u}
	tr, _ := transportFor(proxyConfig{})
	c.transports = &transportSwitch{}
	c.transports.current.Store(tr)
	c.HTTP = &http.Client{Transport: c.transports, Timeout: 5 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return errors.New("Too many redirects")
		}
		return c.Validate(r.URL)
	}}
	return c, nil
}
func publicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	// Shared address space and benchmarking networks are not public upstream destinations.
	if v := ip.To4(); v != nil {
		if v[0] == 100 && v[1] >= 64 && v[1] <= 127 || v[0] == 198 && (v[1] == 18 || v[1] == 19) {
			return false
		}
	}
	return true
}
func (c *Client) Validate(u *url.URL) error {
	if u.Scheme != c.Base.Scheme || u.Host != c.Base.Host || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return errors.New("Only the fixed upstream without query parameters is allowed")
	}
	if u.RawPath != "" || strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "//") || !strings.HasPrefix(u.Path, c.Base.Path+"/") {
		return errors.New("Invalid upstream path")
	}
	for _, p := range strings.Split(u.Path, "/") {
		if p == ".." || p == "." {
			return errors.New("Upstream path traversal is not allowed")
		}
	}
	return nil
}
func (c *Client) URL(path string) string {
	return c.Base.String() + "/" + strings.TrimPrefix(path, "/")
}
func (c *Client) Get(ctx context.Context, source string, headers http.Header) (*http.Response, error) {
	u, e := url.Parse(source)
	if e != nil {
		return nil, e
	}
	if e = c.Validate(u); e != nil {
		return nil, e
	}
	r, e := http.NewRequestWithContext(ctx, "GET", source, nil)
	if e != nil {
		return nil, e
	}
	r.Header.Set("Accept-Encoding", "identity")
	for _, key := range []string{"Range", "If-Range", "If-None-Match"} {
		if v := headers.Get(key); v != "" {
			r.Header.Set(key, v)
		}
	}
	resp, e := c.HTTP.Do(r)
	if e != nil {
		return nil, errors.New("Upstream connection failed")
	}
	if resp.Uncompressed || (resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity") {
		resp.Body.Close()
		return nil, fmt.Errorf("Unsafe upstream Content-Encoding")
	}
	return resp, nil
}
