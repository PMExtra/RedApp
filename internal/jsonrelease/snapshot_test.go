package jsonrelease

import (
	"github.com/PMExtra/RedApp/internal/releaseconfig"
	"strings"
	"testing"
)

func releaseFixture() releaseconfig.Release {
	size := int64(42)
	return releaseconfig.Release{Version: "1.2.3", Build: "abc", Artifacts: []releaseconfig.Artifact{{Key: "windows-x64-exe-user", OS: "windows", Arch: "x64", Format: "exe", Scope: "user", URL: "https://cdn.example/files/app.exe", SHA256: strings.Repeat("a", 64), Size: &size}}}
}
func TestSnapshotOwnsItsDataAndSelection(t *testing.T) {
	r := releaseFixture()
	snapshot, err := NewSnapshot(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Artifacts[0].URL = "https://evil.example/"
	*r.Artifacts[0].Size = 0
	copy := snapshot.Release()
	copy.Artifacts[0].SHA256 = ""
	*copy.Artifacts[0].Size = 1
	selected, err := snapshot.Select([]string{"windows-x64-exe-user"})
	if err != nil {
		t.Fatal(err)
	}
	if selected[0].URL != "https://cdn.example/files/app.exe" || *selected[0].Size != 42 || selected[0].SHA256 == "" {
		t.Fatal("snapshot mutated", selected)
	}
	*selected[0].Size = 2
	if *snapshot.Release().Artifacts[0].Size != 42 {
		t.Fatal("selection shared mutable size")
	}
	if _, err = snapshot.Select([]string{"windows-arm64-exe-user"}); err == nil {
		t.Fatal("missing architecture fell back")
	}
	if _, err = snapshot.Select([]string{"windows-x64-exe-user", "windows-x64-exe-user"}); err == nil {
		t.Fatal("duplicate accepted")
	}
}
func TestSnapshotIdentityAndInternalDownload(t *testing.T) {
	r := releaseFixture()
	old, err := NewSnapshot(r)
	if err != nil {
		t.Fatal(err)
	}
	path, err := old.DownloadPath("microsoft/vscode", r.Artifacts[0].Key)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/microsoft/vscode/releases/1.2.3/windows-x64-exe-user" {
		t.Fatal(path)
	}
	r.Artifacts[0].SHA256 = strings.Repeat("b", 64)
	conflict, err := NewSnapshot(r)
	if err != nil {
		t.Fatal(err)
	}
	if old.CheckImmutable(conflict) == nil {
		t.Fatal("same version/build changed bytes")
	}
	// Another build of a known version conflicts: the version is the identity.
	r.Build = "def"
	rebuilt, err := NewSnapshot(r)
	if err != nil {
		t.Fatal(err)
	}
	if old.CheckImmutable(rebuilt) == nil {
		t.Fatal("another build of the same version accepted")
	}
	r.Version = "1.2.4"
	next, err := NewSnapshot(r)
	if err != nil {
		t.Fatal(err)
	}
	if err = old.CheckImmutable(next); err != nil {
		t.Fatal(err)
	}
	nextPath, err := next.DownloadPath("microsoft/vscode", r.Artifacts[0].Key)
	if err != nil || nextPath == path {
		t.Fatal(nextPath, err)
	}
	stablePath, err := old.DownloadPath("microsoft/vscode", r.Artifacts[0].Key)
	if err != nil || stablePath != path {
		t.Fatal("captured snapshot changed", stablePath, err)
	}
	if _, err = old.DownloadPath("../evil", r.Artifacts[0].Key); err == nil {
		t.Fatal("invalid local path")
	}
}
