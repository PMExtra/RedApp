package download

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/spool"
	"github.com/PMExtra/RedApp/internal/store"
)

var (
	ErrWriterLimit   = errors.New("active download limit exceeded")
	ErrReaderLimit   = errors.New("client limit exceeded")
	ErrArtifactLimit = errors.New("artifact length exceeds configured limit")
)

func validApplication(app string) bool { return store.ValidAppID(app) }

func (m *Manager) ConfigureLimits(writers, readers int, bytes int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if writers <= 0 || readers <= 0 || bytes <= 0 {
		return errors.New("download limits must be positive")
	}
	if m.jobs != 0 {
		return errors.New("download limits must be configured before serving requests")
	}
	m.maxWriters, m.maxReaders, m.maxBytes = writers, readers, bytes
	return nil
}

// validateResource checks r against the registered upstreams (validateIdentityLocked,
// caller holds mu) and its persisted authorization (checkBinding).
func (m *Manager) validateResource(r Resource) error {
	if e := m.validateIdentityLocked(r); e != nil {
		return e
	}
	return m.checkBinding(r)
}
func (m *Manager) validateIdentityLocked(r Resource) error {
	if !validApplication(r.Application) || r.Version == "" || r.Key == "" || strings.ContainsAny(r.Version+r.Key, "\x00\r\n") || r.ID != LogicalIdentity(r.Application, r.Version, r.Key) || len(r.Hash) != 64 || !validID(r.Hash) || strings.ToLower(r.Hash) != r.Hash || (r.Size != nil && *r.Size < 0) {
		return errors.New("invalid logical resource identity")
	}
	client := m.upstreams[r.Application]
	if client == nil {
		return errors.New("unknown resource application")
	}
	u, e := url.Parse(r.Source)
	if e != nil || client.Validate(u) != nil {
		return errors.New("resource does not belong to application upstream")
	}
	return nil
}

