// Package download manages immutable resource identities and disk-backed generations.
// Neither metadata formats nor application versions are interpreted here.
package download

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
	"io"
	mathrand "math/rand/v2"
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
	hashing        bool // pinned by a manager-owned verification running without mu
	rangeable      bool // upstream advertised or served byte ranges for this generation
	samples        []sample
	upstreamStatus int
	downloadNS     int64
}

// active generations own their files: a writer, readers or an in-flight hash.
func (g *Generation) active() bool { return g.running || g.readers > 0 || g.hashing }

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

// Lock invariant: mu guards in-memory state and short database/file metadata
// operations, but is never held while hashing a whole file. A complete-file
// SHA256 runs in a manager-owned goroutine registered in verifying; callers
// wait for it without mu and then re-run admission, which re-checks generation
// identity and state. A generation being hashed is pinned (hashing) and is not
// removed until that verification commits its result under mu.
type Manager struct {
	publicationMu sync.Mutex // Configuration publication and Close only; never acquired while holding mu.
	mu            sync.Mutex
	verifying     map[string]*verification // logical resource ID -> in-flight verification
	dir           string
	db            *store.Store
	upstreams     map[string]*distributor.Client
	current       map[string]*Generation
	all           map[string]*Generation
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	closed        bool
	maxBytes      int64
	maxReaders    int
	maxWriters    int
	jobs          int
	httpReaders   int
	// Upstream transfer policy: a body read waiting idleTimeout without bytes
	// fails the attempt; transient failures retry up to retryAttempts times.
	idleTimeout   time.Duration
	retryAttempts int
	retryBase     time.Duration
	retryMax      time.Duration
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
	m := &Manager{dir: dir, db: db, upstreams: upstreams, current: map[string]*Generation{}, all: map[string]*Generation{}, verifying: map[string]*verification{}, ctx: ctx, cancel: cancel, maxBytes: 4 << 30, maxReaders: 512, maxWriters: 16,
		idleTimeout: distributor.DefaultIdleTimeout, retryAttempts: 6, retryBase: time.Second, retryMax: 30 * time.Second}
	if e := m.recover(); e != nil {
		cancel()
		m.closeFiles()
		return nil, e
	}
	return m, nil
}

// RegisterUpstreams adds immutable source clients before a new runtime registry
// snapshot becomes visible. Historical clients remain available for recovery.
// UpstreamPublication owns the narrow publication gate through the DB commit.
// Lock order: publicationMu -> short mu prepare -> release mu -> DB transaction
// -> commit/release DB connection -> mu publish. Transfers never take this gate.
type UpstreamPublication struct {
	manager *Manager
	clients map[string]*distributor.Client
}

func (m *Manager) PrepareUpstreams(clients map[string]*distributor.Client) (*UpstreamPublication, error) {
	m.publicationMu.Lock()
	m.mu.Lock()
	defer m.mu.Unlock()
	fail := func(err error) (*UpstreamPublication, error) { m.publicationMu.Unlock(); return nil, err }
	if m.closed {
		return fail(errors.New("Server is shutting down"))
	}
	copied := make(map[string]*distributor.Client, len(clients))
	for app, client := range clients {
		if client == nil || !validApplication(app) {
			return fail(errors.New("Invalid application upstream registration"))
		}
		if old := m.upstreams[app]; old != nil && old.Base.String() != client.Base.String() {
			return fail(errors.New("Source namespace cannot change upstream"))
		}
		copied[app] = client
	}
	return &UpstreamPublication{manager: m, clients: copied}, nil
}

func (p *UpstreamPublication) Abort()   { p.manager.publicationMu.Unlock() }
func (p *UpstreamPublication) Publish() { p.PublishWith(func() {}) }

func (p *UpstreamPublication) PublishWith(publish func()) {
	m := p.manager
	m.mu.Lock()
	for app, client := range p.clients {
		if m.upstreams[app] == nil {
			m.upstreams[app] = client
		}
	}
	m.mu.Unlock()
	publish()
	m.publicationMu.Unlock()
}
func (m *Manager) RegisterUpstreams(clients map[string]*distributor.Client) error {
	plan, err := m.PrepareUpstreams(clients)
	if err != nil {
		return err
	}
	plan.Publish()
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
	ok, err := hashMatches(ctx, f, n, expected)
	return ok && err == nil
}

// hashMatches reports whether the first n bytes of f have the expected digest.
// A non-nil error means the check was cancelled and proves nothing about the
// content; unreadable or short content is reported as a mismatch.
func hashMatches(ctx context.Context, f *os.File, n int64, expected string) (bool, error) {
	h := sha256.New()
	copied, e := io.Copy(h, verificationReader{ctx, io.NewSectionReader(f, 0, n)})
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return e == nil && copied == n && hex.EncodeToString(h.Sum(nil)) == expected, nil
}

