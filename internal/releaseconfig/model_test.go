package releaseconfig

import (
	"strings"
	"testing"
)

func fixture() Release {
	return Release{Version: "1.2.3", Build: "abc123", Artifacts: []Artifact{{Key: "windows-x64-exe-user", OS: "windows", Arch: "x64", Format: "exe", Scope: "user", URL: "https://cdn.example/releases/app.exe", SHA256: strings.Repeat("a", 64)}}}
}
func TestPlatformFanoutKeepsDistinctPackages(t *testing.T) {
	first, second := fixture(), fixture()
	second.Artifacts[0].Key = "windows-x64-exe-system"
	second.Artifacts[0].Scope = "system"
	combined, err := Merge([]Release{first, second})
	if err != nil || len(combined.Artifacts) != 2 {
		t.Fatal(combined, err)
	}
	second.Build = "other"
	if _, err = Merge([]Release{first, second}); err == nil {
		t.Fatal("mixed builds accepted")
	}
	if _, err = Merge([]Release{first, first}); err == nil {
		t.Fatal("duplicate key accepted")
	}
}
func TestReleaseValidationDoesNotInventTrustOrPlatforms(t *testing.T) {
	for _, change := range []func(*Artifact){
		func(a *Artifact) { a.OS = "win32" }, func(a *Artifact) { a.Arch = "x86-64" }, func(a *Artifact) { a.SHA256 = "" }, func(a *Artifact) { a.SHA256 = strings.Repeat("A", 64) }, func(a *Artifact) { a.URL = "https://user:secret@cdn.example/app" }, func(a *Artifact) { n := int64(-1); a.Size = &n },
	} {
		r := fixture()
		change(&r.Artifacts[0])
		if r.Validate(true) == nil {
			t.Fatal("invalid artifact accepted", r)
		}
	}
	r := fixture()
	r.Artifacts[0].SHA256 = ""
	r.Artifacts[0].ChecksumURL = "https://cdn.example/releases/app.sha256"
	if err := r.Validate(false); err != nil {
		t.Fatal(err)
	}
	if r.Validate(true) == nil {
		t.Fatal("unresolved checksum accepted")
	}
	r = fixture()
	r.Artifacts[0].Arch = "unknown"
	if err := r.Validate(true); err != nil {
		t.Fatal(err)
	}
}
func TestChecksumMatching(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, input := range []string{hash, strings.ToUpper(hash) + "\n", hash + "  app.exe\r\n", hash + " *app.exe\n"} {
		got, err := ParseChecksum([]byte(input), "app.exe")
		if err != nil || got != hash {
			t.Fatal(got, err)
		}
	}
	for _, input := range []string{hash + "  other.exe", hash + "  app.exe\n" + hash + "  app.exe", "SHA1 (app.exe) = abc", strings.Repeat("a", MaxChecksumBytes+1)} {
		if _, err := ParseChecksum([]byte(input), "app.exe"); err == nil {
			t.Fatal("unmatched/invalid checksum accepted")
		}
	}
	if _, err := ParseChecksum([]byte(hash+"  app.exe"), ""); err == nil {
		t.Fatal("filename guessed")
	}
}
func TestSourceRolesAndPathBoundaries(t *testing.T) {
	allowed := []Source{{Origin: "https://cdn.example", Prefix: "/releases/"}}
	for _, raw := range []string{"https://cdn.example/releases/app.exe", "https://cdn.example/releases/app.exe?token=opaque"} {
		if !Allows(allowed, raw) {
			t.Fatal("valid source rejected", raw)
		}
	}
	for _, raw := range []string{"http://cdn.example/releases/app.exe", "https://evil.example/releases/app.exe", "https://cdn.example/releases-other/app.exe", "https://cdn.example/releases/../secret", "https://cdn.example/releases/%2e%2e/secret", "https://cdn.example/releases/%252e%252e/secret", "https://cdn.example/releases/a%2fb", "https://user:secret@cdn.example/releases/app.exe", "https://cdn.example/releases/app.exe#fragment", "https://cdn.example:8443/releases/app.exe"} {
		if Allows(allowed, raw) {
			t.Fatal("unauthorized source accepted", raw)
		}
	}
	if Allows(nil, "https://cdn.example/releases/app.exe") {
		t.Fatal("empty role accepted")
	}
}
