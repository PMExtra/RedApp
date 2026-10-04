package download

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
)

var (
	ErrWriterLimit   = errors.New("Active download limit exceeded")
	ErrReaderLimit   = errors.New("Client limit exceeded")
	ErrArtifactLimit = errors.New("Artifact length exceeds configured limit")
)

func validApplication(app string) bool { return store.ValidAppID(app) }

func (m *Manager) ConfigureLimits(writers, readers int, bytes int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if writers <= 0 || readers <= 0 || bytes <= 0 {
		return errors.New("Download limits must be positive")
	}
	if m.jobs != 0 {
		return errors.New("Download limits must be configured before serving requests")
	}
	m.maxWriters, m.maxReaders, m.maxBytes = writers, readers, bytes
	return nil
}

func (m *Manager) validateResource(r Resource) error {
	if !validApplication(r.Application) || r.Version == "" || r.Key == "" || strings.ContainsAny(r.Version+r.Key, "\x00\r\n") || r.ID != LogicalIdentity(r.Application, r.Version, r.Key) || len(r.Hash) != 64 || !validID(r.Hash) || strings.ToLower(r.Hash) != r.Hash || (r.Size != nil && *r.Size < 0) {
		return errors.New("Invalid logical resource identity")
	}
	client := m.upstreams[r.Application]
	if client == nil {
		return errors.New("Unknown resource application")
	}
	u, e := url.Parse(r.Source)
	if e != nil || client.Validate(u) != nil {
		return errors.New("Resource does not belong to application upstream")
	}
	bound, e := m.db.Resource(r.Application, r.Version, r.Key)
	if e != nil {
		return fmt.Errorf("Resource has no persisted metadata authorization: %w", e)
	}
	if bound.SourceURL != r.Source || bound.SHA256 != r.Hash || !equalSize(bound.ExpectedSize, r.Size) {
		return errors.New("Resource differs from immutable metadata authorization")
	}
	return nil
}
func equalSize(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func (m *Manager) partPath(generation string) string {
	return filepath.Join(m.dir, "objects", "parts", generation+".part")
}
func (m *Manager) blobPath(r Resource) string {
	return filepath.Join(m.dir, "objects", "blobs", digestApplication(r.Application), r.Hash+".blob")
}
func digestApplication(app string) string {
	h := sha256.Sum256([]byte(app))
	return hex.EncodeToString(h[:])
}

func ensureDirectory(path string) error {
	st, e := os.Lstat(path)
	created := false
	if os.IsNotExist(e) {
		e = os.Mkdir(path, 0700)
		if e != nil && !os.IsExist(e) {
			return e
		}
		created = e == nil
		st, e = os.Lstat(path)
	}
	if e != nil {
		return e
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("Cache directory must be a real directory")
	}
	// Persist the new directory's entry in its already-created parent. Callers
	// initialize each cache-tree level in order before publishing any blobs.
	if created {
		return syncDirectory(filepath.Dir(path))
	}
	return nil
}

// Cache files are never followed through a symlink, and the opened inode must
// match the inspected regular file. The instance lock owns the containing tree.
func openRegular(path string) (*os.File, error) {
	before, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("Cache path is not a regular file")
	}
	f, e := os.OpenFile(path, os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) {
		f.Close()
		return nil, errors.New("Cache file changed while opening")
	}
	return f, nil
}
func syncDirectory(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func (m *Manager) record(g *Generation) store.Generation {
	phase := "incomplete"
	blob := ""
	if g.State == "complete" && g.Path == m.blobPath(g.Resource) {
		phase = "complete"
		blob = g.Resource.Hash
	} else if g.State == "failed" || g.State == "invalid" {
		phase = "failed"
	}
	var total *int64
	if g.Total >= 0 {
		v := g.Total
		total = &v
	}
	var retired, finished *time.Time
	if g.Retired {
		now := time.Now()
		retired = &now
	}
	if !g.Finished.IsZero() {
		v := g.Finished
		finished = &v
	}
	verification := g.VerificationNS
	duration := g.downloadNS
	if !g.Received.IsZero() {
		duration = g.Received.Sub(g.Started).Nanoseconds()
		if duration < 0 {
			duration = 0
		}
	}
	return store.Generation{SourceFence: g.Resource.SourceFence, ID: g.ID, AppID: g.Resource.Application, Version: g.Resource.Version, ResourceKey: g.Resource.Key, ExpectedSHA256: g.Resource.Hash, BlobSHA256: blob, Phase: phase, IsCurrent: !g.Retired, RetiredAt: retired, Bytes: g.Bytes, TotalBytes: total, SourceBytes: g.SourceBytes, ETag: g.ETag, Resumes: g.Resumes, StartedAt: g.Started, FinishedAt: finished, VerificationNS: &verification, LastErrorCode: g.Error, FullRetry: g.FullRetry, DownloadNS: duration}
}
func (m *Manager) save(g *Generation) error {
	if g.Retired {
		if e := m.db.RetireGeneration(g.Resource.Application, g.ID, time.Now()); e != nil {
			return e
		}
	}
	return m.db.SaveGeneration(m.record(g))
}
func (m *Manager) closeFiles() {
	for _, g := range m.all {
		if g.file != nil {
			g.file.Close()
			g.file = nil
		}
	}
}

func (m *Manager) recover() error {
	for _, p := range []string{filepath.Join(m.dir, "objects"), filepath.Join(m.dir, "objects", "parts"), filepath.Join(m.dir, "objects", "blobs")} {
		if e := ensureDirectory(p); e != nil {
			return e
		}
	}
	records, e := m.db.Generations()
	if e != nil {
		return e
	}
	for _, row := range records {
		if !validID(row.ID) {
			return errors.New("Invalid persisted generation identity")
		}
		bound, e := m.db.Resource(row.AppID, row.Version, row.ResourceKey)
		if e != nil {
			return e
		}
		r := Resource{Application: row.AppID, SourceFence: row.SourceFence, Version: row.Version, Key: row.ResourceKey, Source: bound.SourceURL, Hash: bound.SHA256, Size: bound.ExpectedSize}
		if uid, _, ok := identity.ParseStorageID(r.Application); ok {
			r.MetricsID = identity.MetricsID(uid)
		}
		r.ID = LogicalIdentity(r.Application, r.Version, r.Key)
		if e = m.validateResource(r); e != nil {
			return e
		}
		if row.ExpectedSHA256 != r.Hash {
			return errors.New("Persisted generation digest differs from authorization")
		}
		g := &Generation{ID: row.ID, Resource: r, Path: m.partPath(row.ID), State: "interrupted", Bytes: row.Bytes, Total: -1, SourceBytes: row.SourceBytes, ETag: row.ETag, Resumes: row.Resumes, Started: row.StartedAt, Error: row.LastErrorCode, Retired: row.RetiredAt != nil || !row.IsCurrent, FullRetry: row.FullRetry, downloadNS: row.DownloadNS, changed: make(chan struct{})}
		fences := []store.SourceFence{r.SourceFence}
		if row.Phase == "complete" {
			fences = nil
		} // completed immutable content needs no writer admission
		if e = m.db.CheckSourceActive(r.Application, fences...); e != nil {
			if !errors.Is(e, store.ErrSourceInactive) {
				return e
			}
			g.dormant = true
		}
		if row.TotalBytes != nil {
			g.Total = *row.TotalBytes
		}
		if row.FinishedAt != nil {
			g.Finished = *row.FinishedAt
		}
		if row.VerificationNS != nil {
			g.VerificationNS = *row.VerificationNS
		}
		if row.Phase == "complete" {
			g.Path = m.blobPath(r)
			g.State = "complete"
			g.done = true
		}
		m.all[g.ID] = g
		if !g.Retired && row.Phase != "failed" {
			m.current[r.ID] = g
		}
	}
	// Recover every referenced blob before deleting any orphaned files.
	for _, g := range m.all {
		if g.Retired || g.dormant || m.current[g.Resource.ID] != g {
			continue
		}

		blobPath := m.blobPath(g.Resource)
		f, openErr := openRegular(blobPath)
		if openErr == nil {
			st, statErr := f.Stat()
			valid := statErr == nil && (g.Resource.Size == nil || *g.Resource.Size == st.Size()) && verified(f, st.Size(), g.Resource.Hash)
			f.Close()
			if valid {
				wasComplete := g.State == "complete"
				g.Path, g.Bytes, g.Total, g.State, g.done = blobPath, st.Size(), st.Size(), "complete", true
				if g.Finished.IsZero() {
					g.Finished = time.Now()
				}
				if !wasComplete {
					if e = m.db.CompleteGeneration(g.Resource.Application, g.ID, store.Blob{AppID: g.Resource.Application, SHA256: g.Resource.Hash, SizeBytes: g.Bytes, VerifiedAt: time.Now()}, g.Finished, g.VerificationNS, g.Resource.SourceFence); e != nil {
						return e
					}
				}
				if e = os.Remove(m.partPath(g.ID)); e != nil && !os.IsNotExist(e) {
					return e
				}
				continue
			}
			openErr = errors.New("Recovered blob failed verification")
		}
		if g.State == "complete" || !os.IsNotExist(openErr) {
			g.Error = "Completed cache file is missing or invalid"
			g.Retired = true
			if e = m.db.RetireGeneration(g.Resource.Application, g.ID, time.Now()); e != nil {
				return e
			}
			delete(m.current, g.Resource.ID)
			m.recordFailure(g)
			continue
		}
		f, e = openRegular(g.Path)
		if e != nil {
			g.Retired = true
			if e = m.db.RetireGeneration(g.Resource.Application, g.ID, time.Now()); e != nil {
				return e
			}
			delete(m.current, g.Resource.ID)
			continue
		}
		st, e := f.Stat()
		if e != nil {
			f.Close()
			return e
		}
		g.file, g.Bytes, g.State = f, st.Size(), "interrupted"
		if (g.Total >= 0 && g.Bytes > g.Total) || (g.Resource.Size != nil && g.Bytes > *g.Resource.Size) {
			g.Retired = true
			delete(m.current, g.Resource.ID)
		}
		if e = m.save(g); e != nil {
			return e
		}
	}
	// Resolve all surviving heads before collecting retired rows: an atomic rename
	// may have published content for another head just before its database commit.
	for _, g := range m.all {
		if g.Retired || m.current[g.Resource.ID] != g {
			g.Retired = true
			if e = m.db.RetireGeneration(g.Resource.Application, g.ID, time.Now()); e != nil {
				return e
			}
			if e = m.removeLocked(g); e != nil {
				return e
			}
		}
	}

	blobs, e := m.db.Blobs()
	if e != nil {
		return e
	}
	for _, b := range blobs {
		if _, e = m.collectBlob(Resource{Application: b.AppID, Hash: b.SHA256}); e != nil {
			return e
		}
	}
	if e = m.removeOrphans(); e != nil {
		return e
	}
	return m.db.DeleteExpiredCleanupPreviews(time.Now())
}

func (m *Manager) publishLocked(g *Generation) error {
	path := m.blobPath(g.Resource)
	if e := ensureDirectory(filepath.Dir(path)); e != nil {
		return e
	}
	if f, e := openRegular(path); e == nil {
		st, se := f.Stat()
		valid := se == nil && st.Size() == g.Bytes && verified(f, g.Bytes, g.Resource.Hash)
		f.Close()
		if !valid {
			// Existing readers keep the old inode but must observe failure;
			// only this application's references to this digest are retired.
			if e = m.invalidateBlobLocked(g.Resource, path); e != nil {
				return e
			}
			if e = os.Rename(g.Path, path); e != nil {
				return errors.New("Cache repair publication failed")
			}
		} else if e = os.Remove(g.Path); e != nil {
			return e
		}

	} else if !os.IsNotExist(e) {
		return e
	} else if e = os.Rename(g.Path, path); e != nil {
		return errors.New("Cache publication failed")
	}
	g.Path = path
	if e := syncDirectory(filepath.Dir(path)); e != nil {
		return e
	}
	return syncDirectory(filepath.Dir(m.partPath(g.ID)))
}
func (m *Manager) invalidateBlobLocked(r Resource, path string) error {
	for _, old := range m.all {
		if old.Resource.Application != r.Application || old.Resource.Hash != r.Hash || old.Path != path || old.State != "complete" {
			continue
		}
		if e := m.db.RetireGeneration(old.Resource.Application, old.ID, time.Now()); e != nil {
			return e
		}
		old.Retired, old.done, old.State, old.Error = true, true, "invalid", "Completed cache blob failed verification"
		if m.current[old.Resource.ID] == old {
			delete(m.current, old.Resource.ID)
		}
		signal(old)
		if e := m.save(old); e != nil {
			return e
		}
	}
	return nil
}

func (m *Manager) collectBlob(r Resource) (int64, error) {
	refs, e := m.db.BlobReferences(r.Application, r.Hash)
	if e != nil {
		return 0, e
	}
	if refs != 0 {
		return 0, nil
	}
	path := m.blobPath(r)
	// A just-published blob can exist before its row commits. Existing generations
	// still pin the same content while their writer or reader owns that inode.
	for _, g := range m.all {
		if g.Resource.Application == r.Application && g.Resource.Hash == r.Hash && g.Path == path && (g.running || g.readers > 0) {
			return 0, nil
		}
	}
	var released int64
	if st, e := os.Lstat(path); e == nil {
		if !st.Mode().IsRegular() {
			return 0, errors.New("Invalid blob file type")
		}
		released = st.Size()
	} else if !os.IsNotExist(e) {
		return 0, e
	}
	if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
		return 0, e
	}
	if released > 0 {
		for _, g := range m.all {
			if g.Resource.Application == r.Application && g.Resource.Hash == r.Hash {
				m.checkpoint("delete.after_blob_unlink", g)
				break
			}
		}
	}
	if _, e = m.db.DeleteUnreferencedBlob(r.Application, r.Hash); e != nil {
		return 0, e
	}
	return released, nil
}
func (m *Manager) removeOrphans() error {
	kept := map[string]bool{}
	for _, g := range m.all {
		kept[g.Path] = true
	}
	// Each storage component owns its subtree. Icons and mutable HTTP cache
	// files must never be collected by immutable release recovery.
	for _, root := range []string{"parts", "blobs"} {
		err := filepath.WalkDir(filepath.Join(m.dir, "objects", root), func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return errors.New("Symlink in cache directory")
			}
			if d.IsDir() {
				return nil
			}
			if kept[path] {
				return nil
			}
			return os.Remove(path)
		})
		if err != nil {
			return err
		}
	}
	return nil
}
func (m *Manager) recordFailure(g *Generation) {
	// The structured method is shared by metadata and download diagnostics.
	var status *int
	if g.upstreamStatus >= 100 && g.upstreamStatus <= 599 {
		v := g.upstreamStatus
		status = &v
	}
	_ = m.db.RecordEvent(store.Event{UpstreamStatus: status, AppID: g.Resource.MetricScope(), Version: g.Resource.Version, ResourceKey: g.Resource.Key, GenerationID: g.ID, Category: failureCategory(g.Error), Code: "download_failed", Message: g.Error})
}