// fileCheck is a whole-file verification result bound to the inode it read.
type fileCheck struct {
	info  os.FileInfo
	valid bool
}

// checkFile hashes a cache file without holding mu. A nil result with a nil
// error means the file could not be opened as a regular cache file.
func checkFile(ctx context.Context, path string, n int64, expected string) (*fileCheck, error) {
	f, err := fsutil.OpenRegular(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, nil
	}
	ok := false
	if st.Size() == n {
		if ok, err = hashMatches(ctx, f, n, expected); err != nil {
			return nil, err
		}
	}
	return &fileCheck{info: st, valid: ok}, nil
}

// matches reports whether path is still the exact inode and size that was hashed.
func (c *fileCheck) matches(st os.FileInfo) bool {
	return c != nil && st != nil && os.SameFile(c.info, st) && st.Size() == c.info.Size() && st.ModTime().Equal(c.info.ModTime())
}

// verification is one in-flight whole-file check for a logical resource.
// err is written before done is closed and is returned to every waiter.
type verification struct {
	done chan struct{}
	err  error
}

var errVerificationPending = errors.New("Cache verification in progress")

// startVerificationLocked runs check without mu under application work owned
// by the manager, so a waiter's cancellation never aborts it for the others.
// commit runs under mu with the hashing result (or the cancellation error).
func (m *Manager) startVerificationLocked(r Resource, check func(context.Context) (*fileCheck, error), commit func(*fileCheck, error) error) error {
	if m.verifying[r.ID] != nil {
		return errVerificationPending
	}
	ctx, finish, err := m.db.ApplicationWork(m.ctx, r.Application)
	if err != nil {
		return err
	}
	v := &verification{done: make(chan struct{})}
	m.verifying[r.ID] = v
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer finish()
		m.checkpoint("verification.before_hash", nil)
		result, err := check(ctx)
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.verifying, r.ID)
		v.err = commit(result, err)
		close(v.done)
	}()
	return nil
}

// verifyDormantLocked re-admits a dormant complete head only after its bytes
// verify. Cancellation leaves it untouched; only proven-invalid content retires.
func (m *Manager) verifyDormantLocked(g *Generation) error {
	path, n, hash := g.Path, g.Bytes, g.Resource.Hash
	err := m.startVerificationLocked(g.Resource, func(ctx context.Context) (*fileCheck, error) {
		return checkFile(ctx, path, n, hash)
	}, func(check *fileCheck, err error) error {
		g.hashing = false
		if err != nil {
			return err
		}
		if m.current[g.Resource.ID] != g || g.Retired || g.State != "complete" || !g.dormant || g.Path != path {
			// Ownership changed while hashing; removal deferred by the pin happens now.
			if g.Retired && !g.running && g.readers == 0 && g.State != "deleted" {
				return m.removeLocked(g)
			}
			return nil
		}
		if st, e := os.Lstat(path); e == nil && check.matches(st) && check.valid {
			g.dormant = false
			return nil
		}
		return m.discardCompleteLocked(g)
	})
	if err == nil {
		g.hashing = true
	}
	return err
}

// discardCompleteLocked retires a complete head whose bytes are missing or invalid.
func (m *Manager) discardCompleteLocked(g *Generation) error {
	g.Retired = true
	g.Error = "Completed cache file is missing or invalid"
	m.save(g)
	if err := m.db.RetireGeneration(g.Resource.Application, g.ID, time.Now()); err != nil {
		return err
	}
	if m.current[g.Resource.ID] == g {
		delete(m.current, g.Resource.ID)
	}
	m.removeLocked(g)
	return nil
}

