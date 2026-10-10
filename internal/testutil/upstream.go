package testutil

import (
	"github.com/PMExtra/RedApp/internal/distributor"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Local HTTP is confined to test construction; production has no insecure upstream switch.
func Upstream(t *testing.T, h http.Handler) (*distributor.Client, *httptest.Server) {
	t.Helper()
	s := httptest.NewServer(h)
	u, _ := url.Parse(s.URL)
	c := &distributor.Client{Base: u}
	c.HTTP = &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error { return c.Validate(r.URL) }}
	t.Cleanup(s.Close)
	return c, s
}

// SourceURL builds the upstream URL of a relative path and panics on an
// invalid path, which in a test is a fixture bug.
func SourceURL(c *distributor.Client, path string) string {
	u, err := c.RelativeURL(path)
	if err != nil {
		panic(err)
	}
	return u
}
