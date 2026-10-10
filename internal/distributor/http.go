// Package distributor implements a bounded upstream HTTP capability; it knows no application metadata.
package distributor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/identity"
)

// ClientMode separates built-in public release sources from administrator-configured
// sources. It does not change a release provider's metadata or digest validation.
// ErrConnection classifies transport/network failures without exposing the URL
// or proxy credentials. Invalid redirects, encoding and certificates are not
// retryable connection failures.
var ErrConnection = errors.New("Upstream connection failed")

// ErrUnsafeEncoding rejects a response whose representation is not identity.
var ErrUnsafeEncoding = errors.New("Unsafe upstream Content-Encoding")

// ErrorKind classifies an upstream failure for diagnostics only.
type ErrorKind uint8

const (
	KindNetwork ErrorKind = iota
	KindDNS
	KindTLS
	KindTimeout
	KindRedirect
)

// RequestError keeps a stable public message without exposing the URL or
// proxy credentials, while retaining the failure class. It matches
// ErrConnection only for retryable transport failures.
type RequestError struct {
	Kind      ErrorKind
	retryable bool
	message   string
	cause     error // an exported sentinel callers may match, never a URL-bearing error
}

func (e *RequestError) Error() string {
	if e.message != "" {
		return e.message
	}
	return ErrConnection.Error()
}
func (e *RequestError) Is(target error) bool { return target == ErrConnection && e.retryable }
func (e *RequestError) Unwrap() error        { return e.cause }

// Classify reports the failure class of a request error or of a raw
// transport/body read error.
func Classify(err error) ErrorKind {
	var request *RequestError
	if errors.As(err, &request) {
		return request.Kind
	}
	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalidCertificate x509.CertificateInvalidError
	var record tls.RecordHeaderError
	var alert tls.AlertError
	var network net.Error
	switch {
	case errors.As(err, &dns):
		return KindDNS
	case errors.As(err, &certificate) || errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalidCertificate) || errors.As(err, &record) || errors.As(err, &alert):
		return KindTLS
	case errors.As(err, &network) && network.Timeout():
		return KindTimeout
	}
	return KindNetwork
}

type redirectPolicyError struct{ error }

type ClientMode uint8

const (
	PublicRelease ClientMode = iota
	ConfiguredRelease
	GeneralHTTP
)

type Client struct {
	Base       *url.URL
	HTTP       *http.Client
	mode       ClientMode
	pool       *Pool
	transports *transportSwitch
}

