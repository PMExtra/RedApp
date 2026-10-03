package claude

import (
	"bytes"
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/testutil"
	"net/http"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T) ([]byte, []byte) {
	t.Helper()
	raw, e := os.ReadFile("testdata/manifest.json")
	if e != nil {
		t.Fatal(e)
	}
	sig, e := os.ReadFile("testdata/manifest.json.sig")
	if e != nil {
		t.Fatal(e)
	}
	return raw, sig
}
func TestPinnedOfficialSignature(t *testing.T) {
	raw, sig := fixture(t)
	if e := Verify(raw, sig); e != nil {
		t.Fatal(e)
	}
	for _, r := range [][]byte{append(append([]byte{}, raw...), '\n'), bytes.Replace(raw, []byte("2.1.285"), []byte("2.1.286"), 1), nil} {
		if Verify(r, sig) == nil {
			t.Fatal("tampered manifest accepted")
		}
	}
	bad := append([]byte{}, sig...)
	bad[len(bad)/2] ^= 1
	if Verify(raw, bad) == nil || Verify(raw, nil) == nil || verifyWithKey(raw, sig, []byte("untrusted key")) == nil {
		t.Fatal("untrusted signature accepted")
	}
}
func TestManifestVersionAndBounds(t *testing.T) {
	raw, _ := fixture(t)
	if _, err := parse(raw, "2.1.285"); err != nil {
		t.Fatalf("valid fixture: %v", err)
	}
	for _, v := range []string{"../2.1.285", "2.1.285/../../", "02.1.285", "2.1.285\n", "2.1.285?x", "999999999999999999999.1.2"} {
		if ValidVersion(v) {
			t.Fatal(v)
		}
	}
	for name, change := range map[string]func(*Manifest){
		"version mismatch": func(m *Manifest) { m.Version = "2.1.286" },
		"unsafe binary": func(m *Manifest) {
			p := m.Platforms["linux-x64"]
			p.Binary = "../../secret"
			m.Platforms["linux-x64"] = p
		},
	} {
		t.Run(name, func(t *testing.T) {
			var m Manifest
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			change(&m)
			b, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parse(b, "2.1.285"); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}
func TestManifestDuplicateKeys(t *testing.T) {
	raw, _ := fixture(t)
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	good, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parse(good, m.Version); err != nil {
		t.Fatalf("valid fixture: %v", err)
	}
	for name, field := range map[string]string{
		"top level":       `"version":"2.1.285"`,
		"nested platform": `"binary":"claude"`,
	} {
		t.Run(name, func(t *testing.T) {
			bad := bytes.Replace(good, []byte(field), []byte(field+","+field), 1)
			if bytes.Equal(good, bad) {
				t.Fatal("fixture did not contain target field")
			}
			if _, err := parse(bad, m.Version); err == nil {
				t.Fatal("duplicate field accepted in otherwise valid manifest")
			}
		})
	}
}
func TestManifestRejectsInvalidPlatformFields(t *testing.T) {
	raw, _ := fixture(t)
	for _, mutate := range []func(*Manifest){
		func(m *Manifest) { m.Platforms["unreviewed-platform"] = m.Platforms["linux-x64"] },
		func(m *Manifest) {
			p := m.Platforms["linux-x64"]
			p.Binary = "claude.exe"
			m.Platforms["linux-x64"] = p
		},
		func(m *Manifest) { p := m.Platforms["linux-x64"]; p.Checksum = ""; m.Platforms["linux-x64"] = p },
		func(m *Manifest) {
			p := m.Platforms["linux-x64"]
			p.Checksum = strings.Repeat("z", 64)
			m.Platforms["linux-x64"] = p
		},
		func(m *Manifest) { p := m.Platforms["linux-x64"]; p.Size = 0; m.Platforms["linux-x64"] = p },
		func(m *Manifest) { p := m.Platforms["linux-x64"]; p.Size = 1 << 40; m.Platforms["linux-x64"] = p },
	} {
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		mutate(&m)
		body, _ := json.Marshal(m)
		if _, err := parse(body, "2.1.285"); err == nil {
			t.Fatal("invalid platform accepted")
		}
	}
}

func TestProtocolPreservesSignedRepresentationAndPathBoundary(t *testing.T) {
	raw, sig := fixture(t)
	upstream, _ := testutil.Upstream(t, http.NotFoundHandler())
	p := NewProtocol(upstream)
	release, err := p.VerifyRelease("2.1.285", application.Envelope{Raw: raw, Signature: sig})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manifest.json", "manifest.json.sig"} {
		op, err := p.ParsePath("2.1.285/" + name)
		if err != nil {
			t.Fatal(err)
		}
		response, err := p.Render(release, op, "https://mirror.example/anthropic/claude-code")
		expected := raw
		if name == "manifest.json.sig" {
			expected = sig
		}
		if err != nil || !bytes.Equal(response.Body, expected) {
			t.Fatalf("signed bytes changed: %v", err)
		}
	}
	for _, path := range []string{"2.1.285/other/claude", "2.1.285/linux-x64/claude.exe", "2.1.285/linux-x64/claude.zst", "2.1.285/manifest.zst.json", "latest/linux-x64/claude", "../manifest.json"} {
		if _, err := p.ParsePath(path); err == nil {
			t.Fatalf("unauthorized path %q", path)
		}
	}
	if _, err := p.CompareVersions("2.1.0-alpha", "2.1.0-beta"); err == nil {
		t.Fatal("unknown prerelease ordering became destructive")
	}
}
