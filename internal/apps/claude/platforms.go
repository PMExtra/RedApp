package claude

import (
	"fmt"
	"github.com/PMExtra/RedApp/internal/application"
)

func (p *Protocol) Platforms() []application.Platform {
	out := []application.Platform{}
	for _, id := range []string{"darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "linux-arm64-musl", "linux-x64-musl", "win32-x64", "win32-arm64"} {
		out = append(out, application.Platform{ID: id, Name: id})
	}
	return out
}
func (p *Protocol) SelectArtifacts(r application.Release, ids []string) ([]string, error) {
	exists := map[string]bool{}
	for _, a := range r.Artifacts {
		exists[a.Key] = true
	}
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		binary := platforms[id]
		if binary == "" {
			return nil, fmt.Errorf("unsupported platform")
		}
		key := id + "/" + binary
		if !exists[key] {
			return nil, fmt.Errorf("platform resource missing")
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out, nil
}
