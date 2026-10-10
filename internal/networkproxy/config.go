// Package networkproxy defines administrative proxy data, without transports.
package networkproxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type Config struct {
	Mode string `json:"mode"`
	URL  string `json:"url,omitempty"`
}
type Effective struct {
	Config
	SourceScope string `json:"source_scope"`
	SourceID    string `json:"source_id"`
	DNS         string `json:"dns"`
}
type Scope struct {
	VendorUID string
	Proxy     Effective
	Allowed   bool
}

func Inherit() Config { return Config{Mode: "inherit"} }
func Direct() Config  { return Config{Mode: "direct"} }
func (c *Config) UnmarshalJSON(raw []byte) error {
	type plain Config
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode((*plain)(c)); err != nil {
		return errors.New("invalid proxy configuration")
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil || keys == nil {
		return errors.New("invalid proxy configuration")
	}
	if c.Mode != "url" {
		if _, ok := keys["url"]; ok {
			return errors.New("proxy URL only allowed in url mode")
		}
	}
	return c.Validate(true)
}
func (c Config) Validate(allowInherit bool) error {
	if c.Mode == "inherit" && allowInherit && c.URL == "" || c.Mode == "direct" && c.URL == "" {
		return nil
	}
	if c.Mode != "url" || c.URL == "" {
		return errors.New("invalid proxy mode")
	}
	_, err := ParseURL(c.URL)
	return err
}

// ParseURL validates a proxy server URL: http, https or socks5 with a host and
// an explicit port, optional bounded credentials, and no path, query or
// fragment. It is the only proxy URL validation; transports use its result.
func ParseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || len(raw) > 4096 || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") || strings.ContainsAny(raw, "\r\n\t ") {
		return nil, errors.New("invalid proxy URL")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("proxy requires explicit port")
	}
	if u.User != nil {
		password, _ := u.User.Password()
		if len(u.User.Username()) > 255 || len(password) > 255 || strings.ContainsAny(u.User.Username()+password, "\r\n\x00") {
			return nil, errors.New("invalid proxy credentials")
		}
	}
	return u, nil
}

// RedactedPassword replaces saved proxy passwords in administrative responses.
// A write may submit it only to keep the password saved for the same proxy.
const RedactedPassword = "****"

var ErrRedactedMismatch = errors.New("redacted proxy password requires the saved proxy scheme, username and host")

// RedactURL replaces a non-empty password; scheme, username and host stay
// visible. Unparseable values are withheld entirely.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.User == nil {
		return raw
	}
	if password, ok := u.User.Password(); !ok || password == "" {
		return raw
	}
	user := url.User(u.User.Username()).String()
	u.User = nil
	return u.Scheme + "://" + user + ":" + RedactedPassword + "@" + strings.TrimPrefix(u.String(), u.Scheme+"://")
}

// Redacted returns the configuration as shown to administrators.
func (c Config) Redacted() Config {
	c.URL = RedactURL(c.URL)
	return c
}

// KeepRedactedPassword resolves a submitted RedactedPassword to the saved URL.
// A different scheme, username or host is rejected so a saved password is never
// sent to another proxy. Other submissions are returned unchanged.
func KeepRedactedPassword(submitted, saved Config) (Config, error) {
	if submitted.Mode != "url" {
		return submitted, nil
	}
	u, err := url.Parse(submitted.URL)
	if err != nil || u.User == nil {
		return submitted, nil
	}
	if password, ok := u.User.Password(); !ok || password != RedactedPassword {
		return submitted, nil
	}
	if saved.Mode == "url" {
		s, err := url.Parse(saved.URL)
		if err == nil && s.User != nil {
			password, ok := s.User.Password()
			if ok && password != "" && s.Scheme == u.Scheme && s.Host == u.Host && s.User.Username() == u.User.Username() {
				return saved, nil
			}
		}
	}
	return submitted, ErrRedactedMismatch
}

func Resolve(app Config, appID string, vendor Config, vendorID string, global Config) Effective {
	c, scope, id := app, "app", appID
	if c.Mode == "inherit" {
		c, scope, id = vendor, "vendor", vendorID
	}
	if c.Mode == "inherit" {
		c, scope, id = global, "global", ""
	}
	if c.Mode == "inherit" || c.Mode == "" {
		c = Direct()
	}
	dns := "local"
	if c.Mode == "url" {
		dns = "proxy"
	}
	return Effective{Config: c, SourceScope: scope, SourceID: id, DNS: dns}
}

func (e *Effective) UnmarshalJSON(raw []byte) error {
	var value struct {
		Mode        string `json:"mode"`
		URL         string `json:"url"`
		SourceScope string `json:"source_scope"`
		SourceID    string `json:"source_id"`
		DNS         string `json:"dns"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*e = Effective{Config: Config{Mode: value.Mode, URL: value.URL}, SourceScope: value.SourceScope, SourceID: value.SourceID, DNS: value.DNS}
	return nil
}
