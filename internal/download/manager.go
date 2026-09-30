// Package download manages immutable resource identities and disk-backed generations.
// Neither metadata formats nor application versions are interpreted here.
package download

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/store"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Resource struct {
	ID     string
	Source string
	Hash   string
	Size   *int64
	Labels map[string]string
}
type Generation struct {
	ID             string
	Resource       Resource
	State          string
	Path           string
	Bytes          int64
	Total          int64
	ETag           string
	Started        time.Time
	Received       time.Time
	Finished       time.Time
	VerificationNS int64
	SourceBytes    int64
	Resumes        int
	Retired        bool
	Error          string
	FullRetry      bool
	file           *os.File
	changed        chan struct{}
	running        bool
	done           bool
	readers        int
	samples        []sample
}
type sample struct {
	time  time.Time
	bytes int64
}
type Current struct{ Generation string }
type Selection struct {
	Resource   string
	Generation string
}
type Cleanup struct {
	ID       string
	Selected []Selection
	Created  time.Time
}
type View struct {
	Generation
	Readers    int
	Current    bool
	RecentBPS  float64
	AverageBPS float64
	DownloadNS int64
	SampledAt  time.Time
}
type Manager struct {
	mu         sync.Mutex
	dir        string
	db         *store.Store
	upstream   *distributor.Client
	current    map[string]*Generation
	all        map[string]*Generation
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	closed     bool
	maxBytes   int64
	maxReaders int
	maxWriters int
	jobs       int
	// Unexported fault barrier used only by package tests; production leaves it nil.
	testFault func(string, *Generation)
}

