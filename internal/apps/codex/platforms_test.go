package codex

import (
	"github.com/PMExtra/RedApp/internal/application"
	"reflect"
	"testing"
)

func TestPlatformsPreferInstallerPackageAndDeduplicate(t *testing.T) {
	p := &Protocol{}
	r := application.Release{Version: "1.2.3"}
	for _, key := range []string{"codex-package-x86_64-unknown-linux-musl.tar.gz", "codex-package_SHA256SUMS", "codex-npm-linux-x64-1.2.3.tgz", "codex-npm-win32-arm64-1.2.3.tgz"} {
		r.Artifacts = append(r.Artifacts, application.VerifiedArtifact{Key: key})
	}
	keys, err := p.SelectArtifacts(r, []string{"linux-x64", "win32-arm64", "linux-x64"})
	if err != nil || !reflect.DeepEqual(keys, []string{"codex-package-x86_64-unknown-linux-musl.tar.gz", "codex-package_SHA256SUMS", "codex-npm-win32-arm64-1.2.3.tgz"}) {
		t.Fatal(keys, err)
	}
	if _, err = p.SelectArtifacts(r, []string{"darwin-arm64"}); err == nil {
		t.Fatal("missing platform accepted")
	}
	if len(p.Platforms()) != 6 {
		t.Fatal("platform count")
	}
}
