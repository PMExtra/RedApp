package distributor

import (
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/store"
	"net/http"
	"sync"
	"sync/atomic"
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
	net           network
}

func NewPool() *Pool { return newPool(systemNetwork) }

func newPool(n network) *Pool {
	p := &Pool{net: n}
	transports, _ := n.transports(proxyConfig{})
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

// set returns the transports of the reference's current proxy setting.
func (r transportReference) set() (*transportSet, error) {
	if r.appUID == "" {
		return r.pool.transports.Load(), nil
	}
	snapshot := r.pool.scopes.Load()
	if snapshot == nil {
		return nil, errors.New("unknown application transport scope")
	}
	scope, ok := snapshot.scopes[r.appUID]
	if !ok || !scope.allowed || scope.vendorUID != r.vendorUID {
		return nil, errors.New("inactive application transport scope")
	}
	return scope.transports, nil
}

// Load is the transport for the source host itself.
func (r transportReference) Load() *http.Transport {
	t := r.pool.transports.Load()
	if r.configured {
		return t.configured
	}
	return t.public
}

// transportSwitch resolves the proxy setting on every hop, so a settings
// change applies to new requests without rebuilding clients. base is the
// configured source host used for the redirect address policy.
type transportSwitch struct {
	current transportReference
	base    string
}

func (s *transportSwitch) RoundTrip(r *http.Request) (*http.Response, error) {
	set, err := s.current.set()
	if err != nil {
		return nil, err
	}
	transport, err := set.forRequest(r, s.current.pool.net, s.current.configured, s.base)
	if err != nil {
		return nil, err
	}
	return transport.RoundTrip(r)
}

func (c *Pool) LoadProxy(db *store.Store) error {
	snapshot, err := db.DirectoryConfigurationSnapshot()
	if err != nil {
		return errors.New("failed to read upstream proxy settings")
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

// SetProxy saves the global proxy under CAS. It returns ErrInvalidProxySettings
// for an invalid setting, also wrapping networkproxy.ErrRedactedMismatch when
// the redacted password is submitted for another proxy, and store.ErrConflict
// when expected is stale.
func (c *Pool) SetProxy(update ProxyUpdate, expected int64) error {
	if c.proxyStore == nil {
		return errors.New("upstream proxy settings are unavailable")
	}
	conf := networkproxy.Config{Mode: update.Mode, URL: update.URL}
	if err := conf.Validate(false); err != nil {
		return invalidProxy("Invalid proxy settings")
	}
	_, err := c.proxyStore.PatchGlobalProxy(expected, conf)
	if errors.Is(err, networkproxy.ErrRedactedMismatch) {
		return fmt.Errorf("%w: %w", ErrInvalidProxySettings, networkproxy.ErrRedactedMismatch)
	}
	if errors.Is(err, store.ErrInvalidDirectory) {
		return invalidProxy("Invalid proxy settings")
	}
	return err
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
		tr, err := p.net.transports(proxyConfig{Server: c.URL})
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
