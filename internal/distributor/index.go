package distributor

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Index validates every directory redirect inside this exact configured base.
func (c *Client) Index(ctx context.Context, relative string) (*http.Response, error) {
	if relative == "" {
		source := *c.Base
		source.Path = strings.TrimSuffix(source.Path, "/") + "/"
		source.RawPath = ""
		return c.Send(ctx, Request{Method: http.MethodGet, URL: source.String(), directory: true})
	}
	source, err := c.RelativeURL(strings.TrimSuffix(relative, "/"))
	if err != nil {
		return nil, err
	}
	source += "/"
	return c.Send(ctx, Request{Method: http.MethodGet, URL: source, directory: true})
}
func (c *Client) IndexBoundary(u *url.URL) bool {
	if u == nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Scheme != c.Base.Scheme || !strings.EqualFold(u.Host, c.Base.Host) {
		return false
	}
	raw := strings.ToLower(u.EscapedPath())
	if strings.ContainsAny(u.Path, "\\\x00") || strings.Contains(raw, "%2f") || strings.Contains(raw, "%5c") || strings.Contains(raw, "%25") {
		return false
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return strings.HasPrefix(u.Path, strings.TrimSuffix(c.Base.Path, "/")+"/")
}

var errIndexBoundary = errors.New("directory target outside configured root")
