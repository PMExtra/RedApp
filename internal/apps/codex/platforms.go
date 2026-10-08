package codex

import (
	"fmt"
	"github.com/PMExtra/RedApp/internal/application"
)

var targets = map[string]string{"darwin-arm64": "aarch64-apple-darwin", "darwin-x64": "x86_64-apple-darwin", "linux-arm64": "aarch64-unknown-linux-musl", "linux-x64": "x86_64-unknown-linux-musl", "win32-arm64": "aarch64-pc-windows-msvc", "win32-x64": "x86_64-pc-windows-msvc"}

func (p *Protocol) Platforms() []application.Platform {
	out := []application.Platform{}
	for _, id := range []string{"darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "win32-arm64", "win32-x64"} {
		out = append(out, application.Platform{ID: id, Name: id})
	}
	return out
}
func (p *Protocol) SelectArtifacts(r application.Release, platforms []string) ([]string, error) {
	exists := map[string]bool{}
	for _, a := range r.Artifacts {
		exists[a.Key] = true
	}
	seen := map[string]bool{}
	out := []string{}
	for _, id := range platforms {
		target, ok := targets[id]
		if !ok {
			return nil, fmt.Errorf("unsupported platform")
		}
		keys := []string{"codex-package-" + target + ".tar.gz", "codex-package_SHA256SUMS"}
		if !exists[keys[0]] || !exists[keys[1]] {
			keys = []string{"codex-npm-" + id + "-" + r.Version + ".tgz"}
		}
		for _, key := range keys {
			if !exists[key] {
				return nil, fmt.Errorf("platform resource missing")
			}
			if !seen[key] {
				seen[key] = true
				out = append(out, key)
			}
		}
	}
	return out, nil
}
