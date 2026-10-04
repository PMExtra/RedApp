package pathmatch

import (
	"errors"
	"strings"
	"testing"
)

func TestGlobMatchesFilesAndDirectoryAncestors(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"/", "/any/depth/file", true},
		{"/releases", "/releases", true}, {"/releases", "/releases/v1/file", true}, {"/releases", "/releases-other/file", false},
		{"/releases/", "/releases", false}, {"/releases/", "/releases/", true}, {"/releases/", "/releases/v1/file", true},
		{"/releases/*/", "/releases/v1", false}, {"/releases/*/", "/releases/v1/", true}, {"/releases/*/", "/releases/v1/file", true}, {"/releases/*/", "/releases/v1/deep/file", true}, {"/releases/*/", "/releases", false},
		{"/releases/*.zip", "/releases/a.zip", true}, {"/releases/*.zip", "/releases/v1/a.zip", false},
		{"/releases/**/*.zip", "/releases/a.zip", true}, {"/releases/**/*.zip", "/releases/v1/a.zip", true},
		{"/{releases,builds}/v[12]/file?.bin", "/builds/v2/file3.bin", true},
		{"/资料/", "/资料/file name.bin", true},
		{`/literal/\*.bin`, "/literal/*.bin", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"|"+tt.path, func(t *testing.T) {
			m, err := Compile(Spec{Type: "glob", Pattern: tt.pattern})
			if err != nil {
				t.Fatal(err)
			}
			if got := m.Match(tt.path); got != tt.want {
				t.Fatalf("Match(%q)=%v want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestRE2IsAFullPathMatchWithoutDirectoryInheritance(t *testing.T) {
	m, err := Compile(Spec{Type: "re2", Pattern: `/releases|/files/[a-z]+\.bin`})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/releases", "/files/file.bin"} {
		if !m.Match(path) {
			t.Fatal("full regex match rejected", path)
		}
	}
	for _, path := range []string{"/releases/child", "/prefix/releases", "/files/file.bin/child", "/files/a.bin.extra"} {
		if m.Match(path) {
			t.Fatal("substring or ancestor regex match accepted", path)
		}
	}
}

func TestInvalidPatternsAndUncanonicalPathsAreRejected(t *testing.T) {
	for _, spec := range []Spec{{Type: "glob", Pattern: ""}, {Type: "glob", Pattern: "/["}, {Type: "glob", Pattern: "/{a,b"}, {Type: "glob", Pattern: strings.Repeat("a", MaxPatternBytes+1)}, {Type: "glob", Pattern: "/line\nbreak"}, {Type: "re2", Pattern: `(?<=x)y`}, {Type: "re2", Pattern: `(x)\1`}, {Type: "other", Pattern: "/"}} {
		if _, err := Compile(spec); !errors.Is(err, ErrInvalidPattern) {
			t.Fatal(spec, err)
		}
	}
	m, err := Compile(Spec{Type: "glob", Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative/file", "/a/../file", "/a/./file", "/a//file", "/a\\file", "/a%2fb", "/file?query=1", "/file#fragment", "/a\x00b", string([]byte{'/', 0xff})} {
		if ValidatePath(path) == nil || m.Match(path) {
			t.Fatal("path was cleaned or decoded instead of rejected", path)
		}
	}
}
