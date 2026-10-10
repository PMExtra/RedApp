package instance

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLockSubprocess(t *testing.T) {
	if os.Getenv("REDAPP_LOCK_HELPER") == "1" {
		g, e := Acquire(os.Getenv("REDAPP_LOCK_DIR"))
		if e != nil {
			os.Exit(2)
		}
		defer g.Close()
		os.Stdout.Write([]byte("ready\n"))
		select {}
	}
}
func TestLockAliasDoubleInstanceAndSIGKILL(t *testing.T) {
	dir := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if e := os.Symlink(dir, alias); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockSubprocess$")
	cmd.Env = append(os.Environ(), "REDAPP_LOCK_HELPER=1", "REDAPP_LOCK_DIR="+dir)
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer cmd.Process.Kill()
	b := make([]byte, 6)
	if _, e = out.Read(b); e != nil {
		t.Fatal(e)
	}
	if _, e = Acquire(alias); e == nil {
		t.Fatal("路径别名绕过独占锁")
	}
	if e = cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	cmd.Wait()
	g, e := Acquire(alias)
	if e != nil {
		t.Fatal("SIGKILL 后锁未释放", e)
	}
	g.Close()
	if _, e = os.Stat(filepath.Join(dir, LockName)); e != nil {
		t.Fatal("锁 inode 被删除")
	}
}
