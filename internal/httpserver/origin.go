package httpserver

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// PublicURL accepts an explicit origin or the empty request-derived mode.
func PublicURL(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") || !validHost(u.Host) {
		return "", errors.New("Public base URL must be a safe HTTP(S) origin; subpaths are not supported")
	}
	return strings.TrimSuffix(s, "/"), nil
}

func validHost(host string) bool {
	if host == "" || len(host) > 320 || strings.ContainsAny(host, "'\"`$\\/@?#% \t\r\n") {
		return false
	}
	u, err := url.Parse("http://" + host)
	if err != nil || u.Host != host {
		return false
	}
	name := u.Hostname()
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	} else if strings.HasSuffix(host, ":") {
		return false
	}
	if strings.HasPrefix(host, "[") {
		return net.ParseIP(name) != nil && strings.Contains(name, ":")
	}
	if strings.Contains(name, ":") || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// origin is request-scoped: it never updates shared state or cached metadata.
func (s *Server) origin(r *http.Request) (string, error) {
	if !validHost(r.Host) {
		return "", errors.New("Invalid request Host")
	}
	if s.Public != "" {
		u, err := url.Parse(s.Public)
		if err != nil || r.Host != u.Host {
			return "", errors.New("Host does not match public base URL")
		}
		return s.Public, nil
	}
	host, proto := r.Host, "http"
	if r.TLS != nil {
		proto = "https"
	}
	peer := parseIP(r.RemoteAddr)
	if s.Proxy.trusted(peer) {
		if values := r.Header.Values("Forwarded"); len(values) > 0 {
			fields, err := forwardedFields(strings.Join(values, ","))
			if err != nil {
				return "", errors.New("Invalid trusted Forwarded header")
			}
			selected := len(fields) - 1
			for i := len(fields) - 1; i >= 0 && s.Proxy.trusted(peer); i-- {
				selected = i
				next := parseIP(fields[i]["for"])
				if next == nil {
					if len(fields) != 1 {
						return "", errors.New("Forwarded chain requires for addresses")
					}
					break
				}
				peer = next
			}
			if v := fields[selected]["host"]; v != "" {
				host = v
			}
			if v := fields[selected]["proto"]; v != "" {
				proto = v
			}
		} else {
			// Single values must be overwritten by the directly trusted proxy. Lists
			// must align with X-Forwarded-For so untrusted prefixes cannot choose origin.
			hops, index := 0, 0
			if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
				parts, err := splitHeader(strings.Join(values, ","), ',')
				if err != nil {
					return "", errors.New("Invalid trusted X-Forwarded-For header")
				}
				ips := make([]net.IP, len(parts))
				for i, v := range parts {
					ips[i] = parseIP(v)
					if ips[i] == nil {
						return "", errors.New("Invalid trusted X-Forwarded-For header")
					}
				}
				hops = len(ips)
				for i := hops - 1; i >= 0 && s.Proxy.trusted(peer); i-- {
					index = i
					peer = ips[i]
				}
			}
			for _, item := range []struct {
				key    string
				target *string
			}{{"X-Forwarded-Host", &host}, {"X-Forwarded-Proto", &proto}} {
				if values := r.Header.Values(item.key); len(values) > 0 {
					parts, err := splitHeader(strings.Join(values, ","), ',')
					if err != nil {
						return "", errors.New("Invalid trusted origin header")
					}
					if len(parts) == 1 {
						*item.target = parts[0]
					} else if hops > 0 && len(parts) == hops {
						*item.target = parts[index]
					} else {
						return "", errors.New("Forwarded origin list must match proxy chain")
					}
				}
			}
		}
	}
	if (proto != "http" && proto != "https") || !validHost(host) {
		return "", errors.New("Invalid request origin")
	}
	return proto + "://" + host, nil
}