// createLocked returns errVerificationPending when an existing blob must be
// verified first; the verification then installs either a complete generation
// reusing the blob or a queued download generation.
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
	// Only complete, revalidated content may be reused across logical resources.
	// In-flight writers are never coalesced by content digest.
	if blob, e := m.db.Blob(r.Application, r.Hash); e == nil {
		if blob.SizeBytes > m.maxBytes {
			return nil, ErrArtifactLimit
		}
		path := m.blobPath(r)
		if st, err := os.Lstat(path); err == nil && st.Mode().IsRegular() && st.Size() == blob.SizeBytes && (r.Size == nil || *r.Size == st.Size()) {
			if e = m.verifyBlobLocked(r, fullRetry, path, blob.SizeBytes); e != nil {
				return nil, e
			}
			return nil, errVerificationPending
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	return m.createPartLocked(ctx, r, fullRetry)
}

// verifyBlobLocked hashes an existing blob without mu, then installs a complete
// generation if the same inode verified, or a queued download otherwise.
func (m *Manager) verifyBlobLocked(r Resource, fullRetry bool, path string, size int64) error {
	return m.startVerificationLocked(r, func(ctx context.Context) (*fileCheck, error) {
		return checkFile(ctx, path, size, r.Hash)
	}, func(check *fileCheck, err error) error {
		if err != nil {
			return err
		}
		if m.closed {
			return errors.New("Server is shutting down")
		}
		if m.current[r.ID] != nil {
			return nil // Another admission installed a head; waiters re-run admission.
		}
		if e := m.db.CheckSourceActive(r.Application, r.SourceFence); e != nil {
			return e
		}
		st, statErr := os.Lstat(path)
		if statErr == nil && check != nil && !check.matches(st) {
			return nil // Replaced while hashing (e.g. a repair published); verify again.
		}
		if statErr == nil && check != nil && check.valid {
			blob, e := m.db.Blob(r.Application, r.Hash)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if e == nil && blob.SizeBytes == size {
				id, e := fsutil.RandomID()
				if e != nil {
					return e
				}
				g := &Generation{ctx: m.ctx, ID: id, Resource: r, Path: path, Bytes: size, Total: size, State: "complete", done: true, Started: time.Now(), changed: make(chan struct{}), FullRetry: fullRetry}
				g.Finished = time.Now()
				if e = m.db.CreateGeneration(m.record(g)); e != nil {
					return e
				}
				m.current[r.ID], m.all[g.ID] = g, g
				return nil
			}
		}
		_, e := m.createPartLocked(m.ctx, r, fullRetry)
		return e
	})
}

func (m *Manager) createPartLocked(ctx context.Context, r Resource, fullRetry bool) (*Generation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.jobs >= m.maxWriters {
		return nil, ErrWriterLimit
	}
	id, err := fsutil.RandomID()
	if err != nil {
		return nil, err
	}
	g := &Generation{ctx: ctx, ID: id, Resource: r, State: "queued", Total: -1, Started: time.Now(), changed: make(chan struct{}), FullRetry: fullRetry}
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
	for first := true; ; first = false {
		reader, hit, wait, err := m.admit(ctx, r, finish, first)
		if wait == nil {
			keep = err == nil
			return reader, hit, err
		}
		// Only this caller stops waiting on cancellation; the manager-owned
		// verification continues for other waiters and commits its own result.
		m.checkpoint("acquire.wait_verification", nil)
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-wait.done:
		}
		if wait.err != nil {
			return nil, false, wait.err
		}
	}
}

// admit performs one admission pass under mu. A non-nil verification means the
// caller must wait for it without mu and then re-run admission.
func (m *Manager) admit(ctx context.Context, r Resource, finish func(), first bool) (*Reader, bool, *verification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if first {
		m.checkpoint("acquire_before_db_validation", nil)
	}
	reader, hit, err := m.admitLocked(ctx, r, finish)
	if errors.Is(err, errVerificationPending) {
		if v := m.verifying[r.ID]; v != nil {
			return nil, false, v, nil
		}
	}
	return reader, hit, nil, err
}

