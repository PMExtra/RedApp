package distributor

import "errors"

// WithReleaseSources returns an independent client for a compiled release
// provider's frozen role-specific URL policy. It permits query parameters and
// explicit additional origins; it does not replace proxy scope, address-class
// checks, TLS verification, redirect credential stripping or body idle limits.
// Callers must not derive the policy from a public download request.
func (c *Client) WithReleaseSources(allow func(string) bool) (*Client, error) {
	if c == nil || c.Base == nil || c.HTTP == nil || allow == nil {
		return nil, errors.New("release transport and source policy required")
	}
	clone := *c
	base := *c.Base
	clone.Base = &base
	httpClient := *c.HTTP
	httpClient.Jar = nil
	clone.HTTP = &httpClient
	clone.releaseSource = allow
	return &clone, nil
}
