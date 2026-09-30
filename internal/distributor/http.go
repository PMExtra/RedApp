// Package distributor implements a fixed upstream HTTP boundary; it knows no application metadata.
package distributor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	Base *url.URL
	HTTP *http.Client
}

func New(base string) (*Client, error) {
	u, e := url.Parse(base)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" || u.Port() != "" {
		return nil, errors.New("BASE_URL 必须是无端口、查询和凭据的 HTTPS 根地址")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	c := &Client{Base: u}
	tr := &http.Transport{Proxy: nil, DisableCompression: true, MaxIdleConns: 16, MaxConnsPerHost: 16, ResponseHeaderTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(addr)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, e
		}
		if len(ips) == 0 {
			return nil, errors.New("DNS 无地址")
		}
		for _, ip := range ips {
			if !publicIP(ip.IP) {
				return nil, errors.New("上游 DNS 指向非公开地址")
			}
		}
		var last error
		for _, ip := range ips {
			conn, e := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		return nil, last
	}
	c.HTTP = &http.Client{Transport: tr, Timeout: 5 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return errors.New("过多重定向")
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
		return errors.New("禁止非固定上游或带查询的地址")
	}
	if u.RawPath != "" || strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "//") || !strings.HasPrefix(u.Path, c.Base.Path+"/") {
		return errors.New("禁止异常上游路径")
	}
	for _, p := range strings.Split(u.Path, "/") {
		if p == ".." || p == "." {
			return errors.New("禁止上游路径遍历")
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
		return nil, errors.New("上游连接失败")
	}
	if resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity" {
		resp.Body.Close()
		return nil, fmt.Errorf("上游 Content-Encoding 不安全")
	}
	return resp, nil
}
