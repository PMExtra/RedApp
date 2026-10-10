// Package hosted owns durable administrator-managed resources. These files have
// no TTL, origin fallback or cache-cleanup membership.
package hosted

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
)

// ErrTransferInUse reports that another running transfer uses the transfer ID.
var ErrTransferInUse = errors.New("transfer ID is in use")

type Progress struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	Total  int64  `json:"total"`
	State  string `json:"state"`
	uid    string
	cancel context.CancelFunc
}
type Service struct {
	dir       string
	db        *store.Store
	budget    download.Budget
	mu        sync.Mutex
	transfers map[string]*Progress
	closed    bool
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

func New(dir string, db *store.Store, budget download.Budget) (*Service, error) {
	if db == nil || budget == nil {
		return nil, errors.New("Persistent file storage unavailable")
	}
	for _, part := range []string{filepath.Join(dir, "objects"), filepath.Join(dir, "objects", "hosted")} {
		if err := fsutil.EnsureDir(part); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{dir: filepath.Join(dir, "objects", "hosted"), db: db, budget: budget, ctx: ctx, cancel: cancel, transfers: map[string]*Progress{}}
	// Only incomplete/unreferenced commits are recovered. Saved resources never expire.
	ids, err := db.HostedIDs()
	if err != nil {
		cancel()
		return nil, err
	}
	files, err := os.ReadDir(s.dir)
	if err != nil {
		cancel()
		return nil, err
	}
	for _, f := range files {
		if f.Type()&os.ModeSymlink != 0 || f.IsDir() {
			cancel()
			return nil, errors.New("Unexpected persistent resource entry")
		}
		name := strings.TrimSuffix(f.Name(), ".part")
		if !identity.ValidUID(name) {
			cancel()
			return nil, errors.New("Unexpected persistent resource filename")
		}
		if strings.HasSuffix(f.Name(), ".part") || !ids[name] {
			if _, err = fsutil.Remove(filepath.Join(s.dir, f.Name())); err != nil {
				cancel()
				return nil, err
			}
		}
	}
	return s, nil
}
func (s *Service) Close() { s.mu.Lock(); s.closed = true; s.cancel(); s.mu.Unlock(); s.wg.Wait() }
func (s *Service) Progress(uid, id string) (Progress, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.transfers[id]
	// A committed transfer is finished; the upload response reports it.
	if !ok || p.uid != uid || p.State == "complete" {
		return Progress{}, false
	}
	copy := *p
	copy.cancel = nil
	return copy, true
}
func (s *Service) Cancel(uid, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.transfers[id]
	if !ok || p.uid != uid || p.State == "complete" {
		return false
	}
	p.cancel()
	return true
}
func (s *Service) Put(ctx context.Context, entry application.Entry, path, expected, id string, open func(context.Context) (io.ReadCloser, int64, error)) (store.HostedFile, error) {
	ctx, finish, err := s.db.ApplicationWork(ctx, entry.UID)
	if err != nil {
		return store.HostedFile{}, err
	}
	defer finish()
	if entry.Provider != application.Hosted || entry.DeletedAt != nil || !store.ValidHostedPath(path) || !identity.ValidUID(id) || expected != "" && !identity.ValidUID(expected) {
		return store.HostedFile{}, store.ErrInvalidDirectory
	}
	release, err := s.budget.AcquireHTTPWriter()
	if err != nil {
		return store.HostedFile{}, err
	}
	defer release()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return store.HostedFile{}, context.Canceled
	}
	if s.transfers[id] != nil {
		s.mu.Unlock()
		return store.HostedFile{}, ErrTransferInUse
	}
	p := &Progress{ID: id, Path: path, Total: -1, State: "receiving", uid: entry.UID, cancel: cancel}
	s.transfers[id] = p
	s.wg.Add(1)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.transfers, id); s.mu.Unlock(); s.wg.Done() }()
	existing, err := s.db.HostedFile(entry.UID, path)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.HostedFile{}, err
	}
	if existing.ID != expected {
		return store.HostedFile{}, store.ErrHostedFileChanged
	}
	reader, size, err := open(ctx)
	if err != nil {
		return store.HostedFile{}, err
	}
	defer reader.Close()
	readerClosed := make(chan struct{})
	stopReader := context.AfterFunc(ctx, func() { defer close(readerClosed); reader.Close() })
	defer func() {
		if !stopReader() {
			<-readerClosed
		}
	}()
	limit := s.budget.MaxArtifactBytes()
	if size > limit {
		return store.HostedFile{}, download.ErrArtifactLimit
	}
	s.mu.Lock()
	p.Total = size
	s.mu.Unlock()
	objectID, err := identity.NewUID()
	if err != nil {
		return store.HostedFile{}, err
	}
	temp := filepath.Join(s.dir, objectID+".part")
	target := filepath.Join(s.dir, objectID)
	file, err := fsutil.CreateStaged(temp)
	if err != nil {
		return store.HostedFile{}, err
	}
	defer file.Discard()
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	var copied int64
	for {
		if err = ctx.Err(); err != nil {
			return store.HostedFile{}, err
		}
		n, readErr := reader.Read(buffer)
		if err = ctx.Err(); err != nil {
			return store.HostedFile{}, err
		}
		if n > 0 {
			copied += int64(n)
			if copied > limit {
				return store.HostedFile{}, download.ErrArtifactLimit
			}
			if _, err = file.Write(buffer[:n]); err != nil {
				return store.HostedFile{}, err
			}
			hash.Write(buffer[:n])
			s.mu.Lock()
			p.Bytes = copied
			s.mu.Unlock()
		}
		if readErr != nil {
			if readErr != io.EOF {
				return store.HostedFile{}, readErr
			}
			break
		}
	}
	if size >= 0 && size != copied {
		return store.HostedFile{}, io.ErrUnexpectedEOF
	}
	if err = ctx.Err(); err != nil {
		return store.HostedFile{}, err
	}
	if err = file.Seal(); err != nil {
		return store.HostedFile{}, err
	}
	s.mu.Lock()
	p.State = "committing"
	s.mu.Unlock()
	committed := false
	defer func() {
		if !committed {
			fsutil.Remove(target)
		}
	}()
	if err = file.Publish(target); err != nil {
		return store.HostedFile{}, err
	}
	if err = ctx.Err(); err != nil {
		return store.HostedFile{}, err
	}
	value := store.HostedFile{AppUID: entry.UID, ID: objectID, Path: path, SHA256: hex.EncodeToString(hash.Sum(nil)), SizeBytes: copied, CreatedAt: time.Now().UTC()}
	// Serialize publication/deletion/open. Existing open descriptors remain valid
	// across replacement, while new requests only see the committed generation.
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return store.HostedFile{}, err
	}
	old, err := s.db.CommitHosted(entry.Descriptor.ID, entry.Revision, entry.VendorRevision, expected, value)
	if err != nil {
		return store.HostedFile{}, err
	}
	committed = true
	// Publication and cancellation share this lock: a cancellation arriving
	// after commit cannot claim that it stopped the completed publication.
	p.State = "complete"
	if old.ID != "" {
		fsutil.Remove(filepath.Join(s.dir, old.ID))
	}
	return value, nil
}
func (s *Service) Open(uid, path string) (*os.File, store.HostedFile, func(), error) {
	ctx, finish, err := s.db.ApplicationWork(context.Background(), uid)
	if err != nil {
		return nil, store.HostedFile{}, nil, err
	}
	keep := false
	defer func() {
		if !keep {
			finish()
		}
	}()
	release, err := s.budget.AcquireHTTPReader()
	if err != nil {
		return nil, store.HostedFile{}, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := s.db.HostedFile(uid, path)
	if err != nil {
		release()
		return nil, row, nil, err
	}
	if !identity.ValidUID(row.ID) {
		release()
		return nil, row, nil, errors.New("Invalid persistent resource identity")
	}
	file, err := fsutil.OpenRegular(filepath.Join(s.dir, row.ID))
	if err != nil {
		release()
		return nil, row, nil, fmt.Errorf("open hosted file: %w", err)
	}
	if info, err := file.Stat(); err != nil || info.Size() != row.SizeBytes {
		file.Close()
		release()
		return nil, row, nil, errors.New("hosted file size does not match its record")
	}
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(closed); file.Close() })
	keep = true
	var once sync.Once
	return file, row, func() {
		once.Do(func() {
			if !stop() {
				<-closed
			}
			file.Close()
			release()
			finish()
		})
	}, nil
}
func (s *Service) Delete(uid, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := s.db.DeleteHosted(uid, id)
	if err != nil {
		return err
	}
	_, err = fsutil.Remove(filepath.Join(s.dir, row.ID))
	return err
}
