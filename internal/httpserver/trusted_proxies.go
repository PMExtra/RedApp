package httpserver

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// TrustedProxies are the reverse proxies (CIDRs from the deployment
// configuration) whose forwarding headers decide the request origin and the
// client IP. Requests from other peers never have their headers trusted.
type TrustedProxies struct{ networks []*net.IPNet }

// ParseTrustedProxies parses CIDR notations such as "10.0.0.0/8".
func ParseTrustedProxies(cidrs []string) (TrustedProxies, error) {
	p := TrustedProxies{}
	for _, s := range cidrs {
		_, n, e := net.ParseCIDR(strings.TrimSpace(s))
		if e != nil {
			return p, fmt.Errorf("parse trusted proxy %q: %w", s, e)
		}
		p.networks = append(p.networks, n)
	}
	return p, nil
}
func (p TrustedProxies) trusted(ip net.IP) bool {
	for _, n := range p.networks {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
func parseIP(s string) net.IP {
	s = strings.TrimSpace(s)
	if ip := net.ParseIP(s); ip != nil {
		return ip
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		return net.ParseIP(s[1 : len(s)-1])
	}
	host, _, e := net.SplitHostPort(s)
	if e == nil {
		return net.ParseIP(host)
	}
	return nil
}
func splitHeader(s string, delimiter byte) ([]string, error) {
	if len(s) > 8192 {
		return nil, errors.New("header limit exceeded")
	}
	parts := []string{}
	quoted, escaped := false, false
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\r' || c == '\n' {
			return nil, errors.New("invalid newline")
		}
		if escaped {
			escaped = false
			continue
		}
		if quoted && c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			quoted = !quoted
		}
		if c == delimiter && !quoted {
			parts = append(parts, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	if quoted || escaped {
		return nil, errors.New("invalid quoting")
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	if len(parts) > 32 {
		return nil, errors.New("proxy hop limit exceeded")
	}
	return parts, nil
}
func forwardedFields(s string) ([]map[string]string, error) {
	elements, e := splitHeader(s, ',')
	if e != nil {
		return nil, e
	}
	fieldsList := []map[string]string{}
	for _, element := range elements {
		params, e := splitHeader(element, ';')
		if e != nil {
			return nil, e
		}
		fields := map[string]string{}
		for _, param := range params {
			k, v, ok := strings.Cut(param, "=")
			k = strings.ToLower(strings.TrimSpace(k))
			v = strings.TrimSpace(v)
			_, duplicate := fields[k]
			if !ok || duplicate || !headerToken(k) {
				return nil, errors.New("invalid Forwarded parameter")
			}
			if strings.HasPrefix(v, "\"") {
				v, e = unquoteForwarded(v)
				if e != nil {
					return nil, e
				}
			}
			if v == "" {
				return nil, errors.New("empty Forwarded parameter")
			}
			fields[k] = v
		}
		if fields["for"] != "" && parseIP(fields["for"]) == nil {
			return nil, errors.New("invalid Forwarded for")
		}
		if proto := fields["proto"]; proto != "" && proto != "http" && proto != "https" {
			return nil, errors.New("invalid proto")
		}
		if h := fields["host"]; strings.ContainsAny(h, "/\\@?# \t") {
			return nil, errors.New("invalid host")
		}
		fieldsList = append(fieldsList, fields)
	}
	return fieldsList, nil
}

func forwarded(s string) ([]net.IP, error) {
	fields, err := forwardedFields(s)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(fields))
	for _, f := range fields {
		ip := parseIP(f["for"])
		if ip == nil {
			return nil, errors.New("invalid Forwarded for")
		}
		ips = append(ips, ip)
	}
	return ips, nil
}

// ClientIP walks from the directly connected peer to the first untrusted hop.
func (p TrustedProxies) ClientIP(r *http.Request) string {
	peer := parseIP(r.RemoteAddr)
	if peer == nil {
		return "unknown"
	}
	if !p.trusted(peer) {
		return peer.String()
	}
	var ips []net.IP
	var e error
	if h := r.Header.Values("Forwarded"); len(h) > 0 {
		ips, e = forwarded(strings.Join(h, ","))
	} else if h := r.Header.Values("X-Forwarded-For"); len(h) > 0 {
		var parts []string
		parts, e = splitHeader(strings.Join(h, ","), ',')
		for _, s := range parts {
			ip := parseIP(s)
			if ip == nil {
				e = errors.New("invalid X-Forwarded-For")
				break
			}
			ips = append(ips, ip)
		}
	}
	if e != nil {
		return peer.String()
	}
	for i := len(ips) - 1; i >= 0 && p.trusted(peer); i-- {
		peer = ips[i]
	}
	return peer.String()
}

// RFC quoted-pair removes the backslash; Go string escapes must not reinterpret IPs.
func unquoteForwarded(s string) (string, error) {
	if len(s) < 2 || s[len(s)-1] != '"' {
		return "", errors.New("invalid quoting")
	}
	var out strings.Builder
	for i := 1; i < len(s)-1; i++ {
		c := s[i]
		if c == '\\' {
			i++
			if i >= len(s)-1 {
				return "", errors.New("invalid escape")
			}
			c = s[i]
		} else if c == '"' {
			return "", errors.New("invalid quoting")
		}
		if c < 32 || c == 127 {
			return "", errors.New("invalid control character")
		}
		out.WriteByte(c)
	}
	return out.String(), nil
}
func headerToken(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}
