// Package download manages immutable resource identities and disk-backed generations.
// Neither metadata formats nor application versions are interpreted here.
package download

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Resource struct {
	// Application, Version and Key are the authorized logical identity. Labels are display only.
	Application string
	// MetricsID remains stable across source epochs; SourceFence binds admission.
	MetricsID   string
	SourceFence store.SourceFence
	Version     string
	Key         string
	ID          string
	Source      string
	Hash        string
	Size        *int64
	Labels      map[string]string
}

func (r Resource) MetricScope() string {
	if uid, _, ok := identity.ParseStorageID(r.Application); ok {
		return identity.MetricsID(uid)
	}
	if r.MetricsID != "" {
		return r.MetricsID
	}
	return r.Application
}

func cloneResource(r Resource) Resource {
	if r.Size != nil {
		n := *r.Size
		r.Size = &n
	}
	if r.Labels != nil {
		labels := make(map[string]string, len(r.Labels))
		for k, v := range r.Labels {
			labels[k] = v
		}
		r.Labels = labels
	}
	return r
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
	dormant        bool
	running        bool
	ctx            context.Context
	finishWork     func()
	done           bool
	readers        int
	samples        []sample
	upstreamStatus int
	downloadNS     int64
}
type sample struct {
	time  time.Time
	bytes int64
}
type Selection struct {
	Resource   string
	Generation string
	Version    string
	Key        string
	Bytes      int64
}
type Cleanup struct {
	ID                   string
	Application          string
	Selected             []Selection
	Created              time.Time
	Expires              time.Time
	Executed             *time.Time
	LogicalBytes         int64
	ReclaimableBlobBytes int64
	ActiveGenerations    int
}
type View struct {
	Generation
	ActiveWriter bool
	Readers      int
	Current      bool
	RecentBPS    float64
	AverageBPS   float64
	DownloadNS   int64
	SampledAt    time.Time
}
type Manager struct {
	mu          sync.Mutex
	dir         string
	db          *store.Store
	upstreams   map[string]*distributor.Client
	current     map[string]*Generation
	all         map[string]*Generation
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	closed      bool
	maxBytes    int64
	maxReaders  int
	maxWriters  int
	jobs        int
	httpReaders int
	// Unexported fault barrier used only by package tests; production leaves it nil.
	testFault func(string, *Generation)
}

// NewApplications owns one cache and one set of global limits across registered upstreams.
// There is deliberately no implicit application or default upstream.
func NewApplications(dir string, db *store.Store, clients map[string]*distributor.Client) (*Manager, error) {
	upstreams := make(map[string]*distributor.Client, len(clients))
	for app, client := range clients {
		if client == nil || !validApplication(app) {
			return nil, errors.New("Invalid application upstream registration")
		}
		upstreams[app] = client
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{dir: dir, db: db, upstreams: upstreams, current: map[string]*Generation{}, all: map[string]*Generation{}, ctx: ctx, cancel: cancel, maxBytes: 4 << 30, maxReaders: 512, maxWriters: 16}
	if e := m.recover(); e != nil {
		cancel()
		m.closeFiles()
		return nil, e
	}
	return m, nil
}

// RegisterUpstreams adds immutable source clients before a new runtime registry
// snapshot becomes visible. Historical clients remain available for recovery.
func (m *Manager) RegisterUpstreams(clients map[string]*distributor.Client) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("Server is shutting down")
	}
	for app, client := range clients {
		if client == nil || !validApplication(app) {
			return errors.New("Invalid application upstream registration")
		}
		if old := m.upstreams[app]; old != nil && old.Base.String() != client.Base.String() {
			return errors.New("Source namespace cannot change upstream")
		}
	}
	for app, client := range clients {
		if m.upstreams[app] == nil {
			m.upstreams[app] = client
		}
	}
	return nil
}

