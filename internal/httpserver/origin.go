package httpserver

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/PMExtra/RedApp/internal/config"
)

func validHost(s string) bool { return config.ValidHost(s) }

// origin is request-scoped: it never updates shared state or cached metadata.
func (p TrustedProxies) origin(r *http.Request) (string, error) {
	if !validHost(r.Host) {
		return "", errors.New("invalid request Host")
	}
	host, proto := r.Host, "http"
	if r.TLS != nil {
		proto = "https"
	}
	peer := parseIP(r.RemoteAddr)
	if p.trusted(peer) {
		if values := r.Header.Values("Forwarded"); len(values) > 0 {
			fields, err := forwardedFields(strings.Join(values, ","))
			if err != nil {
				return "", errors.New("invalid trusted Forwarded header")
			}
			selected := len(fields) - 1
			for i := len(fields) - 1; i >= 0 && p.trusted(peer); i-- {
				selected = i
				next := parseIP(fields[i]["for"])
				if next == nil {
					if len(fields) != 1 {
						return "", errors.New("forwarded chain requires for addresses")
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
					return "", errors.New("invalid trusted X-Forwarded-For header")
				}
				ips := make([]net.IP, len(parts))
				for i, v := range parts {
					ips[i] = parseIP(v)
					if ips[i] == nil {
						return "", errors.New("invalid trusted X-Forwarded-For header")
					}
				}
				hops = len(ips)
				for i := hops - 1; i >= 0 && p.trusted(peer); i-- {
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
						return "", errors.New("invalid trusted origin header")
					}
					if len(parts) == 1 {
						*item.target = parts[0]
					} else if hops > 0 && len(parts) == hops {
						*item.target = parts[index]
					} else {
						return "", errors.New("forwarded origin list must match proxy chain")
					}
				}
			}
		}
	}
	if (proto != "http" && proto != "https") || !validHost(host) {
		return "", errors.New("invalid request origin")
	}
	return proto + "://" + host, nil
}