func New(dir string, db *store.Store, c *distributor.Client) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{dir: dir, db: db, upstream: c, current: map[string]*Generation{}, all: map[string]*Generation{}, ctx: ctx, cancel: cancel, maxBytes: 4 << 30, maxReaders: 512, maxWriters: 16}
	if e := os.MkdirAll(filepath.Join(dir, "objects"), 0700); e != nil {
		cancel()
		return nil, e
	}
	raw, e := db.List("generation")
	if e != nil {
		cancel()
		return nil, e
	}
	for _, b := range raw {
		g := new(Generation)
		if e = json.Unmarshal(b, g); e != nil {
			cancel()
			return nil, e
		}
		if !validID(g.ID) || !validID(g.Resource.ID) {
			cancel()
			return nil, errors.New("无效持久缓存身份")
		}
		expected := filepath.Join(dir, "objects", g.Resource.ID, g.ID)
		if g.Path != expected+".part" && g.Path != expected+".blob" {
			cancel()
			return nil, errors.New("无效持久缓存路径")
		}
		g.changed = make(chan struct{})
		m.all[g.ID] = g
		if g.Retired || g.State == "failed" || g.State == "invalid" || g.State == "deleted" {
			if e = m.removeLocked(g); e != nil {
				cancel()
				return nil, e
			}
			continue
		}
		// Rename may have succeeded before the SQLite commit. Revalidate every recovered blob.
		blob := expected + ".blob"
		if _, e = os.Stat(blob); e == nil {
			g.Path = blob
			f, e := os.OpenFile(blob, os.O_RDWR, 0600)
			if e != nil {
				cancel()
				return nil, e
			}
			g.file = f
			st, e := f.Stat()
			if e == nil {
				g.Bytes = st.Size()
			}
			if e != nil || !verified(f, g.Bytes, g.Resource.Hash) {
				g.State = "invalid"
				g.done = true
				m.removeLocked(g)
				continue
			}
			g.State = "complete"
			g.done = true
			g.file.Close()
			g.file = nil
		} else {
			f, e := os.OpenFile(g.Path, os.O_RDWR, 0600)
			if e != nil {
				g.State = "failed"
				g.done = true
				m.removeLocked(g)
				continue
			}
			g.file = f
			st, e := f.Stat()
			if e != nil {
				cancel()
				return nil, e
			}
			g.Bytes = st.Size()
			g.State = "interrupted"
		}
		if e = m.save(g); e != nil {
			cancel()
			return nil, e
		}
	}
	ptrs, e := db.List("current")
	if e != nil {
		cancel()
		return nil, e
	}
	for _, b := range ptrs {
		var p struct {
			Resource   string
			Generation string
		}
		if e = json.Unmarshal(b, &p); e != nil {
			cancel()
			return nil, e
		}
		g := m.all[p.Generation]
		if g != nil && g.Resource.ID == p.Resource && !g.Retired && g.State != "deleted" {
			m.current[p.Resource] = g
		} else {
			if e = db.Delete("current", p.Resource); e != nil {
				cancel()
				return nil, e
			}
		}
	}
	// Any generation without a durable current pointer is an orphan of a crash window.
	for _, g := range m.all {
		if m.current[g.Resource.ID] != g {
			g.Retired = true
			if e = m.removeLocked(g); e != nil {
				cancel()
				return nil, e
			}
		}
	}
	if e = m.removeOrphans(); e != nil {
		cancel()
		return nil, e
	}
	return m, nil
}
func validID(s string) bool {
	if len(s) != 32 && len(s) != 64 {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}
func (m *Manager) removeOrphans() error {
	return filepath.WalkDir(filepath.Join(m.dir, "objects"), func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		for _, g := range m.all {
			if g.State != "deleted" && path == g.Path {
				return nil
			}
		}
		return os.Remove(path)
	})
}
func (m *Manager) save(g *Generation) error { return m.db.Put("generation", g.ID, g) }
func signal(g *Generation)                  { close(g.changed); g.changed = make(chan struct{}) }
func id() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func Identity(source, hash string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + hash))
	return hex.EncodeToString(sum[:])
}
func verified(f *os.File, n int64, expected string) bool {
	h := sha256.New()
	copied, e := io.Copy(h, io.NewSectionReader(f, 0, n))
	return e == nil && copied == n && hex.EncodeToString(h.Sum(nil)) == expected
}
func (m *Manager) createLocked(r Resource, fullRetry bool) (*Generation, error) {
	if !validID(r.ID) || len(r.Hash) != 64 {
		return nil, errors.New("无效资源身份")
	}
	if m.jobs >= m.maxWriters {
		return nil, errors.New("活动下载达到上限")
	}
	if e := os.MkdirAll(filepath.Join(m.dir, "objects", r.ID), 0700); e != nil {
		return nil, e
	}
	g := &Generation{ID: id(), Resource: r, State: "queued", Total: -1, Started: time.Now(), changed: make(chan struct{}), FullRetry: fullRetry}
	g.Path = filepath.Join(m.dir, "objects", r.ID, g.ID+".part")
	f, e := os.OpenFile(g.Path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	g.file = f
	if e = m.save(g); e != nil {
		f.Close()
		os.Remove(g.Path)
		return nil, e
	}
	if e = m.db.Put("current", r.ID, struct {
		Resource   string
		Generation string
	}{r.ID, g.ID}); e != nil {
		f.Close()
		os.Remove(g.Path)
		return nil, e
	}
	m.current[r.ID] = g
	m.all[g.ID] = g
	return g, nil
}

type Reader struct {
	m      *Manager
	g      *Generation
	offset int64
	ctx    context.Context
	once   sync.Once
	Kind   string
}

func (m *Manager) Acquire(ctx context.Context, r Resource) (*Reader, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, false, errors.New("服务关闭中")
	}
	readers := 0
	for _, g := range m.all {
		readers += g.readers
	}
	if readers >= m.maxReaders {
		return nil, false, errors.New("客户端达到上限")
	}
	g := m.current[r.ID]
	if g != nil && g.Retired {
		if e := m.db.Delete("current", r.ID); e != nil {
			return nil, false, e
		}
		delete(m.current, r.ID)
		g = nil
	}
	hit := g != nil
	if g != nil && g.State == "complete" {
		st, err := os.Stat(g.Path)
		if err != nil || st.Size() != g.Bytes {
			g.Retired = true
			g.Error = "完成缓存文件缺失或长度改变"
			m.save(g)
			if err := m.db.Delete("current", r.ID); err != nil {
				return nil, false, err
			}
			delete(m.current, r.ID)
			m.removeLocked(g)
			g = nil
			hit = false
		}
	}
	if g == nil {
		var e error
		g, e = m.createLocked(r, false)
		if e != nil {
			return nil, false, e
		}
	}
	kind := "miss"
	if hit {
		kind = "shared_follower"
		if g.State == "complete" {
			kind = "cache_hit"
		}
	}
	if g.file == nil {
		f, err := os.OpenFile(g.Path, os.O_RDWR, 0600)
		if err != nil {
			return nil, false, err
		}
		g.file = f
	}
	if !g.done && !g.running {
		if m.jobs >= m.maxWriters {
			return nil, false, errors.New("活动下载达到上限")
		}
		g.running = true
		m.jobs++
		m.wg.Add(1)
		go m.run(g)
	}
	g.readers++
	return &Reader{m: m, g: g, ctx: ctx, Kind: kind}, hit, nil
}
func (r *Reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if e := r.ctx.Err(); e != nil {
			return 0, e
		}
		r.m.mu.Lock()
		g := r.g
		available := g.Bytes - r.offset
		errMsg := g.Error
		done := g.done
		state := g.State
		ch := g.changed
		f := g.file
		// Failure must not become a successful EOF, even after all bytes were streamed.
		if done && state != "complete" {
			r.m.mu.Unlock()
			return 0, fmt.Errorf("下载未验证: %s", errMsg)
		}
		if available > 0 {
			if int64(len(p)) > available {
				p = p[:available]
			}
			r.m.mu.Unlock()
			n, e := f.ReadAt(p, r.offset)
			r.offset += int64(n)
			if e == io.EOF {
				e = io.ErrUnexpectedEOF
			}
			return n, e
		}
		r.m.mu.Unlock()
		if done {
			return 0, io.EOF
		}
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		case <-ch:
		}
	}
}
func (r *Reader) Close() error {
	var err error
	r.once.Do(func() {
		r.m.mu.Lock()
		defer r.m.mu.Unlock()
		r.g.readers--
		if r.g.readers == 0 && r.g.done && (r.g.Retired || r.g.State != "complete" && r.g.State != "interrupted") {
			err = r.m.removeLocked(r.g)
		} else if r.g.readers == 0 && r.g.done && r.g.file != nil {
			r.g.file.Close()
			r.g.file = nil
		}
	})
	return err
}
func (m *Manager) removeLocked(g *Generation) error {
	if g.running || g.readers > 0 {
		return nil
	}
	if g.file != nil {
		g.file.Close()
		g.file = nil
	}
	var released int64
	if st, e := os.Stat(g.Path); e == nil {
		released = st.Size()
	}
	m.checkpoint("delete.before_files", g)
	paths := []string{g.Path, filepath.Join(m.dir, "objects", g.Resource.ID, g.ID+".blob"), filepath.Join(m.dir, "objects", g.Resource.ID, g.ID+".part")}
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		e := os.Remove(path)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil {
			if strings.HasSuffix(path, ".part") {
				m.checkpoint("delete.after_part_unlink", g)
			} else {
				m.checkpoint("delete.after_blob_unlink", g)
			}
		}
	}
	m.checkpoint("delete.after_files", g)
	if g.Retired && released > 0 {
		m.db.Add("cleanup_freed_bytes", released)
	}
	if e := m.db.Delete("generation", g.ID); e != nil {
		return e
	}
	m.checkpoint("delete.after_generation_delete", g)
	g.State = "deleted"
	g.done = true
	delete(m.all, g.ID)
	return nil
}