func validID(s string) bool {
	if len(s) != 32 && len(s) != 64 {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}
func signal(g *Generation) { close(g.changed); g.changed = make(chan struct{}) }
func id() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func LogicalIdentity(application, version, key string) string {
	sum := sha256.Sum256([]byte(application + "\x00" + version + "\x00" + key))
	return hex.EncodeToString(sum[:])
}

type verificationReader struct {
	context.Context
	io.Reader
}

func (r verificationReader) Read(p []byte) (int, error) {
	if err := r.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}
func verified(f *os.File, n int64, expected string) bool {
	return verifiedContext(context.Background(), f, n, expected)
}
func verifiedContext(ctx context.Context, f *os.File, n int64, expected string) bool {
	h := sha256.New()
	copied, e := io.Copy(h, verificationReader{ctx, io.NewSectionReader(f, 0, n)})
	return e == nil && copied == n && hex.EncodeToString(h.Sum(nil)) == expected
}
func (m *Manager) createLocked(r Resource, fullRetry bool, contexts ...context.Context) (*Generation, error) {
	ctx := m.ctx
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e := m.validateResource(r); e != nil {
		return nil, e
	}
	g := &Generation{ctx: ctx, ID: id(), Resource: r, State: "queued", Total: -1, Started: time.Now(), changed: make(chan struct{}), FullRetry: fullRetry}
	// Only complete, revalidated content may be reused across logical resources.
	// In-flight writers are never coalesced by content digest.
	if blob, e := m.db.Blob(r.Application, r.Hash); e == nil {
		if blob.SizeBytes > m.maxBytes {
			return nil, ErrArtifactLimit
		}
		path := m.blobPath(r)
		f, err := openRegular(path)
		if err == nil {
			st, statErr := f.Stat()
			valid := statErr == nil && st.Size() == blob.SizeBytes && (r.Size == nil || *r.Size == st.Size()) && verifiedContext(ctx, f, st.Size(), r.Hash)
			f.Close()
			if valid {
				g.Path, g.Bytes, g.Total, g.State, g.done = path, blob.SizeBytes, blob.SizeBytes, "complete", true
				g.Finished = time.Now()
				if e = m.db.CreateGeneration(m.record(g)); e != nil {
					return nil, e
				}
				m.current[r.ID], m.all[g.ID] = g, g
				return g, nil
			}
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.jobs >= m.maxWriters {
		return nil, ErrWriterLimit
	}
	g.Path = m.partPath(g.ID)
	f, e := os.OpenFile(g.Path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	g.file = f
	if e = m.db.CreateGeneration(m.record(g)); e != nil {
		f.Close()
		os.Remove(g.Path)
		return nil, e
	}
	m.current[r.ID], m.all[g.ID] = g, g
	return g, nil
}

type Reader struct {
	m          *Manager
	g          *Generation
	offset     int64
	ctx        context.Context
	once       sync.Once
	Kind       string
	finishWork func()
}

func (m *Manager) Acquire(ctx context.Context, r Resource) (*Reader, bool, error) {
	r = cloneResource(r)
	ctx, finish, err := m.db.ApplicationWork(ctx, r.Application)
	if err != nil {
		return nil, false, err
	}
	keep := false
	defer func() {
		if !keep {
			finish()
		}
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.validateResource(r); e != nil {
		return nil, false, e
	}
	if e := m.db.CheckSourceActive(r.Application, r.SourceFence); e != nil {
		return nil, false, e
	}
	if m.closed {
		return nil, false, errors.New("Server is shutting down")
	}
	if r.Size != nil && *r.Size > m.maxBytes {
		return nil, false, ErrArtifactLimit
	}
	if m.readersLocked() >= m.maxReaders {
		return nil, false, ErrReaderLimit
	}
	g := m.current[r.ID]
	// A previous admission may finish its readers, but cannot recruit new
	// readers or resume after an app/vendor revision has changed. Complete
	// immutable content remains reusable under a fresh fenced generation.
	if g != nil && g.State != "complete" && g.Resource.SourceFence != r.SourceFence {
		if e := m.db.RetireGeneration(r.Application, g.ID, time.Now()); e != nil {
			return nil, false, e
		}
		g.Retired = true
		delete(m.current, r.ID)
		g = nil
	}
	if g != nil && g.Bytes > m.maxBytes {
		return nil, false, ErrArtifactLimit
	}
	if g != nil && (g.Resource.Source != r.Source || g.Resource.Hash != r.Hash || g.Resource.Application != r.Application) {
		return nil, false, errors.New("Cached resource application or identity does not match")
	}
	if g != nil && g.Retired {
		if e := m.db.RetireGeneration(r.Application, g.ID, time.Now()); e != nil {
			return nil, false, e
		}
		delete(m.current, r.ID)
		g = nil
	}
	hit := g != nil
	if g != nil && g.State == "complete" {
		st, err := os.Stat(g.Path)
		valid := err == nil && st.Size() == g.Bytes
		if valid && g.dormant {
			// Inactive recovery preserves files without changing their ownership.
			// Re-admission must verify the bytes before trusting that dormant head.
			f, err := openRegular(g.Path)
			valid = err == nil && verifiedContext(ctx, f, g.Bytes, g.Resource.Hash)
			if f != nil {
				f.Close()
			}
		}
		if !valid {
			g.Retired = true
			g.Error = "Completed cache file is missing or invalid"
			m.save(g)
			if err := m.db.RetireGeneration(r.Application, g.ID, time.Now()); err != nil {
				return nil, false, err
			}
			delete(m.current, r.ID)
			m.removeLocked(g)
			g = nil
			hit = false
		} else {
			g.dormant = false
		}
	}
	if g == nil {
		var e error
		g, e = m.createLocked(r, false, ctx)
		if e != nil {
			return nil, false, e
		}
	}
	hit = hit || g.State == "complete"
	kind := "miss"
	if hit {
		kind = "shared_follower"
		if g.State == "complete" {
			kind = "cache_hit"
		}
	}
	if g.file == nil {
		f, err := openRegular(g.Path)
		if err != nil {
			return nil, false, err
		}
		g.file = f
	}
	if !g.done && !g.running {
		if m.jobs >= m.maxWriters {
			return nil, false, ErrWriterLimit
		}
		if err := m.startLocked(g); err != nil {
			return nil, false, err
		}
	}
	g.readers++
	keep = true
	return &Reader{m: m, g: g, ctx: ctx, Kind: kind, finishWork: finish}, hit, nil
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
			return 0, fmt.Errorf("Download is not verified: %s", errMsg)
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
		if r.finishWork != nil {
			defer r.finishWork()
		}
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
func (m *Manager) retireLocked(g *Generation) error {
	if err := m.db.RetireGeneration(g.Resource.Application, g.ID, time.Now()); err != nil {
		return err
	}
	g.Retired = true
	if m.current[g.Resource.ID] == g {
		delete(m.current, g.Resource.ID)
	}
	return nil
}

func (m *Manager) removeLocked(g *Generation) error {
	if g.running || g.readers > 0 {
		return nil
	}
	if g.file != nil {
		g.file.Close()
		g.file = nil
	}
	m.checkpoint("delete.before_files", g)
	part := m.partPath(g.ID)
	var released int64
	if st, e := os.Lstat(part); e == nil {
		released = st.Size()
	}
	if e := os.Remove(part); e != nil && !os.IsNotExist(e) {
		return e
	} else if e == nil {
		m.checkpoint("delete.after_part_unlink", g)
	}
	if e := m.db.DeleteGeneration(g.Resource.Application, g.ID); e != nil {
		return e
	}
	m.checkpoint("delete.after_generation_delete", g)
	// The generation reference disappears before shared content can be unlinked.
	// A crash between these steps leaves an unreferenced blob for startup GC.
	freed, e := m.collectBlob(g.Resource)
	if e != nil {
		return e
	}
	released += freed
	m.checkpoint("delete.after_files", g)
	if g.Retired && released > 0 {
		if e := m.db.AddFor(g.Resource.MetricScope(), "cleanup_freed_bytes", released); e != nil {
			return e
		}
	}
	g.State, g.done = "deleted", true
	delete(m.all, g.ID)
	return nil
}

var unsafeResume = errors.New("Unsafe upstream resume; a new generation is required")

type upstreamHTTPError int

func (e upstreamHTTPError) Error() string { return fmt.Sprintf("Upstream HTTP %d", int(e)) }

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
	m.mu.Lock()
	client := m.upstreams[g.Resource.Application]
	m.mu.Unlock()
	if client == nil {
		return errors.New("Unknown persisted resource application")
	}
	resp, e := client.Get(g.ctx, g.Resource.Source, headers)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if offset > 0 {
		if resp.StatusCode == 416 {
			var n int64
			if _, e = fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes */%d", &n); e == nil && n == offset && resp.Header.Get("Content-Range") == "bytes */"+strconv.FormatInt(n, 10) && (total < 0 || total == n) && verifiedContext(g.ctx, g.file, offset, g.Resource.Hash) {
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
			return upstreamHTTPError(resp.StatusCode)
		}
		total = resp.ContentLength
	}
	if total > m.maxBytes || (g.Resource.Size != nil && total >= 0 && *g.Resource.Size != total) {
		return errors.New("Artifact length exceeds limit or does not match")
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
			// Count bytes consumed from the identity HTTP body, even if validation or disk writes fail.
			m.mu.Lock()
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
			m.mu.Unlock()
			if e = m.db.AddFor(g.Resource.MetricScope(), "upstream_bytes", int64(n)); e != nil {
				return e
			}
			if offset+int64(n) > m.maxBytes || (total >= 0 && offset+int64(n) > total) {
				return errors.New("Artifact length exceeds limit")
			}
			written, we := g.file.WriteAt(buf[:n], offset)
			if we != nil {
				return errors.New("Disk write failed")
			}
			if written != n {
				return io.ErrShortWrite
			}
			offset += int64(n)
			m.mu.Lock()
			g.Bytes = offset
			signal(g)
			m.mu.Unlock()
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
				return errors.New("Upstream download interrupted")
			}
			break
		}
	}
	if total >= 0 && offset != total {
		return errors.New("Artifact truncated")
	}
	if g.Resource.Size != nil && offset != *g.Resource.Size {
		return errors.New("Artifact length does not match")
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
func (m *Manager) startLocked(g *Generation) error {
	ctx, finish, err := m.db.ApplicationWork(m.ctx, g.Resource.Application)
	if err != nil {
		return err
	}
	g.ctx = ctx
	g.finishWork = finish
	g.running = true
	m.jobs++
	m.wg.Add(1)
	go m.run(g)
	return nil
}
func (m *Manager) run(g *Generation) {
	defer g.finishWork()
	defer m.wg.Done()
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = m.attempt(g)
		if err == nil || errors.Is(err, unsafeResume) || g.ctx.Err() != nil {
			break
		}
		m.mu.Lock()
		g.State = "retry_wait"
		g.Error = err.Error()
		m.save(g)
		signal(g)
		m.mu.Unlock()
		select {
		case <-g.ctx.Done():
		case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
		}
	}
	if err == nil {
		m.mu.Lock()
		g.Received = time.Now()
		g.State = "verifying"
		signal(g)
		m.mu.Unlock()
		m.checkpoint("download.before_verify", g)
		start := time.Now()
		if !verifiedContext(g.ctx, g.file, g.Bytes, g.Resource.Hash) {
			err = errors.New("Complete file SHA256 does not match")
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
		if e := m.save(g); e != nil {
			err = errors.New("Cache state checkpoint failed")
		}
	}
	if err == nil {
		if e := m.db.CheckSourceActive(g.Resource.Application, g.Resource.SourceFence); e != nil {
			if errors.Is(e, store.ErrSourceInactive) {
				err = m.retireLocked(g)
			} else {
				err = e
			}
		}
	}
	if err == nil {
		if e := g.file.Sync(); e != nil {
			err = errors.New("File fsync failed")
		}
		if err == nil && !g.Retired && m.current[g.Resource.ID] == g {
			err = m.publishLocked(g)
		}
	}
	if err == nil {
		g.State = "complete"
		// A retired writer only finishes its existing readers; it never publishes a head.
		if !g.Retired {
			if e := m.db.CompleteGeneration(g.Resource.Application, g.ID, store.Blob{AppID: g.Resource.Application, SHA256: g.Resource.Hash, SizeBytes: g.Bytes, VerifiedAt: time.Now()}, g.Finished, g.VerificationNS, g.Resource.SourceFence); e != nil {
				if errors.Is(e, store.ErrSourceInactive) {
					err = m.retireLocked(g)
				} else {
					err = errors.New("Cache state commit failed")
				}
			}
		}
		if err == nil && !g.Retired {
			for _, old := range m.all {
				if old != g && old.Retired && old.State == "invalid" && old.Resource.Application == g.Resource.Application && old.Resource.Hash == g.Resource.Hash {
					// New content already has a durable reference. Removing an
					// old lease must not collect the newly published blob.
					_ = m.removeLocked(old)
				}
			}
		}
	}
	if err != nil {
		var status upstreamHTTPError
		if errors.As(err, &status) {
			g.upstreamStatus = int(status)
		}
		g.Error = err.Error()
		g.State = "failed"
		if strings.Contains(g.Error, "SHA256") {
			g.State = "invalid"
		}
		if g.ctx.Err() != nil {
			g.State = "interrupted"
		}
		m.save(g)
		m.recordFailure(g)
		m.db.AddFor(g.Resource.MetricScope(), "upstream_errors", 1)
		if m.current[g.Resource.ID] == g && g.State != "interrupted" {
			delete(m.current, g.Resource.ID)
			m.db.RetireGeneration(g.Resource.Application, g.ID, time.Now())
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
	if errors.Is(err, unsafeResume) && !g.Retired && !g.FullRetry && !m.closed && g.ctx.Err() == nil {
		if next, e := m.createLocked(g.Resource, true, g.ctx); e == nil && !next.done {
			_ = m.startLocked(next)
		}
	}
}
func (m *Manager) Snapshot() []View { return m.SnapshotFor("", "") }

// SnapshotFor scopes listing copies before materializing their runtime state.
func (m *Manager) SnapshotFor(app, version string) []View {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	out := []View{}
	for _, g := range m.all {
		if g.State == "deleted" || app != "" && g.Resource.Application != app || version != "" && g.Resource.Version != version {
			continue
		}
		v := View{Generation: *g, ActiveWriter: g.running, Readers: g.readers, Current: !g.dormant && m.current[g.Resource.ID] == g, SampledAt: now}
		v.Resource = cloneResource(g.Resource)
		v.DownloadNS = g.downloadNS
		if !g.Received.IsZero() {
			v.DownloadNS = g.Received.Sub(g.Started).Nanoseconds()
		}
		if v.DownloadNS > 0 && g.State == "complete" {
			v.AverageBPS = float64(g.Bytes) / (float64(v.DownloadNS) / 1e9)
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
	for _, c := range []struct{ pattern, category string }{{"SHA256", "hash"}, {"resume", "range"}, {"Content-Encoding", "encoding"}, {"Disk", "disk"}, {"fsync", "disk"}, {"length", "length"}, {"truncated", "length"}, {"HTTP", "http"}, {"DNS", "dns"}, {"TLS", "tls"}, {"timeout", "timeout"}, {"commit", "database"}} {
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
