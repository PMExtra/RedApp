package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func setup(t *testing.T, c *distributor.Client) (*Manager, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	db, e := store.Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	m, e := newTestManager(dir, db, c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Close(); db.DB.Close() })
	return m, db, dir
}
func resource(c *distributor.Client, b []byte) Resource {
	source := c.URL("asset")
	hash := digest(b)
	return Resource{Application: testApp, Version: "0.1.0", Key: "asset", ID: LogicalIdentity(testApp, "0.1.0", "asset"), Source: source, Hash: hash, Labels: map[string]string{"version": "0.1.0", "name": "asset"}}
}

const testApp = "openai/codex"

func newTestManager(dir string, db *store.Store, c *distributor.Client) (*Manager, error) {
	m, err := NewApplications(dir, db, map[string]*distributor.Client{testApp: c})
	if err == nil {
		// Package tests keep the production retry count with short backoff.
		m.retryBase, m.retryMax = time.Millisecond, 10*time.Millisecond
	}
	return m, err
}
func authorize(t *testing.T, m *Manager, r Resource) {
	t.Helper()
	if e := m.db.PutRelease(store.ReleaseMetadata{AppID: r.Application, Version: r.Version, Raw: []byte("trusted fixture"), TrustRevision: 1, FetchedAt: time.Now()}, []store.Resource{{AppID: r.Application, Version: r.Version, Key: r.Key, SourceURL: r.Source, SHA256: r.Hash, ExpectedSize: r.Size}}); e != nil {
		t.Fatal(e)
	}
}
func authorizedResource(t *testing.T, m *Manager, c *distributor.Client, b []byte) Resource {
	t.Helper()
	r := resource(c, b)
	authorize(t, m, r)
	return r
}
func collect(t *testing.T, m *Manager, r Resource) []byte {
	t.Helper()
	rd, _, e := m.Acquire(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	defer rd.Close()
	b, e := io.ReadAll(rd)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("barrier 超时")
	}
}
func TestSharedStreaming100LateSlowAndCancelled(t *testing.T) {
	data := bytes.Repeat([]byte("abcdef"), 10000)
	prefix := data[:8192]
	started := make(chan struct{})
	release := make(chan struct{})
	var requests atomic.Int32
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Write(prefix)
		w.(http.Flusher).Flush()
		close(started)
		<-release
		w.Write(data[len(prefix):])
	}))
	m, db, _ := setup(t, c)
	r := authorizedResource(t, m, c, data)
	first, _, e := m.Acquire(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	await(t, started)
	got := make([]byte, len(prefix))
	if _, e = io.ReadFull(first, got); e != nil || !bytes.Equal(got, prefix) {
		t.Fatal("第一客户端未流式取得前缀", e)
	}
	readers := []*Reader{first}
	for i := 1; i < 100; i++ {
		rd, hit, e := m.Acquire(context.Background(), r)
		if e != nil || !hit {
			t.Fatal("未共享同代", e)
		}
		readers = append(readers, rd)
	}
	late := readers[1]
	got = make([]byte, len(prefix))
	if _, e = io.ReadFull(late, got); e != nil || !bytes.Equal(got, prefix) {
		t.Fatal("晚到读者不能从零读取", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancelled, _, e := m.Acquire(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	if _, e = cancelled.Read(make([]byte, 1)); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	cancelled.Close()
	// Last reader remains completely idle while the shared writer and 99 others finish.
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i, rd := range readers[:99] {
		wg.Add(1)
		go func(i int, rd *Reader) {
			defer wg.Done()
			defer rd.Close()
			b, e := io.ReadAll(rd)
			expected := data
			if i < 2 {
				expected = data[len(prefix):]
			}
			if e != nil || !bytes.Equal(b, expected) {
				errs <- fmt.Errorf("reader %d: %v (%d bytes)", i, e, len(b))
			}
		}(i, rd)
	}
	close(release)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	slow := readers[99]
	b, e := io.ReadAll(slow)
	slow.Close()
	if e != nil || !bytes.Equal(b, data) {
		t.Fatal("慢读者结果不完整", e)
	}
	if requests.Load() != 1 {
		t.Fatalf("上游任务 %d", requests.Load())
	}
	if !bytes.Equal(collect(t, m, r), data) || requests.Load() != 1 {
		t.Fatal("永久缓存未复用")
	}
	counters, err := db.Counters()
	if err != nil || counters["upstream_bytes"] != int64(len(data)) {
		t.Fatal("shared readers and cache hits must not duplicate upstream payload", counters, err)
	}
}
func TestHashInvalidStartsNewGeneration(t *testing.T) {
	var bad atomic.Bool
	bad.Store(true)
	data := []byte("trusted-data")
	var count atomic.Int32
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if bad.Load() {
			w.Write([]byte("invalid-data"))
		} else {
			w.Write(data)
		}
	}))
	m, db, _ := setup(t, c)
	r := authorizedResource(t, m, c, data)
	rd, _, e := m.Acquire(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	old := rd.g.ID
	_, e = io.ReadAll(rd)
	if e == nil {
		t.Fatal("坏 hash 被接受")
	}
	rd.Close()
	bad.Store(false)
	if !bytes.Equal(collect(t, m, r), data) {
		t.Fatal("新代失败")
	}
	m.mu.Lock()
	g := m.current[r.ID]
	m.mu.Unlock()
	if g.ID == old || count.Load() != 2 {
		t.Fatal("未建立新代")
	}
	counters, err := db.Counters()
	if err != nil || counters["upstream_bytes"] != int64(len("invalid-data")+len(data)) {
		t.Fatal("invalid hashes still consumed upstream payload", counters, err)
	}
}
func TestValidatedResumeAndRejectedBranches(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), 2000)
	cut := 7000
	for _, mode := range []string{"valid", "200", "bad-range", "etag", "encoding", "416-bad", "short-range"} {
		t.Run(mode, func(t *testing.T) {
			var count atomic.Int32
			var mu sync.Mutex
			ranges := []string{}
			c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := count.Add(1)
				mu.Lock()
				ranges = append(ranges, r.Header.Get("Range"))
				mu.Unlock()
				w.Header().Set("ETag", "\"stable\"")
				if n == 1 {
					w.Header().Set("Content-Length", fmt.Sprint(len(data)))
					w.Write(data[:cut])
					return
				}
				if n == 2 {
					if r.Header.Get("Range") != fmt.Sprintf("bytes=%d-", cut) || r.Header.Get("If-Range") != "\"stable\"" {
						t.Error("缺少续传请求头")
					}
					switch mode {
					case "200":
						w.Write(data)
						return
					case "bad-range":
						w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", cut+1, len(data)-1, len(data)))
					case "etag":
						w.Header().Set("ETag", "\"changed\"")
						w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", cut, len(data)-1, len(data)))
					case "encoding":
						w.Header().Set("Content-Encoding", "gzip")
					case "416-bad":
						w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(data)))
						w.WriteHeader(416)
						return
					case "short-range":
						w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", cut, len(data)-2, len(data)))
					default:
						w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", cut, len(data)-1, len(data)))
					}
					w.Header().Set("Content-Length", fmt.Sprint(len(data)-cut))
					w.WriteHeader(206)
					w.Write(data[cut:])
					return
				}
				w.Write(data)
			}))
			m, db, _ := setup(t, c)
			r := authorizedResource(t, m, c, data)
			rd, _, e := m.Acquire(context.Background(), r)
			if e != nil {
				t.Fatal(e)
			}
			b, e := io.ReadAll(rd)
			rd.Close()
			if mode == "valid" {
				if e != nil || !bytes.Equal(b, data) || count.Load() != 2 {
					t.Fatalf("安全续传失败: %v, count=%d", e, count.Load())
				}
			} else {
				if e == nil {
					t.Fatal("不安全续传未失败")
				}
				if !bytes.Equal(collect(t, m, r), data) {
					t.Fatal("完整重试失败")
				}
				mu.Lock()
				last := ranges[len(ranges)-1]
				mu.Unlock()
				if last != "" {
					t.Fatal("新代不是完整重下")
				}
			}
			want := int64(len(data))
			if mode != "valid" {
				// Rejected response bodies are closed before application reads; only the old prefix and full retry count.
				want += int64(cut)
			}
			counters, err := db.Counters()
			if err != nil || counters["upstream_bytes"] != want {
				t.Fatal("resume payload accounting", counters, want, err)
			}
		})
	}
}
func TestCleanupOldWriterDrainsNewGenerationSurvives(t *testing.T) {
	data := bytes.Repeat([]byte("clean"), 5000)
	firstStarted := make(chan struct{})
	finishOld := make(chan struct{})
	var count atomic.Int32
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		if n == 1 {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.Write(data[:8192])
			w.(http.Flusher).Flush()
			close(firstStarted)
			<-finishOld
			w.Write(data[8192:])
		} else {
			w.Write(data)
		}
	}))
	m, _, _ := setup(t, c)
	r := authorizedResource(t, m, c, data)
	old, _, e := m.Acquire(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	await(t, firstStarted)
	job, e := m.Preview(testApp, map[string]bool{r.ID: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Cleanup(testApp, job.ID); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(collect(t, m, r), data) {
		t.Fatal("新代失败")
	}
	m.mu.Lock()
	fresh := m.current[r.ID].ID
	oldPath := old.g.Path
	m.mu.Unlock()
	close(finishOld)
	b, e := io.ReadAll(old)
	if e != nil || !bytes.Equal(b, data) {
		t.Fatal("旧租约不能排空", e)
	}
	if _, e = os.Stat(oldPath); e != nil {
		t.Fatal("旧文件提前删除")
	}
	old.Close()
	if _, e = os.Stat(oldPath); !os.IsNotExist(e) {
		t.Fatal("旧文件未回收")
	}
	m.mu.Lock()
	got := m.current[r.ID].ID
	m.mu.Unlock()
	if got != fresh || count.Load() != 2 {
		t.Fatal("旧 writer 发布覆盖了新代")
	}
}
func TestCleanupPreviewCannotDeleteLaterGeneration(t *testing.T) {
	data := []byte("x")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	m, _, _ := setup(t, c)
	r := authorizedResource(t, m, c, data)
	collect(t, m, r)
	preview, e := m.Preview(testApp, map[string]bool{r.ID: true})
	if e != nil {
		t.Fatal(e)
	}
	other, e := m.Preview(testApp, map[string]bool{r.ID: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Cleanup(testApp, other.ID); e != nil {
		t.Fatal(e)
	}
	collect(t, m, r)
	if e = m.Cleanup(testApp, preview.ID); e != nil {
		t.Fatal(e)
	}
	if len(m.Snapshot()) != 1 {
		t.Fatal("过期快照删了新代")
	}
}
func TestCrashRecoveryPartRenameAndTombstone(t *testing.T) {
	data := bytes.Repeat([]byte("recovery"), 4000)
	for _, mode := range []string{"part", "rename-before-commit", "bad-blob", "retired"} {
		t.Run(mode, func(t *testing.T) {
			var rangeSeen atomic.Bool
			c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Range") != "" {
					rangeSeen.Store(true)
					w.Header().Set("Content-Range", fmt.Sprintf("bytes 1234-%d/%d", len(data)-1, len(data)))
					w.WriteHeader(206)
					w.Write(data[1234:])
				} else {
					w.Write(data)
				}
			}))
			dir := t.TempDir()
			db, e := store.Open(dir)
			if e != nil {
				t.Fatal(e)
			}
			defer db.DB.Close()
			m, e := newTestManager(dir, db, c)
			if e != nil {
				t.Fatal(e)
			}
			r := authorizedResource(t, m, c, data)
			m.mu.Lock()
			g, e := m.createLocked(r, false)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "part" {
				g.file.WriteAt(data[:1234], 0)
				g.Bytes = 1234
				g.Total = int64(len(data))
				g.State = "downloading"
			} else {
				g.file.WriteAt(data, 0)
				g.Bytes = int64(len(data))
				if mode == "bad-blob" {
					g.file.WriteAt([]byte("bad"), 0)
				}
				if mode == "retired" {
					g.Retired = true
				} else {
					fsutil.EnsureDir(filepath.Dir(m.blobPath(r)))
					os.Rename(g.Path, m.blobPath(r))
				}
			}
			if e = m.save(g); e != nil {
				t.Fatal(e)
			}
			m.mu.Unlock()
			m.Close()
			restored, e := newTestManager(dir, db, c)
			if e != nil {
				t.Fatal(e)
			}
			defer restored.Close()
			if !bytes.Equal(collect(t, restored, r), data) {
				t.Fatal("恢复字节不匹配")
			}
			if mode == "part" && !rangeSeen.Load() {
				t.Fatal("遗留 part 未续传")
			}
			if mode != "part" && rangeSeen.Load() {
				t.Fatal("错误恢复范围")
			}
			if mode == "retired" {
				if _, e = os.Stat(m.partPath(g.ID)); !os.IsNotExist(e) {
					t.Fatal("tombstone 未恢复删除")
				}
			}
		})
	}
}
