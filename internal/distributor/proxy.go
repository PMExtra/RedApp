package distributor

import (
	"context"
	"database/sql"
	"errors"
	"github.com/PMExtra/RedApp/internal/store"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type proxyConfig struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}
type ProxyView struct {
	Server         string `json:"server"`
	HasCredentials bool   `json:"has_credentials"`
	HasPassword    bool   `json:"has_password"`
	DNS            string `json:"dns"`
}
type ProxyUpdate struct {
	Server         string `json:"server"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	PasswordAction string `json:"password_action"`
}

type transportSwitch struct {
	current atomic.Pointer[http.Transport]
}

func (s *transportSwitch) RoundTrip(r *http.Request) (*http.Response, error) {
	return s.current.Load().RoundTrip(r)
}

func (c *Client) LoadProxy(db *store.Store) error {
	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()
	var conf proxyConfig
	if err := db.Get("settings", "upstream_proxy", &conf); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("Failed to read upstream proxy settings")
	}
	tr, err := transportFor(conf)
	if err != nil {
		return err
	}
	if c.transports == nil {
		return errors.New("Upstream transport is not configurable")
	}
	old := c.transports.current.Swap(tr)
	old.CloseIdleConnections()
	c.proxyStore = db
	c.proxyConfig = conf
	return nil
}
func (c *Client) Proxy() ProxyView {
	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()
	conf := c.proxyConfig
	dns := "local"
	if conf.Server != "" {
		dns = "proxy"
	}
	return ProxyView{Server: conf.Server, HasCredentials: conf.Username != "" || conf.Password != "", HasPassword: conf.Password != "", DNS: dns}
}
func (c *Client) SetProxy(update ProxyUpdate) error {
	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()
	if c.proxyStore == nil || c.transports == nil {
		return errors.New("Upstream proxy settings are unavailable")
	}
	conf := proxyConfig{Server: update.Server, Username: update.Username}
	switch update.PasswordAction {
	case "keep":
		if update.Password != "" || update.Username != "" {
			return errors.New("Credentials must be empty when keeping saved credentials")
		}
		if conf.Server != c.proxyConfig.Server && (c.proxyConfig.Username != "" || c.proxyConfig.Password != "") {
			return errors.New("Clear or replace credentials when changing the proxy server")
		}
		conf.Username = c.proxyConfig.Username
		conf.Password = c.proxyConfig.Password
	case "replace":
		conf.Password = update.Password
	case "clear":
		conf.Username = ""
		if update.Password != "" {
			return errors.New("Password must be empty when clearing saved credentials")
		}
	default:
		return errors.New("Choose keep, replace, or clear for the proxy password")
	}
	if conf.Server == "" {
		conf.Username = ""
		conf.Password = ""
	}
	tr, err := transportFor(conf)
	if err != nil {
		return err
	}
	if err = c.proxyStore.Put("settings", "upstream_proxy", conf); err != nil {
		return errors.New("Failed to persist upstream proxy settings")
	}
	c.proxyConfig = conf
	old := c.transports.current.Swap(tr)
	old.CloseIdleConnections()
	return nil
}
func transportFor(conf proxyConfig) (*http.Transport, error) {
	tr := &http.Transport{Proxy: nil, DisableCompression: true, MaxIdleConns: 16, MaxConnsPerHost: 16, ResponseHeaderTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, DialContext: directDial}
	if conf.Server == "" {
		return tr, nil
	}
	u, err := url.Parse(conf.Server)
	if err != nil || u.User != nil || u.Hostname() == "" || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") || strings.ContainsAny(conf.Server, "\r\n\t ") {
		return nil, errors.New("Proxy server must be an HTTP, HTTPS, or SOCKS5 URL without credentials, path, query, or fragment")
	}
	port, portErr := strconv.Atoi(u.Port())
	if portErr != nil || port < 1 || port > 65535 {
		return nil, errors.New("Proxy server requires an explicit port")
	}
	if len(conf.Username) > 255 || len(conf.Password) > 255 || strings.ContainsAny(conf.Username+conf.Password, "\r\n\x00") {
		return nil, errors.New("Invalid proxy credentials")
	}
	if conf.Username != "" || conf.Password != "" {
		u.User = url.UserPassword(conf.Username, conf.Password)
	}
	tr.Proxy = http.ProxyURL(u)
	tr.DialContext = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	// net/http's SOCKS5 transport sends the original destination hostname to
	// the proxy. Proxy-side DNS cannot be inspected by this process.
	return tr, nil
}

func directDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, e := net.SplitHostPort(addr)
	if e != nil {
		return nil, e
	}
	ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
	if e != nil {
		return nil, e
	}
	if len(ips) == 0 {
		return nil, errors.New("DNS returned no addresses")
	}
	for _, ip := range ips {
		if !publicIP(ip.IP) {
			return nil, errors.New("Upstream DNS resolves to a nonpublic address")
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