// NormalizeBase validates a directory-like base without fetching it. Configured
// sources support HTTP, private DNS and explicit ports; TLS verification is unchanged.
func NormalizeBase(base string, mode ClientMode) (string, error) {
	if mode > GeneralHTTP {
		return "", errors.New("Invalid upstream client mode")
	}
	u, err := url.Parse(base)
	if err != nil || u == nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.Hostname() == "" || strings.ContainsAny(base, "\r\n\t#") {
		return "", errors.New("BASE_URL must be an HTTP(S) base URL without query, fragment, or credentials")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || mode == PublicRelease) {
		return "", errors.New("Invalid upstream URL scheme")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || mode == PublicRelease {
			return "", errors.New("Invalid upstream URL port")
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return "", errors.New("Invalid upstream URL port")
	}
	if err := validatePath(u); err != nil {
		return "", err
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

func New(base string) (*Client, error) { return NewPool().NewClient(base, PublicRelease) }

// NewClient creates a fixed initial origin/root boundary sharing this pool's proxy.
// Its construction performs no network I/O.
func (p *Pool) NewClient(base string, mode ClientMode) (*Client, error) {
	return p.newClient(base, mode, "", "")
}
func (p *Pool) NewScopedClient(base string, mode ClientMode, appUID, vendorUID string) (*Client, error) {
	if !identity.ValidUID(appUID) || !identity.ValidUID(vendorUID) {
		return nil, errors.New("Application transport scope required")
	}
	return p.newClient(base, mode, appUID, vendorUID)
}
func (p *Pool) newClient(base string, mode ClientMode, appUID, vendorUID string) (*Client, error) {
	base, err := NormalizeBase(base, mode)
	if err != nil {
		return nil, err
	}
	if p == nil || p.transports.Load() == nil {
		return nil, errors.New("Upstream transport is unavailable")
	}
	u, _ := url.Parse(base)
	c := &Client{Base: u, mode: mode, pool: p}
	c.transports = &transportSwitch{current: transportReference{pool: p, configured: mode != PublicRelease, appUID: appUID, vendorUID: vendorUID}, base: u.Hostname()}
	// No Client.Timeout: it would bound whole body streaming. The transport
	// bounds dial/TLS/response headers and Do bounds idle body reads.
	c.HTTP = &http.Client{Transport: c.transports, CheckRedirect: c.checkRedirect}
	return c, nil
}

func validatePath(u *url.URL) error {
	if !utf8.ValidString(u.Path) || strings.ContainsAny(u.Path, "\\%") || strings.Contains(u.Path, "//") {
		return errors.New("Invalid upstream path")
	}
	for _, ch := range u.Path {
		if unicode.IsControl(ch) {
			return errors.New("Invalid upstream path")
		}
	}
	for _, segment := range strings.Split(u.EscapedPath(), "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil || strings.ContainsAny(decoded, "/\\") || decoded == "." || decoded == ".." {
			return errors.New("Upstream path traversal is not allowed")
		}
	}
	return nil
}

func sameOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if u.Port() != "" {
			return u.Port()
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return a.Scheme == b.Scheme && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}

func (c *Client) Validate(u *url.URL) error {
	if c.Base == nil || u == nil || u.Opaque != "" || !sameOrigin(u, c.Base) || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery {
		return errors.New("Only the fixed upstream without query parameters is allowed")
	}
	if err := validatePath(u); err != nil {
		return err
	}
	if !strings.HasPrefix(u.Path, c.Base.Path+"/") {
		return errors.New("Invalid upstream path")
	}
	return nil
}

// RelativeURL accepts a decoded relative file path, escaping each legal path
// segment once. It never resolves dot segments or accepts an absolute URL.
func (c *Client) RelativeURL(path string) (string, error) {
	if c.Base == nil || path == "" || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
		return "", errors.New("Invalid relative upstream path")
	}
	u := *c.Base
	u.Path += "/" + path
	u.RawPath = ""
	if err := c.Validate(&u); err != nil {
		return "", err
	}
	return u.String(), nil
}

func (c *Client) checkRedirect(r *http.Request, via []*http.Request) error {
	if len(via) > 4 {
		return errors.New("Too many redirects")
	}
	if len(via) == 0 {
		return c.Validate(r.URL)
	}
	if c.mode == GeneralHTTP {
		// A validator/range describes the original resource, not the redirect
		// target. Strip it even for same-origin redirects or loops back home.
		for _, header := range []string{"If-None-Match", "If-Modified-Since", "If-Match", "If-Unmodified-Since", "If-Range", "Range"} {
			r.Header.Del(header)
		}
	}
	previous := via[len(via)-1].URL
	if !sameOrigin(previous, r.URL) || !sameOrigin(c.Base, r.URL) {
		// Never carry credentials across origins, even for parent/subdomains or
		// a chain that eventually returns to the initial source.
		for _, header := range []string{"Authorization", "Proxy-Authorization", "Cookie", "Cookie2", "Referer"} {
			r.Header.Del(header)
		}
	}
	if previous.Scheme == "https" && r.URL.Scheme != "https" {
		return errors.New("Upstream HTTPS downgrade is not allowed")
	}
	if c.mode != GeneralHTTP || sameOrigin(r.URL, c.Base) {
		return c.Validate(r.URL)
	}
	// An arbitrary initial URL remains forbidden. Only upstream redirects may
	// change origin, and an origin-changing hop must arrive over HTTPS. An
	// HTTP source can upgrade to HTTPS, but HTTPS can never downgrade. Which
	// addresses the new host may resolve to is decided when the hop is dialed
	// (transportSet.forRequest), because only then are the addresses known.
	if r.URL.Scheme != "https" {
		return errors.New("Cross-origin upstream redirects require HTTPS")
	}
	if _, err := NormalizeBase(r.URL.String(), GeneralHTTP); err != nil {
		return err
	}
	return nil
}

// Request is one upstream GET or HEAD inside a client's fixed source.
type Request struct {
	Method string
	URL    string
	// Header supplies representation validators and byte ranges. Cookies,
	// credentials and request-controlled destination headers are never sent.
	Header http.Header
	// IdleTimeout bounds one response body read that receives no bytes; zero
	// means DefaultIdleTimeout. The body has no overall deadline.
	IdleTimeout time.Duration
	// directory keeps every redirect hop inside the configured root (Index).
	directory bool
}

func (c *Client) Get(ctx context.Context, source string, headers http.Header) (*http.Response, error) {
	return c.Send(ctx, Request{Method: http.MethodGet, URL: source, Header: headers})
}

func (c *Client) Head(ctx context.Context, source string, headers http.Header) (*http.Response, error) {
	return c.Send(ctx, Request{Method: http.MethodHead, URL: source, Header: headers})
}

// Send performs one request. The returned body owns the request context: it is
// cancelled on Close or after an idle read.
func (c *Client) Send(ctx context.Context, req Request) (*http.Response, error) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return nil, errors.New("only GET and HEAD upstream requests are supported")
	}
	u, err := url.Parse(req.URL)
	if err != nil {
		return nil, fmt.Errorf("parse upstream URL: %w", err)
	}
	if err = c.Validate(u); err != nil {
		return nil, err
	}
	// There is no overall client deadline: the returned body owns this
	// request context and cancels it on Close or after an idle read.
	requestCtx, cancel := context.WithCancel(ctx)
	owned := false
	defer func() {
		if !owned {
			cancel()
		}
	}()
	r, err := http.NewRequestWithContext(requestCtx, req.Method, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	r.Header.Set("Accept-Encoding", "identity")
	if req.directory {
		if !c.IndexBoundary(u) {
			return nil, errIndexBoundary
		}
		r.Header.Set("Accept", "application/json,text/html")
	}
	for _, key := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since", "If-Match", "If-Unmodified-Since"} {
		if v := req.Header.Get(key); v != "" {
			r.Header.Set(key, v)
		}
	}
	if c.HTTP == nil {
		return nil, errors.New("Upstream HTTP client is unavailable")
	}
	// Retain injected transports/timeouts while enforcing the same redirect
	// boundary for production clients and fixture clients alike.
	httpClient := *c.HTTP
	httpClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if req.directory && !c.IndexBoundary(request.URL) {
			return errIndexBoundary
		}
		if err := c.checkRedirect(request, via); err != nil {
			return &redirectPolicyError{err}
		}
		return nil
	}
	httpClient.Jar = nil
	resp, err := httpClient.Do(r)
	if err != nil {
		var redirect *redirectPolicyError
		var certificate *tls.CertificateVerificationError
		var unknownAuthority x509.UnknownAuthorityError
		var hostname x509.HostnameError
		var invalidCertificate x509.CertificateInvalidError
		if errors.As(err, &redirect) {
			return nil, &RequestError{Kind: KindRedirect}
		}
		if errors.Is(err, ErrAddressNotAllowed) {
			return nil, &RequestError{Kind: KindNetwork, message: "Upstream address is not allowed", cause: ErrAddressNotAllowed}
		}
		if errors.As(err, &certificate) || errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalidCertificate) {
			return nil, &RequestError{Kind: KindTLS}
		}
		networkErr := err
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			networkErr = urlErr.Err
		}
		var network net.Error
		retryable := errors.As(networkErr, &network) || errors.Is(networkErr, io.EOF) || errors.Is(networkErr, io.ErrUnexpectedEOF)
		return nil, &RequestError{Kind: Classify(networkErr), retryable: retryable}
	}
	unsafeEncoding := resp.Uncompressed
	for _, value := range resp.Header.Values("Content-Encoding") {
		for _, encoding := range strings.Split(value, ",") {
			if strings.TrimSpace(encoding) != "" && !strings.EqualFold(strings.TrimSpace(encoding), "identity") {
				unsafeEncoding = true
			}
		}
	}
	if unsafeEncoding {
		resp.Body.Close()
		return nil, ErrUnsafeEncoding
	}
	idle := req.IdleTimeout
	if idle <= 0 {
		idle = DefaultIdleTimeout
	}
	watchBody(resp, idle, cancel)
	owned = true
	return resp, nil
}
