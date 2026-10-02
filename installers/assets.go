// Package installers serves the reviewed generated installers and fixed public
// trust/licensing assets. Application registration remains in the builtin manifest.
package installers

import (
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"regexp"
	"strings"
)

// Never embed upstream install scripts: only patched generated scripts may be served.
//
//go:embed */*/generated/* */*/upstream/LICENSE* */*/upstream/NOTICE */*/upstream/*.asc
var assets embed.FS

var applicationID = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*/[a-z0-9]+(?:-[a-z0-9]+)*$`)
var publicFile = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validAsset(appID, name string) bool {
	parts := strings.Split(appID, "/")
	return applicationID.MatchString(appID) && len(parts[0]) <= 63 && len(parts[1]) <= 63 && publicFile.MatchString(name)
}

// Installer renders a generated installer against the complete canonical public
// application root, e.g. https://downloads.example/openai/codex. The caller must
// authorize the filename against its registered application descriptor first.
func Installer(appID, name, publicAppRoot string) ([]byte, error) {
	if !validAsset(appID, name) || (name != "install.sh" && name != "install.ps1") {
		return nil, fs.ErrNotExist
	}
	u, err := url.Parse(publicAppRoot)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || u.Path != "/"+appID {
		return nil, fmt.Errorf("invalid canonical installer application root")
	}
	// The placeholder is inside double-quoted strings in both reviewed scripts.
	// Restrict the origin rather than allowing shell/PowerShell expansion syntax.
	if strings.ContainsAny(u.Host, "\\\"'`$ \t\r\n") {
		return nil, fmt.Errorf("unsafe installer origin")
	}
	b, err := assets.ReadFile(appID + "/generated/" + name)
	if err != nil {
		return nil, err
	}
	return []byte(strings.ReplaceAll(string(b), "@REDAPP_BASE_URL@", publicAppRoot)), nil
}

// PublicAsset reads only embedded licensing and fixed trust assets; the original
// upstream installers, provenance and patches are deliberately unavailable here.
func PublicAsset(appID, name string) ([]byte, error) {
	name = strings.TrimPrefix(name, "licenses/")
	if !validAsset(appID, name) {
		return nil, fs.ErrNotExist
	}
	return assets.ReadFile(appID + "/upstream/" + name)
}
