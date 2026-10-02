package claude

import (
	"embed"
	"strings"
)

//go:embed upstream/claude-code.asc upstream/LICENSE.md
var sources embed.FS

//go:embed generated/*
var generated embed.FS

func Installer(name, base string) ([]byte, error) {
	b, err := generated.ReadFile("generated/" + name)
	if err != nil {
		return nil, err
	}
	return []byte(strings.ReplaceAll(string(b), "@REDAPP_BASE_URL@", base)), nil
}

func PublicKey() []byte { b, _ := sources.ReadFile("upstream/claude-code.asc"); return b }
func License() []byte   { b, _ := sources.ReadFile("upstream/LICENSE.md"); return b }
