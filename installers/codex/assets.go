package codex

import (
	"embed"
	"strings"
)

//go:embed generated/* upstream/LICENSE upstream/NOTICE
var assets embed.FS

func Installer(name, base string) ([]byte, error) {
	b, e := assets.ReadFile("generated/" + name)
	if e != nil {
		return nil, e
	}
	return []byte(strings.ReplaceAll(string(b), "@REDAPP_BASE_URL@", base)), nil
}
func License(name string) ([]byte, error) { return assets.ReadFile("upstream/" + name) }
