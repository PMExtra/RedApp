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
			p := filepath.Join(dir, LockName)
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
	first, _ := os.Stat(filepath.Join(dir, LockName))
	g.Close()
	g, e = Acquire(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	second, _ := os.Stat(filepath.Join(dir, LockName))
	if !os.SameFile(first, second) {
		t.Fatal("锁 inode 被替换")
	}
}

func TestCheckObservesHolderWithoutCreatingLock(t *testing.T) {
	dir := t.TempDir()
	if err := Check(dir); err != nil {
		t.Fatalf("Check on an unlocked directory = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, LockName)); !os.IsNotExist(err) {
		t.Fatalf("Check created the lock file: %v", err)
	}
	g, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = Check(dir); err == nil {
		t.Fatal("Check ignored a live lock holder")
	}
	g.Close()
	if err = Check(dir); err != nil {
		t.Fatalf("Check after release = %v", err)
	}
	fifo := t.TempDir()
	if err = syscall.Mkfifo(filepath.Join(fifo, LockName), 0600); err != nil {
		t.Fatal(err)
	}
	if err = Check(fifo); err == nil {
		t.Fatal("Check accepted a FIFO as the lock file")
	}
}
