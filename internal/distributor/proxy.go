package distributor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/store"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type proxyConfig struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

var ErrInvalidProxySettings = errors.New("invalid upstream proxy settings")

func invalidProxy(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidProxySettings, message)
}

type ProxyView struct {
	Server         string `json:"server"`
	HasCredentials bool   `json:"has_credentials"`
	HasPassword    bool   `json:"has_password"`
	DNS            string `json:"dns"`
	Revision       int64  `json:"revision"`
}
type ProxyUpdate struct {
	Server         string `json:"server"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	PasswordAction string `json:"password_action"`
}

// Pool owns proxy state independently of application instances. Strict public
// clients and configured clients switch proxy transports in one atomic update.
type Pool struct {
	proxyMu       sync.Mutex
	proxyConfig   proxyConfig
	proxyStore    *store.Store
	proxyRevision int64
	transports    atomic.Pointer[transportSet]
}

type transportSet struct {
	public     *http.Transport
	configured *http.Transport
}

func (s *transportSet) closeIdle() {
	s.public.CloseIdleConnections()
	s.configured.CloseIdleConnections()
}

func NewPool() *Pool {
	p := &Pool{}
	transports, _ := transportsFor(proxyConfig{})
	p.transports.Store(transports)
	return p
}

// CloseIdleConnections releases the current transports' idle sockets without
// interrupting responses already being served.
func (p *Pool) CloseIdleConnections() {
	if p != nil {
		if transports := p.transports.Load(); transports != nil {
			transports.closeIdle()
		}
	}
}

type transportReference struct {
	pool       *Pool
	configured bool
}

func (r transportReference) Load() *http.Transport {
	t := r.pool.transports.Load()
	if r.configured {
		return t.configured
	}
	return t.public
}

type transportSwitch struct{ current transportReference }

func (s *transportSwitch) RoundTrip(r *http.Request) (*http.Response, error) {
	return s.current.Load().RoundTrip(r)
}

// These delegates retain the original client API. The state belongs to Pool,
// so siblings and a pool with no clients have the same settings behavior.
func (c *Client) LoadProxy(db *store.Store) error {
	if c.pool == nil {
		return errors.New("Upstream transport is not configurable")
	}
	return c.pool.LoadProxy(db)
}
func (c *Client) Proxy() ProxyView {
	if c.pool == nil {
		return ProxyView{DNS: "local"}
	}
	return c.pool.Proxy()
}
func (c *Client) SetProxy(update ProxyUpdate, expected int64) error {
	if c.pool == nil {
		return errors.New("Upstream proxy settings are unavailable")
	}
	return c.pool.SetProxy(update, expected)
}

func (c *Pool) LoadProxy(db *store.Store) error {
	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()
	var conf proxyConfig
	revision, err := db.ReadSetting("global", "", "upstream_proxy", &conf)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("Failed to read upstream proxy settings")
	}
	tr, err := transportsFor(conf)
	if err != nil {
		return err
	}
	if c.transports.Load() == nil {
		return errors.New("Upstream transport is not configurable")
	}
	old := c.transports.Swap(tr)
	old.closeIdle()
	c.proxyStore = db
	c.proxyConfig = conf
	c.proxyRevision = revision
	return nil
}
func (c *Pool) Proxy() ProxyView {
	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()
	conf := c.proxyConfig
	dns := "local"
	if conf.Server != "" {
		dns = "proxy"
	}
	return ProxyView{Server: conf.Server, HasCredentials: conf.Username != "" || conf.Password != "", HasPassword: conf.Password != "", DNS: dns, Revision: c.proxyRevision}
}
func (c *Pool) SetProxy(update ProxyUpdate, expected int64) error {
	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()
	if c.proxyStore == nil || c.transports.Load() == nil {
		return errors.New("Upstream proxy settings are unavailable")
	}
	conf := proxyConfig{Server: update.Server, Username: update.Username}
	switch update.PasswordAction {
	case "keep":
		if update.Password != "" || update.Username != "" {
			return invalidProxy("Credentials must be empty when keeping saved credentials")
		}
		if conf.Server != c.proxyConfig.Server && (c.proxyConfig.Username != "" || c.proxyConfig.Password != "") {
			return invalidProxy("Clear or replace credentials when changing the proxy server")
		}
		conf.Username = c.proxyConfig.Username
		conf.Password = c.proxyConfig.Password
	case "replace":
		conf.Password = update.Password
	case "clear":
		conf.Username = ""
		if update.Password != "" {
			return invalidProxy("Password must be empty when clearing saved credentials")
		}
	default:
		return invalidProxy("Choose keep, replace, or clear for the proxy password")
	}
	if conf.Server == "" {
		conf.Username = ""
		conf.Password = ""
	}
	tr, err := transportsFor(conf)
	if err != nil {
		return invalidProxy(err.Error())
	}
	revision, err := c.proxyStore.CompareAndSwapSetting("global", "", "upstream_proxy", expected, conf)
	if err != nil {
		tr.closeIdle()
		return err
	}
	c.proxyConfig = conf
	c.proxyRevision = revision
	old := c.transports.Swap(tr)
	old.closeIdle()
	return nil
}
func transportsFor(conf proxyConfig) (*transportSet, error) {
	public, err := transportForMode(conf, false)
	if err != nil {
		return nil, err
	}
	configured, err := transportForMode(conf, true)
	if err != nil {
		public.CloseIdleConnections()
		return nil, err
	}
	return &transportSet{public: public, configured: configured}, nil
}

func transportFor(conf proxyConfig) (*http.Transport, error) {
	return transportForMode(conf, false)
}

func transportForMode(conf proxyConfig, configured bool) (*http.Transport, error) {
	tr := &http.Transport{Proxy: nil, DisableCompression: true, MaxIdleConns: 16, MaxConnsPerHost: 16, ResponseHeaderTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, DialContext: directDial}
	if conf.Server == "" {
		if configured {
			tr.DialContext = configuredDial
		}
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
	return resolvedDial(ctx, network, addr, true)
}

func configuredDial(ctx context.Context, network, addr string) (net.Conn, error) {
	return resolvedDial(ctx, network, addr, false)
}

func resolvedDial(ctx context.Context, network, addr string, requirePublic bool) (net.Conn, error) {
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
		if requirePublic && !publicIP(ip.IP) {
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
