package httpserver

import (
	"errors"
	"net"
	"net/http"
	"strings"
)

type Proxy struct{ Networks []*net.IPNet }

func NewProxy(cidrs string) (Proxy, error) {
	p := Proxy{}
	if cidrs == "" {
		return p, nil
	}
	for _, s := range strings.Split(cidrs, ",") {
		_, n, e := net.ParseCIDR(strings.TrimSpace(s))
		if e != nil {
			return p, e
		}
		p.Networks = append(p.Networks, n)
	}
	return p, nil
}
func (p Proxy) trusted(ip net.IP) bool {
	for _, n := range p.Networks {
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
		return nil, errors.New("头部超限")
	}
	parts := []string{}
	quoted, escaped := false, false
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\r' || c == '\n' {
			return nil, errors.New("无效换行")
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
		return nil, errors.New("无效引号")
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	if len(parts) > 32 {
		return nil, errors.New("代理跳数超限")
	}
	return parts, nil
}
func forwarded(s string) ([]net.IP, error) {
	elements, e := splitHeader(s, ',')
	if e != nil {
		return nil, e
	}
	ips := []net.IP{}
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
				return nil, errors.New("无效 Forwarded 参数")
			}
			if strings.HasPrefix(v, "\"") {
				v, e = unquoteForwarded(v)
				if e != nil {
					return nil, e
				}
			}
			if v == "" {
				return nil, errors.New("空 Forwarded 参数")
			}
			fields[k] = v
		}
		ip := parseIP(fields["for"])
		if ip == nil {
			return nil, errors.New("无效 Forwarded for")
		}
		if proto := fields["proto"]; proto != "" && proto != "http" && proto != "https" {
			return nil, errors.New("无效 proto")
		}
		if h := fields["host"]; strings.ContainsAny(h, "/\\@?# \t") {
			return nil, errors.New("无效 host")
		}
		ips = append(ips, ip)
	}
	return ips, nil
}

// Host and proto are always sourced from configured public URL. Only IP walks the trusted chain.
func (p Proxy) ClientIP(r *http.Request) string {
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
				e = errors.New("无效 XFF")
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
		return "", errors.New("无效引号")
	}
	var out strings.Builder
	for i := 1; i < len(s)-1; i++ {
		c := s[i]
		if c == '\\' {
			i++
			if i >= len(s)-1 {
				return "", errors.New("无效转义")
			}
			c = s[i]
		} else if c == '"' {
			return "", errors.New("无效引号")
		}
		if c < 32 || c == 127 {
			return "", errors.New("无效控制字符")
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
