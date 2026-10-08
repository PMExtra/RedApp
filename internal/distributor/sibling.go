package distributor

import "errors"

// Sibling keeps the strict public release boundary and shares the proxy owner.
func (c *Client) Sibling(base string) (*Client, error) {
	return c.sibling(base, PublicRelease)
}

func (c *Client) ConfiguredSibling(base string) (*Client, error) {
	return c.sibling(base, ConfiguredRelease)
}

func (c *Client) GeneralSibling(base string) (*Client, error) {
	return c.sibling(base, GeneralHTTP)
}

func (c *Client) sibling(base string, mode ClientMode) (*Client, error) {
	if c.pool == nil {
		return nil, errors.New("Upstream transport is not configurable")
	}
	scope := c.transports.current
	if scope.appUID != "" {
		return c.pool.NewScopedClient(base, mode, scope.appUID, scope.vendorUID)
	}
	return c.pool.NewClient(base, mode)
}
