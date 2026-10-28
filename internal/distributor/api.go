package distributor

import (
	"context"
	"net/http"
	"net/url"
)

// GetAPI reads an API document of another RedApp instance below the client's
// base, with a query string that RedApp builds itself (never one taken from a
// client request). Redirects must stay without a query, as for files.
func (c *Client) GetAPI(ctx context.Context, relative string, query url.Values) (*http.Response, error) {
	source, err := c.RelativeURL(relative)
	if err != nil {
		return nil, err
	}
	if encoded := query.Encode(); encoded != "" {
		source += "?" + encoded
	}
	return c.Send(ctx, Request{Method: http.MethodGet, URL: source, Header: http.Header{"Accept": {"application/json"}}, query: true})
}
