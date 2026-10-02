package installers

import (
	"bytes"
	"testing"
)

func TestCanonicalInstallerRenderingAndAssetBoundary(t *testing.T) {
	for _, appID := range []string{"openai/codex", "anthropic/claude-code"} {
		for _, name := range []string{"install.sh", "install.ps1"} {
			root := "https://download.example:8443/" + appID
			body, err := Installer(appID, name, root)
			if err != nil || !bytes.Contains(body, []byte(root)) || bytes.Contains(body, []byte("@REDAPP_BASE_URL@")) {
				t.Fatalf("%s/%s render: %v", appID, name, err)
			}
		}
	}
	for _, root := range []string{"https://download.example", "https://download.example/anthropic/claude-code", "https://download.example/openai/codex?extra=1", "https://download.example/openai/codex#fragment", "https://user@download.example/openai/codex", "https://bad$host.example/openai/codex", "https://bad`host.example/openai/codex"} {
		if _, err := Installer("openai/codex", "install.sh", root); err == nil {
			t.Errorf("accepted unsafe or mismatched application root %q", root)
		}
	}
	for _, pair := range [][2]string{{"openai/codex", "licenses/LICENSE"}, {"openai/codex", "licenses/NOTICE"}, {"anthropic/claude-code", "LICENSE.md"}, {"anthropic/claude-code", "claude-code.asc"}} {
		if body, err := PublicAsset(pair[0], pair[1]); err != nil || len(body) == 0 {
			t.Fatalf("public asset %v: %v", pair, err)
		}
	}
	for _, pair := range [][2]string{{"openai/codex", "install.sh"}, {"openai/codex", "../generated/install.sh"}, {"openai/codex", "licenses/../../provenance.json"}, {"anthropic/../openai/codex", "LICENSE"}, {"codex", "LICENSE"}, {"unknown/app", "LICENSE"}} {
		if _, err := PublicAsset(pair[0], pair[1]); err == nil {
			t.Errorf("exposed unregistered/raw asset %v", pair)
		}
	}
}
