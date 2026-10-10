package httpcache

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/PMExtra/RedApp/internal/fsutil"
)

// bodyStore is the narrow read/delete boundary used by serving and maintenance.
// Business operations address an immutable generation ID, never a local path.
// Seek supports HTTP ranges; a future object backend can implement it with
// bounded range reads. This does not promise remote append or rename semantics.
type bodyStore interface {
	Open(string) (io.ReadSeekCloser, int64, error)
	Size(string) (int64, error)
	Delete(string) (bool, error)
}

type localBodies struct{ directory string }

var bodyID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (s *Service) bodies() bodyStore { return localBodies{s.dir} }

func (b localBodies) path(id string) (string, error) {
	if !bodyID.MatchString(id) {
		return "", errors.New("Invalid blob identifier")
	}
	return filepath.Join(b.directory, id+".body"), nil
}

func (b localBodies) Open(id string) (io.ReadSeekCloser, int64, error) {
	path, err := b.path(id)
	if err != nil {
		return nil, 0, err
	}
	f, err := fsutil.OpenRegular(path)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

func (b localBodies) Size(id string) (int64, error) {
	path, err := b.path(id)
	if err != nil {
		return 0, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, errors.New("HTTP cache body must be a regular file")
	}
	return info.Size(), nil
}

func (b localBodies) Delete(id string) (bool, error) {
	path, err := b.path(id)
	if err != nil {
		return false, err
	}
	return fsutil.Remove(path)
}

// Staging, fsync, atomic local publication and crash recovery remain in the
// local cache implementation. A future backend must define its own commit and
// abandoned-upload lifecycle instead of emulating POSIX rename guarantees.