var unsafeResume = errors.New("上游续传不安全，必须创建新代")

func (m *Manager) attempt(g *Generation) error {
	m.mu.Lock()
	offset := g.Bytes
	etag := g.ETag
	total := g.Total
	g.State = "downloading"
	if len(g.samples) == 0 {
		g.samples = []sample{{time.Now(), g.SourceBytes}}
	}
	if offset > 0 {
		g.State = "resuming"
		g.Resumes++
	}
	e := m.save(g)
	m.mu.Unlock()
	if e != nil {
		return e
	}
	headers := http.Header{}
	if offset > 0 {
		headers.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		if etag != "" && !strings.HasPrefix(etag, "W/") {
			headers.Set("If-Range", etag)
		}
	}
	resp, e := m.upstream.Get(m.ctx, g.Resource.Source, headers)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if offset > 0 {
		if resp.StatusCode == 416 {
			var n int64
			if _, e = fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes */%d", &n); e == nil && n == offset && resp.Header.Get("Content-Range") == "bytes */"+strconv.FormatInt(n, 10) && (total < 0 || total == n) && verified(g.file, offset, g.Resource.Hash) {
				return nil
			}
			return unsafeResume
		}
		if resp.StatusCode != 206 {
			return unsafeResume
		}
		start, end, n, e := parseRange(resp.Header.Get("Content-Range"))
		if e != nil || start != offset || end != n-1 || (total >= 0 && total != n) || n > m.maxBytes || (resp.ContentLength >= 0 && resp.ContentLength != end-start+1) || (etag != "" && resp.Header.Get("ETag") != "" && resp.Header.Get("ETag") != etag) {
			return unsafeResume
		}
		total = n
	} else {
		if resp.StatusCode != 200 {
			return fmt.Errorf("上游 HTTP %d", resp.StatusCode)
		}
		total = resp.ContentLength
	}
	if total > m.maxBytes || (g.Resource.Size != nil && total >= 0 && *g.Resource.Size != total) {
		return errors.New("制品长度超出限制或不匹配")
	}
	m.mu.Lock()
	g.Total = total
	if offset == 0 {
		g.ETag = resp.Header.Get("ETag")
	}
	g.State = "downloading"
	e = m.save(g)
	m.mu.Unlock()
	if e != nil {
		return e
	}
	buf := make([]byte, 64<<10)
	var checkpoint int64 = offset
	for {
		n, re := resp.Body.Read(buf)
		if n > 0 {
			if offset+int64(n) > m.maxBytes || (total >= 0 && offset+int64(n) > total) {
				return errors.New("制品超长")
			}
			written, we := g.file.WriteAt(buf[:n], offset)
			if we != nil {
				return errors.New("磁盘写入失败")
			}
			if written != n {
				return io.ErrShortWrite
			}
			offset += int64(n)
			m.mu.Lock()
			g.Bytes = offset
			g.SourceBytes += int64(n)
			now := time.Now()
			if len(g.samples) == 0 {
				g.samples = append(g.samples, sample{g.Started, 0})
			}
			if len(g.samples) < 2 || now.Sub(g.samples[len(g.samples)-1].time) > 200*time.Millisecond {
				g.samples = append(g.samples, sample{now, g.SourceBytes})
			} else {
				g.samples[len(g.samples)-1] = sample{now, g.SourceBytes}
			}
			for len(g.samples) > 2 && now.Sub(g.samples[1].time) > 5*time.Second {
				g.samples = g.samples[1:]
			}
			signal(g)
			m.mu.Unlock()
			if e = m.db.Add("upstream_bytes", int64(n)); e != nil {
				return e
			}
			if offset-checkpoint >= 1<<20 {
				m.mu.Lock()
				e = m.save(g)
				m.mu.Unlock()
				if e != nil {
					return e
				}
				checkpoint = offset
			}
		}
		if re != nil {
			if re != io.EOF {
				return errors.New("上游下载中断")
			}
			break
		}
	}
	if total >= 0 && offset != total {
		return errors.New("制品截断")
	}
	if g.Resource.Size != nil && offset != *g.Resource.Size {
		return errors.New("制品长度不匹配")
	}
	return nil
}
func parseRange(s string) (int64, int64, int64, error) {
	if !strings.HasPrefix(s, "bytes ") {
		return 0, 0, 0, unsafeResume
	}
	parts := strings.Split(strings.TrimPrefix(s, "bytes "), "/")
	if len(parts) != 2 {
		return 0, 0, 0, unsafeResume
	}
	bounds := strings.Split(parts[0], "-")
	if len(bounds) != 2 {
		return 0, 0, 0, unsafeResume
	}
	a, e1 := strconv.ParseInt(bounds[0], 10, 64)
	b, e2 := strconv.ParseInt(bounds[1], 10, 64)
	n, e3 := strconv.ParseInt(parts[1], 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || a < 0 || b < a || n <= b {
		return 0, 0, 0, unsafeResume
	}
	return a, b, n, nil
}
func (m *Manager) run(g *Generation) {
	defer m.wg.Done()
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = m.attempt(g)
		if err == nil || errors.Is(err, unsafeResume) || m.ctx.Err() != nil {
			break
		}
		m.mu.Lock()
		g.State = "retry_wait"
		g.Error = err.Error()
		m.save(g)
		signal(g)
		m.mu.Unlock()
		select {
		case <-m.ctx.Done():
		case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
		}
	}
	if err == nil {
		m.mu.Lock()
		g.Received = time.Now()
		g.State = "verifying"
		signal(g)
		m.mu.Unlock()
		start := time.Now()
		if !verified(g.file, g.Bytes, g.Resource.Hash) {
			err = errors.New("完整文件 SHA256 不匹配")
		}
		m.mu.Lock()
		g.VerificationNS = time.Since(start).Nanoseconds()
		m.mu.Unlock()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	g.Finished = time.Now()
	g.running = false
	m.jobs--
	g.done = true
	g.Error = ""
	if err == nil {
		if e := g.file.Sync(); e != nil {
			err = errors.New("文件 fsync 失败")
		}
		if err == nil && !g.Retired && m.current[g.Resource.ID] == g {
			newPath := strings.TrimSuffix(g.Path, ".part") + ".blob"
			if e := os.Rename(g.Path, newPath); e != nil {
				err = errors.New("缓存发布失败")
			} else {
				g.Path = newPath
				if dir, e := os.Open(filepath.Dir(newPath)); e == nil {
					e = dir.Sync()
					dir.Close()
					if e != nil {
						err = e
					}
				}
			}
		}
	}
	if err == nil {
		g.State = "complete"
		if e := m.save(g); e != nil {
			err = errors.New("缓存状态提交失败")
		}
	}
	if err != nil {
		g.Error = err.Error()
		g.State = "failed"
		if strings.Contains(g.Error, "SHA256") {
			g.State = "invalid"
		}
		if m.ctx.Err() != nil {
			g.State = "interrupted"
		}
		m.save(g)
		m.db.Event(g.Resource.ID, failureCategory(g.Error), g.Error)
		m.db.Add("upstream_errors", 1)
		if m.current[g.Resource.ID] == g && g.State != "interrupted" {
			delete(m.current, g.Resource.ID)
			m.db.Delete("current", g.Resource.ID)
		}
	}
	signal(g)
	if g.readers == 0 && g.done && g.file != nil && (g.State == "complete" || g.State == "interrupted") {
		g.file.Close()
		g.file = nil
	}
	if g.readers == 0 && (g.Retired || (g.State != "complete" && g.State != "interrupted")) {
		m.removeLocked(g)
	}
	if errors.Is(err, unsafeResume) && !g.Retired && !g.FullRetry && !m.closed {
		if next, e := m.createLocked(g.Resource, true); e == nil {
			next.running = true
			m.jobs++
			m.wg.Add(1)
			go m.run(next)
		}
	}
}
func (m *Manager) Snapshot() []View {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	out := []View{}
	for _, g := range m.all {
		if g.State == "deleted" {
			continue
		}
		v := View{Generation: *g, Readers: g.readers, Current: m.current[g.Resource.ID] == g, SampledAt: now}
		if !g.Received.IsZero() {
			v.DownloadNS = g.Received.Sub(g.Started).Nanoseconds()
			if v.DownloadNS > 0 && g.State == "complete" {
				v.AverageBPS = float64(g.Bytes) / (float64(v.DownloadNS) / 1e9)
			}
		}
		if len(g.samples) > 1 {
			first := g.samples[0]
			if now.Sub(first.time).Seconds() > 0 && now.Sub(g.samples[len(g.samples)-1].time) < 5*time.Second {
				v.RecentBPS = float64(g.SourceBytes-first.bytes) / now.Sub(first.time).Seconds()
			}
		}
		out = append(out, v)
	}
	return out
}
func (m *Manager) Preview(ids map[string]bool) (Cleanup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := Cleanup{ID: id(), Created: time.Now(), Selected: []Selection{}}
	for rid, g := range m.current {
		if ids[rid] {
			job.Selected = append(job.Selected, Selection{rid, g.ID})
		}
	}
	if e := m.db.Put("cleanup", job.ID, job); e != nil {
		return job, e
	}
	m.checkpoint("preview.after_job_save", nil)
	return job, nil
}
func (m *Manager) Cleanup(jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var job Cleanup
	if !validID(jobID) {
		return errors.New("无效清理 ID")
	}
	if e := m.db.Get("cleanup", jobID, &job); e != nil {
		return e
	}
	if time.Since(job.Created) > 10*time.Minute {
		return errors.New("预览已过期")
	}
	for _, s := range job.Selected {
		g := m.all[s.Generation]
		if g == nil || g.Resource.ID != s.Resource {
			continue
		}
		wasRetired := g.Retired
		g.Retired = true
		if e := m.save(g); e != nil {
			g.Retired = wasRetired
			return e
		}
		m.checkpoint("cleanup.after_tombstone", g)
		if m.current[s.Resource] == g {
			if e := m.db.Delete("current", s.Resource); e != nil {
				return e
			}
			m.checkpoint("cleanup.after_pointer_delete", g)
			delete(m.current, s.Resource)
			m.checkpoint("cleanup.after_detach", g)
		}
		signal(g)
		if e := m.removeLocked(g); e != nil {
			return e
		}
	}
	if e := m.db.Delete("cleanup", jobID); e != nil {
		return e
	}
	m.checkpoint("cleanup.after_job_delete", nil)
	return nil
}
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.cancel()
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, g := range m.all {
		if g.file != nil {
			g.file.Close()
			g.file = nil
		}
	}
	return nil
}

func failureCategory(message string) string {
	for _, c := range []struct{ pattern, category string }{{"SHA256", "hash"}, {"续传", "range"}, {"Content-Encoding", "encoding"}, {"磁盘", "disk"}, {"fsync", "disk"}, {"长度", "length"}, {"截断", "length"}, {"HTTP", "http"}, {"DNS", "dns"}, {"TLS", "tls"}, {"超时", "timeout"}, {"提交", "database"}} {
		if strings.Contains(message, c.pattern) {
			return c.category
		}
	}
	return "upstream"
}

func (m *Manager) checkpoint(point string, g *Generation) {
	if m.testFault != nil {
		m.testFault(point, g)
	}
}
