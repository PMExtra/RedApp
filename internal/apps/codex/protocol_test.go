package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/testutil"
	"net/http"
	"strings"
	"testing"
)

func fixture(base, v string) []byte {
	hash := sha256.Sum256([]byte("artifact"))
	b, _ := json.Marshal(Release{Tag: "rust-v" + v, Assets: []Asset{{Name: "asset.tar.gz", Digest: "sha256:" + hex.EncodeToString(hash[:]), URL: base + "/releases/" + v + "/asset.tar.gz"}, {Name: "other.tgz", Digest: "sha256:" + hex.EncodeToString(hash[:]), URL: base + "/releases/" + v + "/other.tgz"}}})
	return b
}
func TestMalformedMetadataRejected(t *testing.T) {
	c, _ := testutil.Upstream(t, http.NotFoundHandler())
	cat := NewProtocol(c)
	good := string(fixture(c.Base.String(), "0.159.2"))
	cases := []string{`{"tag_name":"rust-v0.159.2","tag_name":"rust-v0.159.2","assets":[]}`, strings.Replace(good, "sha256:", "sha512:", 1), strings.Replace(good, "asset.tar.gz", "../asset.tar.gz", 1), strings.Replace(good, c.Base.String(), "https://evil.example", 1), strings.Replace(good, "rust-v0.159.2", "rust-v0.159.3", 1), strings.Replace(good, "other.tgz", "asset.tar.gz", -1), good + " trailing"}
	for i, b := range cases {
		if _, e := cat.parse([]byte(b), "0.159.2"); e == nil {
			t.Errorf("bad case %d accepted", i)
		}
	}
	m, e := cat.parse([]byte(good), "0.159.2")
	if e != nil {
		t.Fatal(e)
	}

	release, err := cat.VerifyRelease("0.159.2", application.Envelope{Raw: []byte(good)})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cat.Render(release, application.Operation{Kind: application.MetadataOperation, Name: "release.json"}, "https://enterprise.example/openai/codex")
	if err != nil {
		t.Fatal(err)
	}
	var rewritten Release
	if json.Unmarshal(out.Body, &rewritten) != nil || !strings.HasPrefix(rewritten.Assets[0].URL, "https://enterprise.example/openai/codex/releases/0.159.2/") || rewritten.Assets[0].Digest != m.Assets[0].Digest || rewritten.Assets[0].Size != nil {
		t.Fatal("public rewrite changed trusted digest or optional size")
	}
}
func TestVersionOrdering(t *testing.T) {
	for _, pair := range [][2]string{{"v0.9.0", "rust-v0.10.0"}, {"0.150.0-alpha.2", "0.150.0-alpha.10"}, {"0.150.0-beta.1", "0.150.0"}, {"0.150.0-alpha", "0.150.0-beta"}} {
		n, e := Compare(pair[0], pair[1])
		if e != nil || n >= 0 {
			t.Fatal(pair, n, e)
		}
	}
	for _, v := range []string{"../1.0.0", "0.1", "0.01.0", "1.0.0-unknown", "99999999999999999999999.0.0"} {
		if _, e := Normalize(v); e == nil {
			t.Fatal(v)
		}
	}
}

func TestCanonicalDistributionPaths(t *testing.T) {
	p := NewProtocol(nil)
	for _, path := range []string{"releases/v1.0.0/release.json", "releases/rust-v1.0.0/a", "releases/1.0.0/../a", "channels/stable", "releases/1.0.0/a?url=x"} {
		if _, err := p.ParsePath(path); err == nil {
			t.Fatalf("unexpected path accepted: %s", path)
		}
	}
}
