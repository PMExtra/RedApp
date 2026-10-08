package claude

import (
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/testutil"
	"net/http"
	"testing"
)

func TestPlatformsUseVerifiedManifestKeys(t *testing.T) {
	raw, sig := fixture(t)
	upstream, _ := testutil.Upstream(t, http.NotFoundHandler())
	p := NewProtocol(upstream)
	r, err := p.VerifyRelease("2.1.285", application.Envelope{Raw: raw, Signature: sig})
	if err != nil {
		t.Fatal(err)
	}
	keys, err := p.SelectArtifacts(r, []string{"linux-x64-musl", "win32-arm64", "linux-x64-musl"})
	if err != nil || len(keys) != 2 || keys[0] != "linux-x64-musl/claude" || keys[1] != "win32-arm64/claude.exe" {
		t.Fatal(keys, err)
	}
	if len(p.Platforms()) != 8 {
		t.Fatal("platform count")
	}
	if _, err = p.SelectArtifacts(application.Release{}, []string{"linux-x64"}); err == nil {
		t.Fatal("missing platform accepted")
	}
}
