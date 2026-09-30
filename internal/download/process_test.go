package download

import (
	"bytes"
	"context"
	"fmt"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/instance"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
)

func TestManagerProcessHelper(t *testing.T) {
	if os.Getenv("REDAPP_DOWNLOAD_HELPER") != "1" {
		return
	}
	guard, e := instance.Acquire(os.Getenv("REDAPP_DOWNLOAD_DIR"))
	if e != nil {
		os.Exit(2)
	}
	defer guard.Close()
	db, e := store.Open(guard.Directory)
	if e != nil {
		os.Exit(3)
	}
	defer db.DB.Close()
	u, _ := url.Parse(os.Getenv("REDAPP_DOWNLOAD_SOURCE"))
	c := &distributor.Client{Base: u, HTTP: &http.Client{}}
	m, e := New(guard.Directory, db, c)
	if e != nil {
		os.Exit(4)
	}
	hash := os.Getenv("REDAPP_DOWNLOAD_HASH")
	source := c.URL("asset")
	r := Resource{ID: Identity(source, hash), Source: source, Hash: hash}
	rd, _, e := m.Acquire(context.Background(), r)
	if e != nil {
		os.Exit(5)
	}
	if _, e = io.ReadFull(rd, make([]byte, 8192)); e != nil {
		os.Exit(6)
	}
	os.Stdout.Write([]byte("prefix\n"))
	select {}
}
func TestSIGKILLReleasesLockAndResumesDiskPrefix(t *testing.T) {
	data := bytes.Repeat([]byte("process-crash"), 4000)
	var count atomic.Int32
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		w.Header().Set("ETag", "\"stable\"")
		if n == 1 {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.Write(data[:8192])
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		if r.Header.Get("Range") != "bytes=8192-" || r.Header.Get("If-Range") != "\"stable\"" {
			t.Errorf("崩溃恢复请求不正确: %+v", r.Header)
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 8192-%d/%d", len(data)-1, len(data)))
		w.WriteHeader(206)
		w.Write(data[8192:])
	}))
	dir := t.TempDir()
	r := resource(c, data)
	cmd := exec.Command(os.Args[0], "-test.run=^TestManagerProcessHelper$")
	cmd.Env = append(os.Environ(), "REDAPP_DOWNLOAD_HELPER=1", "REDAPP_DOWNLOAD_DIR="+dir, "REDAPP_DOWNLOAD_SOURCE="+c.Base.String(), "REDAPP_DOWNLOAD_HASH="+r.Hash)
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer cmd.Process.Kill()
	if _, e = io.ReadFull(stdout, make([]byte, 7)); e != nil {
		t.Fatal(e)
	}
	if g, e := instance.Acquire(dir); e == nil {
		g.Close()
		t.Fatal("运行时第二实例取得锁")
	}
	cmd.Process.Kill()
	cmd.Wait()
	g, e := instance.Acquire(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	db, e := store.Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	m, e := New(dir, db, c)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	if !bytes.Equal(collect(t, m, r), data) || count.Load() != 2 {
		t.Fatal("SIGKILL 后未安全续传")
	}
}
