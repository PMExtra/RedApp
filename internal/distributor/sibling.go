package distributor

import "errors"

// Sibling keeps an independent origin boundary and shares only the atomic proxy transport.
// Configure the proxy on the owning client; all siblings observe the same switch.
func (c *Client) Sibling(base string) (*Client, error) {
	if c.transports == nil {
		return nil, errors.New("Upstream transport is not configurable")
	}
	sibling, err := New(base)
	if err != nil {
		return nil, err
	}
	sibling.transports.current.Load().CloseIdleConnections()
	sibling.transports = c.transports
	sibling.HTTP.Transport = c.transports
	return sibling, nil
}
