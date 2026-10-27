package jsonrelease

import (
	"context"
	"net/http"

	"github.com/PMExtra/RedApp/internal/distributor"
)

// NewDiscoverer shares one bounded runner between preview and scheduled work.
// The client retains the application's configured proxy and address boundary.
// Discover supplies a separate URL policy for metadata and checksum requests.
func NewDiscoverer(runner *Runner, client *distributor.Client) Discoverer {
	return Discoverer{Query: runner.Run, Fetch: func(ctx context.Context, source string, allow func(string) bool) (*http.Response, error) {
		role, err := client.WithReleaseSources(allow)
		if err != nil {
			return nil, err
		}
		return role.Get(ctx, source, nil)
	}}
}
