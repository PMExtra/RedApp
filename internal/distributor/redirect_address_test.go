package distributor

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeNetwork resolves fixture names to chosen addresses and connects every
// approved address to one local server, recording which addresses were dialed.
type fakeNetwork struct {
	hosts  map[string][]string
	server string
	mu     sync.Mutex
	dialed []string
}

func (f *fakeNetwork) network() network {
	return network{
		lookup: func(_ context.Context, host string) ([]net.IPAddr, error) {
			var out []net.IPAddr
			for _, ip := range f.hosts[host] {
				out = append(out, net.IPAddr{IP: net.ParseIP(ip)})
			}
			if out == nil {
				return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
			}
			return out, nil
		},
		dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(address)
			f.mu.Lock()
			f.dialed = append(f.dialed, host)
			f.mu.Unlock()
			return (&net.Dialer{}).DialContext(ctx, network, f.server)
		},
	}
}

func (f *fakeNetwork) reset() {
	f.mu.Lock()
	f.dialed = nil
	f.mu.Unlock()
}

func (f *fakeNetwork) wasDialed(ip string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, dialed := range f.dialed {
		if dialed == ip {
			return true
		}
	}
	return false
}

func newFakeNetwork(t *testing.T, h http.Handler) *fakeNetwork {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	return &fakeNetwork{server: server.Listener.Addr().String(), hosts: map[string][]string{
		"public.test":    {"203.0.113.10"},
		"cdn.test":       {"203.0.113.20"},
		"intranet.test":  {"10.0.0.1"},
		"mirror.test":    {"10.0.0.2"},
		"internal.test":  {"10.0.0.5"},
		"rebinding.test": {"203.0.113.30", "10.0.0.6"},
		"mixed.test":     {"203.0.113.40", "10.0.0.7"},
	}}
}

func TestRedirectHopsStayInTheSourceAddressClass(t *testing.T) {
	fake := newFakeNetwork(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	pool := newPool(fake.network())
	for _, tc := range []struct {
		name, source, target, refusedIP string
		allowed                         bool
	}{
		{"public source to public host", "http://public.test/files", "http://cdn.test/file", "", true},
		{"public source to private host", "http://public.test/files", "http://internal.test/file", "10.0.0.5", false},
		{"public source to private literal", "http://public.test/files", "http://10.0.0.5/file", "10.0.0.5", false},
		{"public source to rebinding host", "http://public.test/files", "http://rebinding.test/file", "10.0.0.6", false},
		{"private source to private host", "http://intranet.test/files", "http://mirror.test/file", "", true},
		{"private source to public host", "http://intranet.test/files", "http://cdn.test/file", "203.0.113.20", false},
		{"private source to its own host", "http://intranet.test/files", "http://intranet.test/other", "", true},
		{"mixed source to another host", "http://mixed.test/files", "http://cdn.test/file", "203.0.113.20", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake.reset()
			client, err := pool.NewClient(tc.source, GeneralHTTP)
			if err != nil {
				t.Fatal(err)
			}
			// Exercise one hop of the transport directly: the redirect checks in
			// checkRedirect are a separate layer and are covered elsewhere.
			req, _ := http.NewRequest(http.MethodGet, tc.target, nil)
			resp, err := client.HTTP.Transport.RoundTrip(req)
			if tc.allowed {
				if err != nil {
					t.Fatalf("hop refused: %v", err)
				}
				resp.Body.Close()
				return
			}
			if resp != nil {
				resp.Body.Close()
			}
			if err == nil {
				t.Fatal("hop allowed")
			}
			if !errors.Is(err, ErrAddressNotAllowed) {
				t.Fatalf("hop failed for another reason: %v", err)
			}
			if fake.wasDialed(tc.refusedIP) {
				t.Fatalf("refused address %s was dialed", tc.refusedIP)
			}
		})
	}
}

func TestPublicSourceCannotRedirectIntoTheIntranet(t *testing.T) {
	var fake *fakeNetwork
	fake = newFakeNetwork(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "public.test" {
			http.Redirect(w, r, "https://internal.test/admin", http.StatusFound)
			return
		}
		t.Errorf("request reached %s", r.Host)
	}))
	client, err := newPool(fake.network()).NewClient("http://public.test/files", GeneralHTTP)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(context.Background(), sourceURL(client, "file"), nil)
	if err == nil {
		resp.Body.Close()
		t.Fatal("redirect into a private address was followed")
	}
	if !errors.Is(err, ErrAddressNotAllowed) || errors.Is(err, ErrConnection) {
		t.Fatalf("refusal is not a final address-policy error: %v", err)
	}
	if fake.wasDialed("10.0.0.5") {
		t.Fatal("private redirect target was dialed")
	}
}
