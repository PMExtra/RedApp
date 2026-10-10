package networkproxy

import "testing"

func TestParseURLAcceptsOnlyExplicitProxyServers(t *testing.T) {
	for _, raw := range []string{
		"http://proxy.example:3128",
		"https://user:secret@proxy.example:443",
		"socks5://127.0.0.1:1080",
	} {
		if _, err := ParseURL(raw); err != nil {
			t.Errorf("%q rejected: %v", raw, err)
		}
	}
	for _, raw := range []string{
		"", "proxy.example:3128", "ftp://proxy.example:21", "http://proxy.example",
		"http://proxy.example:0", "http://proxy.example:3128/path", "http://proxy.example:3128?x=1",
		"http://proxy.example:3128#f", "http://proxy.example:3128 ", "http://:3128",
		"http://bad\x00user:p@proxy.example:3128",
	} {
		if _, err := ParseURL(raw); err == nil {
			t.Errorf("%q accepted", raw)
		}
	}
}

func TestConfigValidateUsesTheSameURLRules(t *testing.T) {
	cases := []struct {
		config       Config
		allowInherit bool
		valid        bool
	}{
		{Inherit(), true, true},
		{Inherit(), false, false},
		{Direct(), false, true},
		{Config{Mode: "direct", URL: "http://proxy.example:3128"}, false, false},
		{Config{Mode: "url", URL: "http://proxy.example:3128"}, false, true},
		{Config{Mode: "url", URL: "http://proxy.example"}, false, false},
		{Config{Mode: "url"}, false, false},
	}
	for _, c := range cases {
		if err := c.config.Validate(c.allowInherit); (err == nil) != c.valid {
			t.Errorf("%+v allowInherit=%v: %v", c.config, c.allowInherit, err)
		}
	}
}