// checkBinding compares r with its immutable persisted authorization; it reads
// the store and does not need mu.
func (m *Manager) checkBinding(r Resource) error {
	bound, e := m.db.Resource(r.Application, r.Version, r.Key)
	if e != nil {
		return fmt.Errorf("resource has no persisted metadata authorization: %w", e)
	}
	if bound.SourceURL != r.Source || bound.SHA256 != r.Hash || !equalSize(bound.ExpectedSize, r.Size) {
		return errors.New("resource differs from immutable metadata authorization")
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
	return store.Generation{Checkpoint: g.checkpoint, SourceFence: g.Resource.SourceFence, ID: g.ID, AppID: g.Resource.Application, Version: g.Resource.Version, ResourceKey: g.Resource.Key, ExpectedSHA256: g.Resource.Hash, BlobSHA256: blob, Phase: phase, IsCurrent: !g.Retired, RetiredAt: retired, Bytes: g.Bytes, TotalBytes: total, SourceBytes: g.SourceBytes, ETag: g.ETag, Resumes: g.Resumes, StartedAt: g.Started, FinishedAt: finished, VerificationNS: &verification, LastErrorCode: g.Error, FullRetry: g.FullRetry, DownloadNS: duration}
}

// generationWrite is a database snapshot of a generation taken under mu.
type generationWrite struct {
	row     store.Generation
	retired bool
}

// checkpointLocked snapshots g under mu and gives the snapshot the next
// checkpoint of g, so the store applies it only if no later snapshot of g has
// been written already. The snapshot can then be written without mu.
func (m *Manager) checkpointLocked(g *Generation) generationWrite {
	g.checkpoint++
	return generationWrite{row: m.record(g), retired: g.Retired}
}

// write persists a snapshot taken by checkpointLocked; it does not need mu.
func (m *Manager) write(w generationWrite) error {
	if w.retired {
		if e := m.db.RetireGeneration(w.row.AppID, w.row.ID, time.Now()); e != nil {
			return e
		}
	}
	return m.db.SaveGeneration(w.row)
}

// save snapshots and writes g while the caller holds mu; paths that are not
// on the transfer hot path keep their state change and its write together.
func (m *Manager) save(g *Generation) error { return m.write(m.checkpointLocked(g)) }

// completeLocked publishes g's verified blob; the caller holds mu.
func (m *Manager) completeLocked(g *Generation) error {
	g.checkpoint++
	return m.db.CompleteGeneration(store.GenerationCompletion{AppID: g.Resource.Application, ID: g.ID, Checkpoint: g.checkpoint, Blob: store.Blob{AppID: g.Resource.Application, SHA256: g.Resource.Hash, SizeBytes: g.Bytes, VerifiedAt: time.Now()}, Finished: g.Finished, VerificationNS: g.VerificationNS}, g.Resource.SourceFence)
}
func (m *Manager) closeFiles() {
	for _, g := range m.all {
		g.body.CloseFile()
	}
}

func (m *Manager) recover() error {
	for _, p := range []string{filepath.Join(m.dir, "objects"), filepath.Join(m.dir, "objects", "parts"), filepath.Join(m.dir, "objects", "blobs")} {
		if e := fsutil.EnsureDir(p); e != nil {
			return e
		}
	}
	records, e := m.db.Generations()
	if e != nil {
		return e
	}
	for _, row := range records {
		if !validID(row.ID) {
			return errors.New("invalid persisted generation identity")
		}
		bound, e := m.db.Resource(row.AppID, row.Version, row.ResourceKey)
		if e != nil {
			return e
		}
		r := Resource{Application: row.AppID, SourceFence: row.SourceFence, Version: row.Version, Key: row.ResourceKey, Source: bound.SourceURL, Hash: bound.SHA256, Size: bound.ExpectedSize}
		r.ID = LogicalIdentity(r.Application, r.Version, r.Key)
		if e = m.validateResource(r); e != nil {
			return e
		}
		if row.ExpectedSHA256 != r.Hash {
			return errors.New("persisted generation digest differs from authorization")
		}
		g := &Generation{ID: row.ID, Resource: r, Path: m.partPath(row.ID), State: "interrupted", Bytes: row.Bytes, Total: -1, SourceBytes: row.SourceBytes, ETag: row.ETag, Resumes: row.Resumes, Started: row.StartedAt, Error: row.LastErrorCode, checkpoint: row.Checkpoint, Retired: row.RetiredAt != nil || !row.IsCurrent, FullRetry: row.FullRetry, downloadNS: row.DownloadNS, body: spool.NewBody(nil, row.Bytes)}
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
			g.body = spool.NewComplete(nil, g.Bytes)
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
		f, openErr := fsutil.OpenRegular(blobPath)
		if openErr == nil {
			wasComplete := g.State == "complete"
			st, statErr := f.Stat()
			valid := statErr == nil && (g.Resource.Size == nil || *g.Resource.Size == st.Size())
			// A committed complete head of the recorded size is verified lazily
			// on its first admission, so startup does not hash every blob. A
			// blob renamed into place before its commit is hashed now, because
			// recovery is about to commit it.
			lazy := valid && wasComplete && g.Bytes == st.Size()
			if valid && !lazy {
				valid = verified(f, st.Size(), g.Resource.Hash)
			}
			f.Close()
			if lazy {
				g.Path, g.Total, g.unverified = blobPath, st.Size(), true
				if _, e = fsutil.Remove(m.partPath(g.ID)); e != nil {
					return e
				}
				continue
			}
			if valid {
				g.Path, g.Bytes, g.Total, g.State, g.done = blobPath, st.Size(), st.Size(), "complete", true
				g.body = spool.NewComplete(nil, st.Size())
				if g.Finished.IsZero() {
					g.Finished = time.Now()
				}
				if !wasComplete {
					if e = m.completeLocked(g); e != nil {
						return e
					}
				}
				if _, e = fsutil.Remove(m.partPath(g.ID)); e != nil {
					return e
				}
				continue
			}
			openErr = errors.New("recovered blob failed verification")
		}
		if g.State == "complete" || !errors.Is(openErr, fs.ErrNotExist) {
			g.Error = errBlobInvalid.Error()
			g.Retired = true
			if e = m.db.RetireGeneration(g.Resource.Application, g.ID, time.Now()); e != nil {
				return e
			}
			delete(m.current, g.Resource.ID)
			m.recordFailure(g, errBlobInvalid)
			continue
		}
		// The retained part resumes in place, so it is opened for writing.
		f, e = fsutil.OpenRegularWritable(g.Path)
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
		g.body, g.Bytes, g.State = spool.NewBody(f, st.Size()), st.Size(), "interrupted"
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

// publishLocked reuses existing (hashed without mu) when it describes the same
// unchanged blob inode; only a blob replaced since then is hashed under mu.
func (m *Manager) publishLocked(g *Generation, existing *spool.FileCheck) error {
	path := m.blobPath(g.Resource)
	if e := fsutil.EnsureDir(filepath.Dir(path)); e != nil {
		return e
	}
	if f, e := fsutil.OpenRegular(path); e == nil {
		st, se := f.Stat()
		valid := se == nil && st.Size() == g.Bytes
		if valid && existing.Matches(st) {
			valid = existing.Valid
		} else if valid {
			valid = verified(f, g.Bytes, g.Resource.Hash)
		}
		f.Close()
		if !valid {
			// Existing readers keep the old inode but must observe failure;
			// only this application's references to this digest are retired.
			if e = m.invalidateBlobLocked(g.Resource, path); e != nil {
				return e
			}
			if e = fsutil.Rename(g.Path, path); e != nil {
				return fmt.Errorf("publish repaired cache file: %w", e)
			}
		} else if e = os.Remove(g.Path); e != nil {
			return e
		} else if e = fsutil.SyncDir(filepath.Dir(g.Path)); e != nil {
			return e
		}
	} else if !errors.Is(e, fs.ErrNotExist) {
		return e
	} else if e = fsutil.Rename(g.Path, path); e != nil {
		return fmt.Errorf("publish cache file: %w", e)
	}
	g.Path = path
	return nil
}
func (m *Manager) invalidateBlobLocked(r Resource, path string) error {
	for _, old := range m.all {
		if old.Resource.Application != r.Application || old.Resource.Hash != r.Hash || old.Path != path || old.State != "complete" {
			continue
		}
		if e := m.db.RetireGeneration(old.Resource.Application, old.ID, time.Now()); e != nil {
			return e
		}
		old.Retired, old.done, old.State, old.Error = true, true, "invalid", errBlobReplaced.Error()
		if m.current[old.Resource.ID] == old {
			delete(m.current, old.Resource.ID)
		}
		old.body.Finish(errBlobReplaced)
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
		if g.Resource.Application == r.Application && g.Resource.Hash == r.Hash && g.Path == path && g.active() {
			return 0, nil
		}
	}
	var released int64
	if st, e := os.Lstat(path); e == nil {
		if !st.Mode().IsRegular() {
			return 0, errors.New("invalid blob file type")
		}
		released = st.Size()
	} else if !errors.Is(e, fs.ErrNotExist) {
		return 0, e
	}
	if _, e = fsutil.Remove(path); e != nil {
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
				return errors.New("symlink in cache directory")
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
func (m *Manager) recordFailure(g *Generation, err error) {
	// The structured method is shared by metadata and download diagnostics.
	var status *int
	if g.upstreamStatus >= 100 && g.upstreamStatus <= 599 {
		v := g.upstreamStatus
		status = &v
	}
	_ = m.db.RecordEvent(store.Event{UpstreamStatus: status, AppID: g.Resource.MetricScope(), Version: g.Resource.Version, ResourceKey: g.Resource.Key, GenerationID: g.ID, Category: failureCategory(err), Code: "download_failed", Message: g.Error})
}
