package distributor

import (
	"context"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/store"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type proxyConfig struct {
	Server string `json:"server"`
}

var ErrInvalidProxySettings = errors.New("invalid upstream proxy settings")

func invalidProxy(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidProxySettings, message)
}

// ProxyView holds the exact saved URL, including encoded userinfo. Administrative
// responses use Redacted; the URL must never be included in public output,
// diagnostics, or events.
type ProxyView struct {
	Mode     string `json:"mode"`
	URL      string `json:"url,omitempty"`
	DNS      string `json:"dns"`
	Revision int64  `json:"revision"`
}

// Redacted replaces a saved password with networkproxy.RedactedPassword.
func (v ProxyView) Redacted() ProxyView {
	v.URL = networkproxy.RedactURL(v.URL)
	return v
}

// ProxyUpdate requires an explicit mode. A URL whose password is
// networkproxy.RedactedPassword keeps the saved password of the same proxy.
type ProxyUpdate struct {
	Mode string `json:"mode"`
	URL  string `json:"url,omitempty"`
}

// Pool owns proxy state independently of application instances. Strict public
// clients and configured clients switch proxy transports in one atomic update.
type Pool struct {
	proxyMu       sync.Mutex
	proxyConfig   proxyConfig
	proxyStore    *store.Store
	proxyRevision int64
	transports    atomic.Pointer[transportSet]
	scopes        atomic.Pointer[scopeSnapshot]
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
		if scoped := p.scopes.Load(); scoped != nil {
			for _, tr := range scoped.byURL {
				tr.closeIdle()
			}
		}
	}
}

type transportReference struct {
	pool       *Pool
	configured bool
	appUID     string
	vendorUID  string
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
	if s.current.appUID != "" {
		snapshot := s.current.pool.scopes.Load()
		if snapshot == nil {
			return nil, errors.New("Unknown application transport scope")
		}
		scope, ok := snapshot.scopes[s.current.appUID]
		if !ok || !scope.allowed || scope.vendorUID != s.current.vendorUID {
			return nil, errors.New("Inactive application transport scope")
		}
		transport := scope.transports.public
		if s.current.configured {
			transport = scope.transports.configured
		}
		return transport.RoundTrip(r)
	}
	return s.current.Load().RoundTrip(r)
}

func (c *Pool) LoadProxy(db *store.Store) error {
	snapshot, err := db.DirectoryConfigurationSnapshot()
	if err != nil {
		return errors.New("Failed to read upstream proxy settings")
	}
	plan, err := c.PrepareConfiguration(snapshot)
	if err != nil {
		return err
	}
	plan.Publish()
	c.proxyStore = db
	// Startup/standalone callers receive the same config CAS/publish protocol.
	// Server subsequently replaces this hook with the composite runtime plan.
	db.SetInitialConfigurationPrepare(func(snapshot store.DirectorySnapshot) (store.ConfigurationPublication, error) {
		return c.PrepareConfiguration(snapshot)
	})
	return nil
}
func (c *Pool) Proxy() ProxyView {
	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()
	conf := c.proxyConfig
	mode, dns := "direct", "local"
	if conf.Server != "" {
		mode, dns = "url", "proxy"
	}
	return ProxyView{Mode: mode, URL: conf.Server, DNS: dns, Revision: c.proxyRevision}
}
func (c *Pool) SetProxy(update ProxyUpdate, expected int64) error {
	if c.proxyStore == nil {
		return errors.New("Upstream proxy settings are unavailable")
	}
	conf := networkproxy.Config{Mode: update.Mode, URL: update.URL}
	if err := conf.Validate(false); err != nil {
		return invalidProxy("Invalid proxy settings")
	}
	_, err := c.proxyStore.PatchGlobalProxy(expected, conf)
	if errors.Is(err, store.ErrInvalidDirectory) {
		return invalidProxy("Redacted password requires the saved proxy scheme, username and host")
	}
	return err
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

func transportForMode(conf proxyConfig, configured bool) (*http.Transport, error) {
	tr := &http.Transport{Proxy: nil, DisableCompression: true, MaxIdleConns: 16, MaxConnsPerHost: 16, ResponseHeaderTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, DialContext: directDial}
	if conf.Server == "" {
		if configured {
			tr.DialContext = configuredDial
		}
		return tr, nil
	}
	u, err := networkproxy.ParseURL(conf.Server)
	if err != nil {
		return nil, err
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
	// Requests have no overall deadline, so name resolution is bounded here
	// like the dial itself.
	lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	ips, e := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	cancel()
	if e != nil {
		return nil, e
	}
	if len(ips) == 0 {
		return nil, &net.DNSError{Err: "DNS returned no addresses", Name: host, IsNotFound: true}
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

type scopedTransport struct {
	vendorUID  string
	allowed    bool
	transports *transportSet
}
type scopeSnapshot struct {
	scopes map[string]scopedTransport
	byURL  map[string]*transportSet
}
type ProxyPublication struct {
	pool     *Pool
	next     *scopeSnapshot
	global   *transportSet
	config   networkproxy.Config
	revision int64
	created  []*transportSet
}

func (p *Pool) PrepareConfiguration(snapshot store.DirectorySnapshot) (*ProxyPublication, error) {
	p.proxyMu.Lock()
	result := &ProxyPublication{pool: p, next: &scopeSnapshot{scopes: map[string]scopedTransport{}, byURL: map[string]*transportSet{}}, config: snapshot.GlobalProxy, revision: snapshot.GlobalProxyRevision}
	fail := func(err error) (*ProxyPublication, error) { result.Abort(); return nil, err }
	old := p.scopes.Load()
	obtain := func(c networkproxy.Config) (*transportSet, error) {
		if err := c.Validate(false); err != nil {
			return nil, invalidProxy("Invalid proxy settings")
		}
		key := c.URL
		if tr := result.next.byURL[key]; tr != nil {
			return tr, nil
		}
		if old != nil {
			if tr := old.byURL[key]; tr != nil {
				result.next.byURL[key] = tr
				return tr, nil
			}
		}
		tr, err := transportsFor(proxyConfig{Server: c.URL})
		if err != nil {
			return nil, invalidProxy("Invalid proxy settings")
		}
		result.created = append(result.created, tr)
		result.next.byURL[key] = tr
		return tr, nil
	}
	var err error
	result.global, err = obtain(snapshot.GlobalProxy)
	if err != nil {
		return fail(err)
	}
	for uid, scope := range snapshot.ProxyScopes {
		tr, err := obtain(scope.Proxy.Config)
		if err != nil {
			return fail(err)
		}
		result.next.scopes[uid] = scopedTransport{vendorUID: scope.VendorUID, allowed: scope.Allowed, transports: tr}
	}
	return result, nil
}
func (p *ProxyPublication) Abort() {
	for _, tr := range p.created {
		tr.closeIdle()
	}
	p.pool.proxyMu.Unlock()
}
func (p *ProxyPublication) Publish() {
	c := p.pool
	old := c.scopes.Swap(p.next)
	previousGlobal := c.transports.Swap(p.global)
	c.proxyConfig = proxyConfig{Server: p.config.URL}
	c.proxyRevision = p.revision
	retained := map[*transportSet]bool{}
	for _, tr := range p.next.byURL {
		retained[tr] = true
	}
	if old != nil {
		for _, tr := range old.byURL {
			if !retained[tr] {
				tr.closeIdle()
			}
		}
	}
	if !retained[previousGlobal] {
		previousGlobal.closeIdle()
	}
	c.proxyMu.Unlock()
}
