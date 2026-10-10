package distributor

import (
	"net"
	"net/url"
	"testing"
)

func TestFixedOriginAndPaths(t *testing.T) {
	c, e := New("https://releases.openai.com/codex")
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"https://evil.example/codex/a", "http://releases.openai.com/codex/a", "https://releases.openai.com/codex/../secret", "https://releases.openai.com/codex/%2e%2e/secret", "https://releases.openai.com/codex/a?url=https://evil.example", "https://releases.openai.com/codex//a", "https://user@releases.openai.com/codex/a"} {
		u, _ := url.Parse(s)
		if c.Validate(u) == nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "::1", "fc00::1", "0.0.0.0"} {
		if publicIP(net.ParseIP(s)) {
			t.Fatal(s)
		}
	}
	u, _ := url.Parse("https://releases.openai.com/codex/releases/0.159.2/asset")
	if e = c.Validate(u); e != nil {
		t.Fatal(e)
	}
}

// sourceURL builds a fixture URL and panics on an invalid path.
func sourceURL(c *Client, path string) string {
	u, err := c.RelativeURL(path)
	if err != nil {
		panic(err)
	}
	return u
}
