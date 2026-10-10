package distributor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/networkproxy"
)

// ErrAddressNotAllowed reports a destination whose resolved addresses are
// outside the class the request may reach. It is never retryable.
var ErrAddressNotAllowed = errors.New("upstream address is not allowed")

// addressClass is the set of destination addresses a transport may dial.
type addressClass uint8

const (
	// anyAddress is used for the host of an administrator-configured source,
	// which may legitimately be an intranet server.
	anyAddress addressClass = iota
	// publicAddress is used for built-in public release sources and for a
	// redirect that leaves a public configured source.
	publicAddress
	// privateAddress is used for a redirect that leaves a non-public
	// configured source.
	privateAddress
)

func (c addressClass) allows(ip net.IP) bool {
	switch c {
	case publicAddress:
		return publicIP(ip)
	case privateAddress:
		return !publicIP(ip)
	}
	return true
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

// network resolves names and opens TCP connections to addresses the policy
// has already approved. Production uses the system resolver and dialer; package
// tests substitute both to exercise the policy without real DNS.
type network struct {
	lookup func(ctx context.Context, host string) ([]net.IPAddr, error)
	dial   func(ctx context.Context, network, address string) (net.Conn, error)
}

var systemNetwork = network{
	lookup: net.DefaultResolver.LookupIPAddr,
	dial:   (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
}

// resolve bounds name resolution, because requests have no overall deadline.
func (n network) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	addrs, err := n.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, &net.DNSError{Err: "DNS returned no addresses", Name: host, IsNotFound: true}
	}
	ips := make([]net.IP, len(addrs))
	for i, addr := range addrs {
		ips[i] = addr.IP
	}
	return ips, nil
}

// classOf reports whether a source host is public or not. A host resolving to
// both kinds has no single class, so redirects away from it are refused.
func (n network) classOf(ctx context.Context, host string) (addressClass, error) {
	ips, err := n.resolve(ctx, host)
	if err != nil {
		return anyAddress, err
	}
	public := 0
	for _, ip := range ips {
		if publicIP(ip) {
			public++
		}
	}
	switch public {
	case len(ips):
		return publicAddress, nil
	case 0:
		return privateAddress, nil
	}
	return anyAddress, fmt.Errorf("source resolves to public and non-public addresses: %w", ErrAddressNotAllowed)
}

// dialer returns a DialContext that resolves once, refuses the destination if
// any resolved address is outside class, and then dials the checked addresses.
// Dialing the checked IPs, not the name, prevents a second lookup from
// rebinding the connection to an unchecked address.
func (n network) dialer(class addressClass) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := n.resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !class.allows(ip) {
				return nil, ErrAddressNotAllowed
			}
		}
		var last error
		for _, ip := range ips {
			conn, err := n.dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
}

// transportSet holds one proxy setting's transports. Without a proxy each
// transport enforces its address class when dialing; with a proxy all of them
// dial the proxy, which resolves names and decides reachability itself.
type transportSet struct {
	public     *http.Transport // built-in public release sources
	configured *http.Transport // the host of an administrator-configured source
	private    *http.Transport // redirects away from a non-public configured source
	proxied    bool
}

func (s *transportSet) closeIdle() {
	s.public.CloseIdleConnections()
	s.configured.CloseIdleConnections()
	s.private.CloseIdleConnections()
}

func (n network) transports(conf proxyConfig) (*transportSet, error) {
	set := &transportSet{proxied: conf.Server != ""}
	for _, item := range []struct {
		target **http.Transport
		class  addressClass
	}{{&set.public, publicAddress}, {&set.configured, anyAddress}, {&set.private, privateAddress}} {
		tr, err := n.transport(conf, item.class)
		if err != nil {
			return nil, err
		}
		*item.target = tr
	}
	return set, nil
}

func (n network) transport(conf proxyConfig, class addressClass) (*http.Transport, error) {
	tr := &http.Transport{DisableCompression: true, MaxIdleConns: 16, MaxConnsPerHost: 16, ResponseHeaderTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, DialContext: n.dialer(class)}
	if conf.Server == "" {
		return tr, nil
	}
	u, err := networkproxy.ParseURL(conf.Server)
	if err != nil {
		return nil, err
	}
	tr.Proxy = http.ProxyURL(u)
	// net/http's SOCKS5 transport sends the original destination hostname to
	// the proxy. Proxy-side DNS cannot be inspected by this process.
	tr.DialContext = n.dial
	return tr, nil
}

// forRequest selects the transport for one hop of a request from a client
// whose source host is base. A configured source may reach any address, but a
// redirect to a different host may only reach the address class of the source
// itself: a public source can never redirect the server into the intranet, and
// an intranet source never sends the server to the internet.
func (s *transportSet) forRequest(r *http.Request, n network, configured bool, base string) (*http.Transport, error) {
	if !configured {
		return s.public, nil
	}
	if s.proxied || strings.EqualFold(r.URL.Hostname(), base) {
		return s.configured, nil
	}
	class, err := n.classOf(r.Context(), base)
	if err != nil {
		return nil, err
	}
	if class == publicAddress {
		return s.public, nil
	}
	return s.private, nil
}
