package store

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/identity"
)

func TestHostedDirectoryListsDirectChildren(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := s.CreateVendor(VendorInput{ID: "acme", Name: LocalizedText{"Acme", "示例"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateApplication(v.ID, ApplicationInput{ID: "files", Name: LocalizedText{"Files", "文件"}, Provider: "hosted", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// "docs0" and "docs-old" sort next to the "docs/" range and must stay out of it.
	files := map[string]int64{"readme.txt": 1, "docs": 2, "docs/a.txt": 3, "docs/b/c.txt": 4, "docs/b/d.txt": 5, "docs0": 6, "docs-old/x": 7, "文档/说明.txt": 8}
	for path, at := range files {
		id, err := identity.NewUID()
		if err != nil {
			t.Fatal(err)
		}
		f := HostedFile{AppUID: a.UID, Path: path, ID: id, SHA256: strings.Repeat("0", 64), SizeBytes: at * 10, CreatedAt: time.Unix(at, 0)}
		if _, err = s.CommitHosted(a.Key, a.Revision, v.Revision, "", f); err != nil {
			t.Fatal(path, err)
		}
	}
	type row struct {
		Name string
		Dir  bool
		Size int64
		At   int64
	}
	list := func(dir string, limit int) ([]row, bool) {
		t.Helper()
		entries, more, err := s.HostedDirectory(a.UID, dir, limit)
		if err != nil {
			t.Fatal(err)
		}
		out := []row{}
		for _, e := range entries {
			out = append(out, row{e.Name, e.Directory, e.SizeBytes, e.CreatedAt.Unix()})
		}
		return out, more
	}
	root, more := list("", 100)
	want := []row{{"docs", true, 0, 5}, {"docs-old", true, 0, 7}, {"文档", true, 0, 8}, {"docs", false, 20, 2}, {"docs0", false, 60, 6}, {"readme.txt", false, 10, 1}}
	if !reflect.DeepEqual(root, want) || more {
		t.Fatalf("root %+v more=%v", root, more)
	}
	if docs, _ := list("docs", 100); !reflect.DeepEqual(docs, []row{{"b", true, 0, 5}, {"a.txt", false, 30, 3}}) {
		t.Fatalf("docs %+v", docs)
	}
	if nested, _ := list("文档", 100); !reflect.DeepEqual(nested, []row{{"说明.txt", false, 80, 8}}) {
		t.Fatalf("unicode %+v", nested)
	}
	if page, more := list("", 2); len(page) != 2 || !more {
		t.Fatalf("limit %+v more=%v", page, more)
	}
	if empty, more := list("missing", 10); len(empty) != 0 || more {
		t.Fatalf("missing %+v", empty)
	}
	for dir, want := range map[string]bool{"docs": true, "docs/b": true, "readme.txt": false, "doc": false, "missing": false} {
		if got, err := s.HostedDirectoryExists(a.UID, dir); err != nil || got != want {
			t.Fatalf("exists %s: %v %v", dir, got, err)
		}
	}
	if _, _, err = s.HostedDirectory(a.UID, "docs/", 10); err == nil {
		t.Fatal("trailing slash accepted")
	}
}
