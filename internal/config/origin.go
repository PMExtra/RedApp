package config

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// PublicURL accepts a safe HTTP(S) origin, or the empty request-derived mode.
func PublicURL(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	if err != nil || strings.ContainsAny(s, "?#") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") || !ValidHost(u.Host) {
		return "", errors.New("public URL must be a safe HTTP(S) origin without credentials, subpath, query, or fragment")
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

func ValidHost(host string) bool {
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