func (m *Manager) admitLocked(ctx context.Context, r Resource, finish func()) (*Reader, bool, error) {
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
	if m.verifying[r.ID] != nil {
		return nil, false, errVerificationPending
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
			if e := m.verifyDormantLocked(g); e != nil {
				return nil, false, e
			}
			return nil, false, errVerificationPending
		}
		if !valid {
			if err := m.discardCompleteLocked(g); err != nil {
				return nil, false, err
			}
			g = nil
			hit = false
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
		// Complete content is only read; a retained part resumes in place.
		open := fsutil.OpenRegularWritable
		if g.State == "complete" {
			open = fsutil.OpenRegular
		}
		f, err := open(g.Path)
		if err != nil {
			return nil, false, err
		}
		g.file = f
	}
	// A retained part from an exhausted or interrupted transfer resumes.
	resume := g.done && !g.running && g.State == "interrupted"
	if (!g.done || resume) && !g.running {
		if m.jobs >= m.maxWriters {
			return nil, false, ErrWriterLimit
		}
		g.done = false
		if err := m.startLocked(g); err != nil {
			g.done = resume
			return nil, false, err
		}
	}
	g.readers++
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
	if g.active() {
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
	if removed, e := fsutil.Remove(part); e != nil {
		return e
	} else if removed {
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

var (
	unsafeResume    = errors.New("Unsafe upstream resume; a new generation is required")
	errHashMismatch = errors.New("Complete file SHA256 does not match")
	errTruncated    = errors.New("Artifact truncated")
)

type upstreamHTTPError int

func (e upstreamHTTPError) Error() string { return fmt.Sprintf("Upstream HTTP %d", int(e)) }

// causeError keeps a stable user-visible message while preserving its cause
// for classification. transient marks failures a later attempt may resolve.
type causeError struct {
	message   string
	cause     error
	transient bool
}

func (e *causeError) Error() string { return e.message }
func (e *causeError) Unwrap() error { return e.cause }

// retryable reports transport-level failures; integrity, length, encoding,
// disk and client HTTP errors are final.
func retryable(err error) bool {
	var status upstreamHTTPError
	var cause *causeError
	switch {
	case errors.Is(err, distributor.ErrConnection), errors.Is(err, errTruncated):
		return true
	case errors.As(err, &status):
		return status >= 500 || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests
	case errors.As(err, &cause):
		return cause.transient
	}
	return false
}

// retryDelay is exponential backoff with jitter in [d/2, d], capped at retryMax.
func (m *Manager) retryDelay(attempt int) time.Duration {
	d := m.retryMax
	if attempt < 30 && m.retryBase<<attempt < m.retryMax {
		d = m.retryBase << attempt
	}
	if d <= 1 {
		return d
	}
	return d/2 + mathrand.N(d/2+1)
}

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
	resp, e := client.Get(distributor.WithIdleTimeout(g.ctx, m.idleTimeout), g.Resource.Source, headers)
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
	if resp.StatusCode == http.StatusPartialContent || strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes") {
		g.rangeable = true
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
				return &causeError{"Disk write failed", we, false}
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
				return &causeError{"Upstream download interrupted", re, true}
			}
			break
		}
	}
	if total >= 0 && offset != total {
		return errTruncated
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
	for attempt := 0; ; attempt++ {
		err = m.attempt(g)
		if err == nil || !retryable(err) || g.ctx.Err() != nil || attempt+1 >= m.retryAttempts {
			break
		}
		m.mu.Lock()
		g.State = "retry_wait"
		g.Error = err.Error()
		m.save(g)
		signal(g)
		m.mu.Unlock()
		wait := time.NewTimer(m.retryDelay(attempt))
		select {
		case <-g.ctx.Done():
		case <-wait.C:
		}
		wait.Stop()
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
			err = errHashMismatch
		}
		m.mu.Lock()
		g.VerificationNS = time.Since(start).Nanoseconds()
		m.mu.Unlock()
	}
	// Hash any blob already published for this digest before taking mu;
	// publishLocked trusts this result only for the same unchanged inode.
	var existing *fileCheck
	if err == nil {
		existing, _ = checkFile(g.ctx, m.blobPath(g.Resource), g.Bytes, g.Resource.Hash)
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
			err = m.publishLocked(g, existing)
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
		if errors.Is(err, errHashMismatch) {
			g.State = "invalid"
		}
		// Exhausted transient failures keep a resumable prefix; the next
		// admission resumes it with Range. Integrity failures never do.
		if g.ctx.Err() != nil || retryable(err) && g.Bytes > 0 && (g.ETag != "" || g.rangeable) {
			g.State = "interrupted"
		}
		m.save(g)
		m.recordFailure(g, err)
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
	m.checkpoint("close_before_publication_gate", nil)
	m.publicationMu.Lock()
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.publicationMu.Unlock()
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

// failureCategory classifies typed errors first; message patterns remain for
// persisted messages without an error value and for local stable messages.
func failureCategory(err error, message string) string {
	var status upstreamHTTPError
	switch {
	case errors.Is(err, errHashMismatch):
		return "hash"
	case errors.Is(err, unsafeResume):
		return "range"
	case errors.Is(err, distributor.ErrUnsafeEncoding):
		return "encoding"
	case errors.As(err, &status):
		return "http"
	}
	if err != nil {
		switch distributor.Classify(err) {
		case distributor.KindDNS:
			return "dns"
		case distributor.KindTLS:
			return "tls"
		case distributor.KindTimeout:
			return "timeout"
		}
	}
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

// WaitVerified waits for final size and digest verification without reading bytes.
func (r *Reader) WaitVerified() error {
	for {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		r.m.mu.Lock()
		done, state, changed := r.g.done, r.g.State, r.g.changed
		r.m.mu.Unlock()
		if done {
			if state == "complete" {
				return nil
			}
			return errors.New("Download verification failed")
		}
		select {
		case <-r.ctx.Done():
			return r.ctx.Err()
		case <-changed:
		}
	}
}
