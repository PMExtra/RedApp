package instance

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLockRejectsSymlinkAndFIFOAndPreservesInode(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "instance.lock")
			var e error
			if kind == "symlink" {
				target := filepath.Join(t.TempDir(), "target")
				os.WriteFile(target, nil, 0600)
				e = os.Symlink(target, p)
			} else {
				e = syscall.Mkfifo(p, 0600)
			}
			if e != nil {
				t.Fatal(e)
			}
			if g, e := Acquire(dir); e == nil {
				g.Close()
				t.Fatal("无效锁文件被接受")
			}
		})
	}
	dir := t.TempDir()
	g, e := Acquire(dir)
	if e != nil {
		t.Fatal(e)
	}
	first, _ := os.Stat(filepath.Join(dir, "instance.lock"))
	g.Close()
	g, e = Acquire(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	second, _ := os.Stat(filepath.Join(dir, "instance.lock"))
	if !os.SameFile(first, second) {
		t.Fatal("锁 inode 被替换")
	}
}
